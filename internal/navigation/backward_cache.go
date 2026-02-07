package navigation

import (
	"container/heap"
	"sync"
	"time"
	"waze/internal/graph"
)

// BackwardSearchResult stores distances from an entry point to all nodes in a city
type BackwardSearchResult struct {
	EntryNodeID int
	CityName    string
	Distances   map[int]float64 // node ID -> distance (in hours)
	ComputedAt  time.Time
	TTL         time.Duration
}

// IsExpired checks if the cached result is still valid
func (bsr *BackwardSearchResult) IsExpired() bool {
	return time.Since(bsr.ComputedAt) > bsr.TTL
}

// BackwardSearchCache caches backward searches from entry points
type BackwardSearchCache struct {
	mu       sync.RWMutex
	cache    map[string]map[int]*BackwardSearchResult // cityName -> entryNodeID -> result
	ttl      time.Duration
	maxSize  int       // maximum number of cached entries per city
	lastCleanup time.Time
}

func NewBackwardSearchCache(ttl time.Duration) *BackwardSearchCache {
	return &BackwardSearchCache{
		cache:       make(map[string]map[int]*BackwardSearchResult),
		ttl:         ttl,
		maxSize:     10, // Limit to 10 entries per city
		lastCleanup: time.Now(),
	}
}

// Get retrieves a cached backward search result
func (bsc *BackwardSearchCache) Get(cityName string, entryNodeID int) (*BackwardSearchResult, bool) {
	bsc.mu.RLock()
	defer bsc.mu.RUnlock()

	cityCache, exists := bsc.cache[cityName]
	if !exists {
		return nil, false
	}

	result, exists := cityCache[entryNodeID]
	if !exists || result.IsExpired() {
		return nil, false
	}

	return result, true
}

// Set stores a backward search result
func (bsc *BackwardSearchCache) Set(result *BackwardSearchResult) {
	bsc.mu.Lock()
	defer bsc.mu.Unlock()

	if _, exists := bsc.cache[result.CityName]; !exists {
		bsc.cache[result.CityName] = make(map[int]*BackwardSearchResult)
	}

	bsc.cache[result.CityName][result.EntryNodeID] = result

	// Periodic cleanup to prevent unbounded growth
	if time.Since(bsc.lastCleanup) > 30*time.Second {
		bsc.cleanupExpired()
		bsc.lastCleanup = time.Now()
	}
}

// cleanupExpired removes expired cache entries (must be called with lock held)
func (bsc *BackwardSearchCache) cleanupExpired() {
	for cityName, cityCache := range bsc.cache {
		for entryID, result := range cityCache {
			if result.IsExpired() {
				delete(cityCache, entryID)
			}
		}
		// Remove empty city caches
		if len(cityCache) == 0 {
			delete(bsc.cache, cityName)
		}
	}
}

// ForwardSearchResult stores distances and predecessors from an entry point to all nodes in a city
type ForwardSearchResult struct {
	EntryNodeID int
	CityName    string
	Distances   map[int]float64 // node ID -> distance from entry (in hours)
	CameFrom    map[int]int     // node ID -> predecessor for path reconstruction
	ComputedAt  time.Time
	TTL         time.Duration
}

func (fsr *ForwardSearchResult) IsExpired() bool {
	return time.Since(fsr.ComputedAt) > fsr.TTL
}

// ForwardSearchCache caches forward searches from entry points
type ForwardSearchCache struct {
	mu          sync.RWMutex
	cache       map[string]map[int]*ForwardSearchResult // cityName -> entryNodeID -> result
	ttl         time.Duration
	lastCleanup time.Time
}

func NewForwardSearchCache(ttl time.Duration) *ForwardSearchCache {
	return &ForwardSearchCache{
		cache:       make(map[string]map[int]*ForwardSearchResult),
		ttl:         ttl,
		lastCleanup: time.Now(),
	}
}

