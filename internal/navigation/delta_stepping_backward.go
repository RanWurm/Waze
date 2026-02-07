package navigation

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"time"
	"waze/internal/graph"
)

// backwardDeltaState holds state for a backward Delta-Stepping search
// Uses maps to only allocate for visited nodes (not all 113K)
type backwardDeltaState struct {
	g         *graph.Graph
	delta     float64
	nCPUs     int
	cityNodes map[int]bool

	tent    map[int]float64 // node ID → tentative distance
	settled map[int]bool    // node ID → whether finalized
	buckets map[int][]int   // bucket index → list of node IDs
}

func newBackwardDeltaState(g *graph.Graph, entryNodeID int, delta float64, nCPUs int, cityNodes map[int]bool) *backwardDeltaState {
	tent := make(map[int]float64)
	tent[entryNodeID] = 0

	buckets := make(map[int][]int)
	buckets[0] = []int{entryNodeID}

	return &backwardDeltaState{
		g:         g,
		delta:     delta,
		nCPUs:     nCPUs,
		cityNodes: cityNodes,
		tent:      tent,
		settled:   make(map[int]bool),
		buckets:   buckets,
	}
}

func (ds *backwardDeltaState) getTent(nodeID int) float64 {
	if d, ok := ds.tent[nodeID]; ok {
		return d
	}
	return math.Inf(1)
}

// run executes backward Delta-Stepping, stops after maxNodes are settled
func (ds *backwardDeltaState) run(maxNodes int) {
	currentBucket := 0
	maxBucket := 0

	for {
		// Find next non-empty bucket
		found := false
		for currentBucket <= maxBucket {
			if nodes, exists := ds.buckets[currentBucket]; exists && len(nodes) > 0 {
				found = true
				break
			}
			currentBucket++
		}
		if !found {
			break
		}

		// Light phase
		allSettled := make([]int, 0)
		for {
			nodes, exists := ds.buckets[currentBucket]
			if !exists || len(nodes) == 0 {
				break
			}

			R := make([]int, len(nodes))
			copy(R, nodes)
			ds.buckets[currentBucket] = ds.buckets[currentBucket][:0]

			// Filter stale and settled
			active := R[:0]
			for _, u := range R {
				if ds.settled[u] {
					continue
				}
				if int(ds.getTent(u)/ds.delta) != currentBucket {
					continue
				}
				active = append(active, u)
			}
			if len(active) == 0 {
				break
			}

			allSettled = append(allSettled, active...)

			requests := ds.parallelScanBackwardEdges(active, true)
			for _, req := range requests {
				if !ds.settled[req.node] && req.dist < ds.getTent(req.node) {
					ds.tent[req.node] = req.dist
					b := int(req.dist / ds.delta)
					ds.buckets[b] = append(ds.buckets[b], req.node)
					if b > maxBucket {
						maxBucket = b
					}
				}
			}
		}

		// Mark settled
		for _, node := range allSettled {
			ds.settled[node] = true
		}

		// Stop if we've settled enough nodes
		if len(ds.settled) >= maxNodes {
			break
		}

		// Heavy phase
		seen := make(map[int]bool, len(allSettled))
		unique := make([]int, 0, len(allSettled))
		for _, node := range allSettled {
			if !seen[node] {
				seen[node] = true
				unique = append(unique, node)
			}
		}

		requests := ds.parallelScanBackwardEdges(unique, false)
		for _, req := range requests {
			if !ds.settled[req.node] && req.dist < ds.getTent(req.node) {
				ds.tent[req.node] = req.dist
				b := int(req.dist / ds.delta)
				ds.buckets[b] = append(ds.buckets[b], req.node)
				if b > maxBucket {
					maxBucket = b
				}
			}
		}

		currentBucket++
	}
}

// parallelScanBackwardEdges scans reverse edges in parallel, filtered to city nodes
func (ds *backwardDeltaState) parallelScanBackwardEdges(nodes []int, isLight bool) []relaxRequest {
	if len(nodes) == 0 {
		return nil
	}

	numWorkers := ds.nCPUs
	if len(nodes) < numWorkers {
		numWorkers = len(nodes)
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	chunkSize := (len(nodes) + numWorkers - 1) / numWorkers
	results := make([][]relaxRequest, numWorkers)

	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		end := min(start+chunkSize, len(nodes))
		if start >= len(nodes) {
			break
		}

		wg.Add(1)
		go func(workerID, s, e int) {
			defer wg.Done()
			local := make([]relaxRequest, 0, (e-s)*4)

			for i := s; i < e; i++ {
				u := nodes[i]
				uDist := ds.getTent(u)

				if math.IsInf(uDist, 1) {
					continue
				}

				reverseEdges, exists := ds.g.ReverseAdjList[u]
				if !exists {
					continue
				}

				for _, edge := range reverseEdges {
					v := edge.From

					if !ds.cityNodes[v] {
						continue
					}

					speed := edge.GetCurrentSpeed()
					if speed <= 0 {
						speed = 1.0
					}
					weight := edge.Length / speed

					if isLight && weight >= ds.delta {
						continue
					}
					if !isLight && weight < ds.delta {
						continue
					}

					newDist := uDist + weight

					if newDist < ds.getTent(v) {
						local = append(local, relaxRequest{
							node: v,
							dist: newDist,
							from: u,
						})
					}
				}
			}
			results[workerID] = local
		}(w, start, end)
	}

	wg.Wait()

	total := 0
	for _, r := range results {
		total += len(r)
	}
	merged := make([]relaxRequest, 0, total)
	for _, r := range results {
		merged = append(merged, r...)
	}
	return merged
}

