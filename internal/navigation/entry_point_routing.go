package navigation

import (
	"fmt"
	"math"
	"sync"
	"time"
	"waze/internal/graph"
)

// EntryPointRouter handles routing using entry points and cached searches
type EntryPointRouter struct {
	Graph              *graph.Graph
	EntryPointManager  *EntryPointManager
	BackwardCache      *BackwardSearchCache
	ForwardCache       *ForwardSearchCache
	CacheTTL           time.Duration
	mu                 sync.RWMutex
}

func NewEntryPointRouter(g *graph.Graph, cacheTTL time.Duration) *EntryPointRouter {
	return &EntryPointRouter{
		Graph:             g,
		EntryPointManager: NewEntryPointManager(),
		BackwardCache:     NewBackwardSearchCache(cacheTTL),
		ForwardCache:      NewForwardSearchCache(cacheTTL),
		CacheTTL:          cacheTTL,
	}
}

// FindPathWithEntryPoints finds shortest path using entry point strategy
func (epr *EntryPointRouter) FindPathWithEntryPoints(srcID, dstID int) (*PathResult, error) {
	srcNode, ok1 := epr.Graph.Nodes[srcID]
	dstNode, ok2 := epr.Graph.Nodes[dstID]

	if !ok1 || !ok2 {
		return nil, fmt.Errorf("one of the nodes does not exist inside the graph")
	}

	// Check if destination is in a known city
	dstCity, dstInCity := epr.EntryPointManager.GetCity(dstID)

	// If destination is not in a city or same city as source, use regular A*
	srcCity, srcInCity := epr.EntryPointManager.GetCity(srcID)
	if !dstInCity || (srcInCity && srcCity == dstCity) {
		return FindPathDeltaStepping(epr.Graph, srcID, dstID)
	}

	// Get entry points for destination city
	entryPoints, exists := epr.EntryPointManager.GetEntryPoints(dstCity)
	if !exists || len(entryPoints) == 0 {
		// No entry points defined, fall back to regular A*
		return FindPathDeltaStepping(epr.Graph, srcID, dstID)
	}

	// Get or compute backward searches (reverse graph: dist(node → entry)) and
	// forward searches (normal graph: dist(entry → node)) from all entry points
	backwardResults := epr.getOrComputeBackwardSearches(dstCity, entryPoints)
	forwardResults := epr.getOrComputeForwardSearches(dstCity, entryPoints)

	// Find best path through entry points
	bestPath, bestDistance, bestETA, err := epr.findBestPathThroughEntries(srcID, dstID, srcNode, dstNode, entryPoints, backwardResults, forwardResults)
	if err != nil {
		// Fall back to regular A* if entry point routing fails
		return FindPathDeltaStepping(epr.Graph, srcID, dstID)
	}

	return &PathResult{
		RouteNodes: bestPath,
		Distance:   bestDistance,
		ETA:        bestETA * 60, // convert to minutes
	}, nil
}

// getOrComputeBackwardSearches retrieves cached backward searches or computes them
func (epr *EntryPointRouter) getOrComputeBackwardSearches(cityName string, entryPoints []int) []*BackwardSearchResult {
	results := make([]*BackwardSearchResult, len(entryPoints))
	needsCompute := make([]int, 0)
	needsComputeIdx := make([]int, 0)
	cachedCount := 0

	// Check cache
	for i, entryID := range entryPoints {
		if cached, found := epr.BackwardCache.Get(cityName, entryID); found {
			results[i] = cached
			cachedCount++
		} else {
			needsCompute = append(needsCompute, entryID)
			needsComputeIdx = append(needsComputeIdx, i)
		}
	}

	if cachedCount > 0 {
		fmt.Printf("[CACHE HIT] Reusing %d/%d cached backward searches for %s\n", cachedCount, len(entryPoints), cityName)
	}

	// Compute missing backward searches in parallel
	if len(needsCompute) > 0 {
		fmt.Printf("[COMPUTING] Computing %d backward searches in parallel for %s from entries %v\n", len(needsCompute), cityName, needsCompute)
		cityNodes := epr.getCityNodes(cityName)
		computed := ComputeAllBackwardSearchesDelta(epr.Graph, cityName, needsCompute, cityNodes, epr.CacheTTL)

		// Store in cache and results
		for i, result := range computed {
			epr.BackwardCache.Set(result)
			results[needsComputeIdx[i]] = result
		}
		fmt.Printf("[CACHED] Stored %d new backward searches for %s (TTL: %v)\n", len(computed), cityName, epr.CacheTTL)
	}

	return results
}

// getOrComputeForwardSearches retrieves cached forward searches or computes them
func (epr *EntryPointRouter) getOrComputeForwardSearches(cityName string, entryPoints []int) []*ForwardSearchResult {
	results := make([]*ForwardSearchResult, len(entryPoints))
	needsCompute := make([]int, 0)
	needsComputeIdx := make([]int, 0)
	cachedCount := 0

	for i, entryID := range entryPoints {
		if cached, found := epr.ForwardCache.Get(cityName, entryID); found {
			results[i] = cached
			cachedCount++
		} else {
			needsCompute = append(needsCompute, entryID)
			needsComputeIdx = append(needsComputeIdx, i)
		}
	}

	if cachedCount > 0 {
		fmt.Printf("[CACHE HIT] Reusing %d/%d cached forward searches for %s\n", cachedCount, len(entryPoints), cityName)
	}

	if len(needsCompute) > 0 {
		fmt.Printf("[COMPUTING] Computing %d forward searches in parallel for %s from entries %v\n", len(needsCompute), cityName, needsCompute)
		cityNodes := epr.getCityNodes(cityName)
		computed := ComputeAllForwardSearchesDelta(epr.Graph, cityName, needsCompute, cityNodes, epr.CacheTTL)

		for i, result := range computed {
			epr.ForwardCache.Set(result)
			results[needsComputeIdx[i]] = result
		}
		fmt.Printf("[CACHED] Stored %d new forward searches for %s (TTL: %v)\n", len(computed), cityName, epr.CacheTTL)
	}

	return results
}