func (fsc *ForwardSearchCache) Get(cityName string, entryNodeID int) (*ForwardSearchResult, bool) {
	fsc.mu.RLock()
	defer fsc.mu.RUnlock()

	cityCache, exists := fsc.cache[cityName]
	if !exists {
		return nil, false
	}

	result, exists := cityCache[entryNodeID]
	if !exists || result.IsExpired() {
		return nil, false
	}

	return result, true
}

func (fsc *ForwardSearchCache) Set(result *ForwardSearchResult) {
	fsc.mu.Lock()
	defer fsc.mu.Unlock()

	if _, exists := fsc.cache[result.CityName]; !exists {
		fsc.cache[result.CityName] = make(map[int]*ForwardSearchResult)
	}

	fsc.cache[result.CityName][result.EntryNodeID] = result

	if time.Since(fsc.lastCleanup) > 30*time.Second {
		for cityName, cityCache := range fsc.cache {
			for entryID, r := range cityCache {
				if r.IsExpired() {
					delete(cityCache, entryID)
				}
			}
			if len(cityCache) == 0 {
				delete(fsc.cache, cityName)
			}
		}
		fsc.lastCleanup = time.Now()
	}
}

// ComputeBackwardSearch performs backward Dijkstra from an entry point to all nodes in the city
func ComputeBackwardSearch(g *graph.Graph, entryNodeID int, cityName string, cityNodes map[int]bool, ttl time.Duration) *BackwardSearchResult {
	// Use reverse adjacency list for backward search
	distances := make(map[int]float64)
	distances[entryNodeID] = 0

	pq := newPriorityQueue()
	heap.Init(pq)
	heap.Push(pq, &AstarNode{
		NodeId:   entryNodeID,
		Priority: 0,
		Gscore:   0,
	})

	visited := make(map[int]bool)
	maxNodes := 5000 // Limit backward search to prevent memory explosion
	nodesProcessed := 0

	for pq.Len() > 0 && nodesProcessed < maxNodes {
		current := heap.Pop(pq).(*AstarNode)
		u := current.NodeId

		if visited[u] {
			continue
		}
		visited[u] = true
		nodesProcessed++

		// Only explore within the city
		if !cityNodes[u] && u != entryNodeID {
			continue
		}

		// Explore backward edges (incoming edges to u)
		if reverseEdges, exists := g.ReverseAdjList[u]; exists {
			for _, edge := range reverseEdges {
				v := edge.From

				// Only relax if v is in the city
				if !cityNodes[v] && v != entryNodeID {
					continue
				}

				speed := edge.GetCurrentSpeed()
				if speed <= 0 {
					speed = 1.0
				}

				timeCost := edge.Length / speed
				newDist := distances[u] + timeCost

				if oldDist, exists := distances[v]; !exists || newDist < oldDist {
					distances[v] = newDist

					if _, inQueue := pq.index[v]; inQueue {
						pq.Update(v, newDist, newDist)
					} else {
						heap.Push(pq, &AstarNode{
							NodeId:   v,
							Priority: newDist,
							Gscore:   newDist,
						})
					}
				}
			}
		}
	}

	return &BackwardSearchResult{
		EntryNodeID: entryNodeID,
		CityName:    cityName,
		Distances:   distances,
		ComputedAt:  time.Now(),
		TTL:         ttl,
	}
}

// ComputeAllBackwardSearches computes backward searches from all entry points in parallel
func ComputeAllBackwardSearches(g *graph.Graph, cityName string, entryPoints []int, cityNodes map[int]bool, ttl time.Duration) []*BackwardSearchResult {
	results := make([]*BackwardSearchResult, len(entryPoints))
	var wg sync.WaitGroup

	for i, entryID := range entryPoints {
		wg.Add(1)
		go func(idx int, entryNodeID int) {
			defer wg.Done()
			results[idx] = ComputeBackwardSearch(g, entryNodeID, cityName, cityNodes, ttl)
		}(i, entryID)
	}

	wg.Wait()
	return results
}