// ComputeBackwardSearchDelta performs backward Delta-Stepping from an entry point
func ComputeBackwardSearchDelta(g *graph.Graph, entryNodeID int, cityName string, cityNodes map[int]bool, ttl time.Duration) *BackwardSearchResult {
	// Use a larger delta for backward search (10x average edge weight)
	delta := g.DefaultDelta * 10
	if delta <= 0 {
		delta = 0.005
	}

	nCPUs := runtime.GOMAXPROCS(0)
	if nCPUs < 1 {
		nCPUs = 1
	}

	maxNodes := 5000

	ds := newBackwardDeltaState(g, entryNodeID, delta, nCPUs, cityNodes)

	start := time.Now()
	ds.run(maxNodes)
	elapsed := time.Since(start)

	// Build distances map (only reachable city nodes)
	distances := make(map[int]float64)
	for nodeID := range cityNodes {
		if d, ok := ds.tent[nodeID]; ok {
			distances[nodeID] = d
		}
	}

	// Memory stats
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	fmt.Printf("[BACKWARD-DS] city=%s entry=%d delta=%.6f settled=%d reachable=%d time=%v heapMB=%.1f\n",
		cityName, entryNodeID, delta, len(ds.settled), len(distances), elapsed, float64(mem.HeapAlloc)/(1024*1024))

	return &BackwardSearchResult{
		EntryNodeID: entryNodeID,
		CityName:    cityName,
		Distances:   distances,
		ComputedAt:  time.Now(),
		TTL:         ttl,
	}
}

// forwardDeltaState holds state for a forward Delta-Stepping search from an entry point
// Uses forward edges so Distances[v] = dist(entry → v), with cameFrom for path reconstruction
type forwardDeltaState struct {
	g         *graph.Graph
	delta     float64
	nCPUs     int
	cityNodes map[int]bool

	tent     map[int]float64
	cameFrom map[int]int
	settled  map[int]bool
	buckets  map[int][]int
}

func newForwardDeltaState(g *graph.Graph, entryNodeID int, delta float64, nCPUs int, cityNodes map[int]bool) *forwardDeltaState {
	tent := make(map[int]float64)
	tent[entryNodeID] = 0

	buckets := make(map[int][]int)
	buckets[0] = []int{entryNodeID}

	return &forwardDeltaState{
		g:         g,
		delta:     delta,
		nCPUs:     nCPUs,
		cityNodes: cityNodes,
		tent:      tent,
		cameFrom:  make(map[int]int),
		settled:   make(map[int]bool),
		buckets:   buckets,
	}
}

func (ds *forwardDeltaState) getTent(nodeID int) float64 {
	if d, ok := ds.tent[nodeID]; ok {
		return d
	}
	return math.Inf(1)
}

func (ds *forwardDeltaState) run(maxNodes int) {
	currentBucket := 0
	maxBucket := 0

	for {
		found := false
		for currentBucket <= maxBucket {
			if nodes, exists := ds.buckets[currentBucket]; exists && len(nodes) > 0 {
				found = true
				break
			}
			currentBucket++
		}
		if !found {
			break
		}

		allSettled := make([]int, 0)
		for {
			nodes, exists := ds.buckets[currentBucket]
			if !exists || len(nodes) == 0 {
				break
			}

			R := make([]int, len(nodes))
			copy(R, nodes)
			ds.buckets[currentBucket] = ds.buckets[currentBucket][:0]

			active := R[:0]
			for _, u := range R {
				if ds.settled[u] {
					continue
				}
				if int(ds.getTent(u)/ds.delta) != currentBucket {
					continue
				}
				active = append(active, u)
			}
			if len(active) == 0 {
				break
			}

			allSettled = append(allSettled, active...)

			requests := ds.parallelScanForwardEdges(active, true)
			for _, req := range requests {
				if !ds.settled[req.node] && req.dist < ds.getTent(req.node) {
					ds.tent[req.node] = req.dist
					ds.cameFrom[req.node] = req.from
					b := int(req.dist / ds.delta)
					ds.buckets[b] = append(ds.buckets[b], req.node)
					if b > maxBucket {
						maxBucket = b
					}
				}
			}
		}

		for _, node := range allSettled {
			ds.settled[node] = true
		}

		if len(ds.settled) >= maxNodes {
			break
		}

		seen := make(map[int]bool, len(allSettled))
		unique := make([]int, 0, len(allSettled))
		for _, node := range allSettled {
			if !seen[node] {
				seen[node] = true
				unique = append(unique, node)
			}
		}

		requests := ds.parallelScanForwardEdges(unique, false)
		for _, req := range requests {
			if !ds.settled[req.node] && req.dist < ds.getTent(req.node) {
				ds.tent[req.node] = req.dist
				ds.cameFrom[req.node] = req.from
				b := int(req.dist / ds.delta)
				ds.buckets[b] = append(ds.buckets[b], req.node)
				if b > maxBucket {
					maxBucket = b
				}
			}
		}

		currentBucket++
	}
}