// getCityNodes returns the pre-computed map of all nodes in a city
func (epr *EntryPointRouter) getCityNodes(cityName string) map[int]bool {
	if city, exists := epr.EntryPointManager.Cities[cityName]; exists {
		return city.Nodes
	}
	return make(map[int]bool)
}

// findBestPathThroughEntries finds the best path going through entry points
func (epr *EntryPointRouter) findBestPathThroughEntries(srcID, dstID int, srcNode, dstNode *graph.Node, entryPoints []int, backwardResults []*BackwardSearchResult, forwardResults []*ForwardSearchResult) ([]int, float64, float64, error) {
	type EntryPathResult struct {
		entryIdx    int
		pathToEntry []int
		costToEntry float64
		totalCost   float64
		err         error
	}

	results := make([]EntryPathResult, len(entryPoints))
	var wg sync.WaitGroup

	// Compute paths to each entry point in parallel
	for i, entryID := range entryPoints {
		wg.Add(1)
		go func(idx int, entry int) {
			defer wg.Done()

			// Check if destination is reachable from this entry (using forward search)
			forwardResult := forwardResults[idx]
			costFromEntry, reachable := forwardResult.Distances[dstID]
			if !reachable {
				results[idx].err = fmt.Errorf("destination not reachable from entry %d", entry)
				return
			}

			// Compute path from source to entry using Delta-Stepping
			pathResult, err := epr.computePathToEntry(srcID, entry, srcNode, epr.Graph.Nodes[entry])
			if err != nil {
				results[idx].err = err
				return
			}

			results[idx].entryIdx = idx
			results[idx].pathToEntry = pathResult.RouteNodes
			results[idx].costToEntry = pathResult.ETA / 60 // convert back to hours
			results[idx].totalCost = results[idx].costToEntry + costFromEntry
		}(i, entryID)
	}

	wg.Wait()

	// Find best entry point
	bestIdx := -1
	bestCost := math.Inf(1)

	for i, result := range results {
		if result.err == nil && result.totalCost < bestCost {
			bestCost = result.totalCost
			bestIdx = i
		}
	}

	if bestIdx == -1 {
		return nil, 0, 0, fmt.Errorf("no valid path through any entry point")
	}

	// Construct full path: source -> entry -> destination
	bestResult := results[bestIdx]
	entryID := entryPoints[bestIdx]

	// Get path from entry to destination (reconstruct from forward search cameFrom)
	pathFromEntry := epr.reconstructPathFromForward(entryID, dstID, forwardResults[bestIdx])

	// Combine paths (avoid duplicating the entry point)
	fullPath := append([]int{}, bestResult.pathToEntry...)
	if len(pathFromEntry) > 1 {
		fullPath = append(fullPath, pathFromEntry[1:]...) // skip first node (entry) to avoid duplication
	}

	// Calculate total distance
	totalDistance := calcDistFromEdges(epr.Graph, fullPath)

	return fullPath, totalDistance, bestCost, nil
}

// computePathToEntry computes path from source to entry point using Delta-Stepping
func (epr *EntryPointRouter) computePathToEntry(srcID, entryID int, srcNode, entryNode *graph.Node) (*PathResult, error) {
	return FindPathDeltaStepping(epr.Graph, srcID, entryID)
}

// reconstructPathFromForward reconstructs path from entry to destination
// using the CameFrom predecessor map from the forward search
func (epr *EntryPointRouter) reconstructPathFromForward(entryID, dstID int, forwardResult *ForwardSearchResult) []int {
	// Trace predecessors from destination back to entry
	path := []int{}
	current := dstID

	for {
		path = append(path, current)
		if current == entryID {
			break
		}
		prev, ok := forwardResult.CameFrom[current]
		if !ok {
			break
		}
		current = prev
	}

	// Reverse to get entry → destination order
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return path
}

// reconstructRouteNodes reconstructs the route as node IDs
func reconstructRouteNodes(cameFrom map[int]int, current int) []int {
	path := make([]int, 0)
	for {
		path = append(path, current)
		prev, ok := cameFrom[current]
		if !ok {
			break
		}
		current = prev
	}

	// Reverse the path
	for i := 0; i < len(path)/2; i++ {
		path[i], path[len(path)-1-i] = path[len(path)-1-i], path[i]
	}

	return path
}

// calcDistFromEdges calculates distance from node path
func calcDistFromEdges(g *graph.Graph, nodePath []int) float64 {
	totalDist := 0.0
	for i := 0; i < len(nodePath)-1; i++ {
		u := nodePath[i]
		v := nodePath[i+1]

		for _, edge := range g.GetNeighbors(u) {
			if edge.To == v {
				totalDist += edge.Length
				break
			}
		}
	}
	return totalDist
}