func (ds *forwardDeltaState) parallelScanForwardEdges(nodes []int, isLight bool) []relaxRequest {
	if len(nodes) == 0 {
		return nil
	}

	numWorkers := ds.nCPUs
	if len(nodes) < numWorkers {
		numWorkers = len(nodes)
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	chunkSize := (len(nodes) + numWorkers - 1) / numWorkers
	results := make([][]relaxRequest, numWorkers)

	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		end := min(start+chunkSize, len(nodes))
		if start >= len(nodes) {
			break
		}

		wg.Add(1)
		go func(workerID, s, e int) {
			defer wg.Done()
			local := make([]relaxRequest, 0, (e-s)*4)

			for i := s; i < e; i++ {
				u := nodes[i]
				uDist := ds.getTent(u)

				if math.IsInf(uDist, 1) {
					continue
				}

				for _, edge := range ds.g.GetNeighbors(u) {
					v := edge.To

					speed := edge.GetCurrentSpeed()
					if speed <= 0 {
						speed = 1.0
					}
					weight := edge.Length / speed

					if isLight && weight >= ds.delta {
						continue
					}
					if !isLight && weight < ds.delta {
						continue
					}

					newDist := uDist + weight

					if newDist < ds.getTent(v) {
						local = append(local, relaxRequest{
							node: v,
							dist: newDist,
							from: u,
						})
					}
				}
			}
			results[workerID] = local
		}(w, start, end)
	}

	wg.Wait()

	total := 0
	for _, r := range results {
		total += len(r)
	}
	merged := make([]relaxRequest, 0, total)
	for _, r := range results {
		merged = append(merged, r...)
	}
	return merged
}

// ComputeForwardSearchDelta performs forward Delta-Stepping from an entry point
// to all reachable city nodes. Distances[v] = dist(entry → v), with cameFrom for path reconstruction.
func ComputeForwardSearchDelta(g *graph.Graph, entryNodeID int, cityName string, cityNodes map[int]bool, ttl time.Duration) *ForwardSearchResult {
	delta := g.DefaultDelta * 10
	if delta <= 0 {
		delta = 0.005
	}

	nCPUs := runtime.GOMAXPROCS(0)
	if nCPUs < 1 {
		nCPUs = 1
	}

	maxNodes := 5000

	ds := newForwardDeltaState(g, entryNodeID, delta, nCPUs, cityNodes)

	start := time.Now()
	ds.run(maxNodes)
	elapsed := time.Since(start)

	// Store all settled distances and predecessors (not just city nodes)
	// so path reconstruction can follow roads that leave and re-enter the city
	distances := make(map[int]float64, len(ds.tent))
	for nodeID, d := range ds.tent {
		distances[nodeID] = d
	}

	cameFrom := make(map[int]int, len(ds.cameFrom))
	for nodeID, pred := range ds.cameFrom {
		cameFrom[nodeID] = pred
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	fmt.Printf("[FORWARD-DS] city=%s entry=%d delta=%.6f settled=%d reachable=%d time=%v heapMB=%.1f\n",
		cityName, entryNodeID, delta, len(ds.settled), len(distances), elapsed, float64(mem.HeapAlloc)/(1024*1024))

	return &ForwardSearchResult{
		EntryNodeID: entryNodeID,
		CityName:    cityName,
		Distances:   distances,
		CameFrom:    cameFrom,
		ComputedAt:  time.Now(),
		TTL:         ttl,
	}
}

// ComputeAllForwardSearchesDelta computes forward searches from all entry points in parallel
func ComputeAllForwardSearchesDelta(g *graph.Graph, cityName string, entryPoints []int, cityNodes map[int]bool, ttl time.Duration) []*ForwardSearchResult {
	results := make([]*ForwardSearchResult, len(entryPoints))
	var wg sync.WaitGroup

	for i, entryID := range entryPoints {
		wg.Add(1)
		go func(idx int, entryNodeID int) {
			defer wg.Done()
			results[idx] = ComputeForwardSearchDelta(g, entryNodeID, cityName, cityNodes, ttl)
		}(i, entryID)
	}

	wg.Wait()
	return results
}

// ComputeAllBackwardSearchesDelta computes backward searches from all entry points in parallel
func ComputeAllBackwardSearchesDelta(g *graph.Graph, cityName string, entryPoints []int, cityNodes map[int]bool, ttl time.Duration) []*BackwardSearchResult {
	results := make([]*BackwardSearchResult, len(entryPoints))
	var wg sync.WaitGroup

	for i, entryID := range entryPoints {
		wg.Add(1)
		go func(idx int, entryNodeID int) {
			defer wg.Done()
			results[idx] = ComputeBackwardSearchDelta(g, entryNodeID, cityName, cityNodes, ttl)
		}(i, entryID)
	}

	wg.Wait()
	return results
}
