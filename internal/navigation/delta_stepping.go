package navigation

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"waze/internal/graph"
)

// relaxRequest represents a pending edge relaxation
type relaxRequest struct {
	node int     // target node (graph node ID)
	dist float64 // proposed distance
	from int     // predecessor (graph node ID)
}

// deltaStepState holds all state for a single Delta-Stepping invocation
// Uses maps instead of full arrays to only allocate for visited nodes
type deltaStepState struct {
	g     *graph.Graph
	delta float64
	nCPUs int

	tent     map[int]float64 // node ID → tentative distance
	cameFrom map[int]int     // node ID → predecessor node ID
	settled  map[int]bool    // node ID → whether finalized
	buckets  map[int][]int   // bucket index → list of node IDs
}

func newDeltaStepState(g *graph.Graph, srcId int, delta float64, nCPUs int) *deltaStepState {
	tent := make(map[int]float64)
	tent[srcId] = 0

	buckets := make(map[int][]int)
	buckets[0] = []int{srcId}

	return &deltaStepState{
		g:        g,
		delta:    delta,
		nCPUs:    nCPUs,
		tent:     tent,
		cameFrom: make(map[int]int),
		settled:  make(map[int]bool),
		buckets:  buckets,
	}
}

// getTent returns the tentative distance for a node (infinity if not set)
func (ds *deltaStepState) getTent(nodeID int) float64 {
	if d, ok := ds.tent[nodeID]; ok {
		return d
	}
	return math.Inf(1)
}

// run executes the Delta-Stepping algorithm. dstId=-1 means compute all (SSSP).
func (ds *deltaStepState) run(dstId int) {
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

		// Collect all nodes settled from this bucket
		allSettled := make([]int, 0)

		// Light phase: process bucket repeatedly until empty
		for {
			nodes, exists := ds.buckets[currentBucket]
			if !exists || len(nodes) == 0 {
				break
			}

			// Take snapshot and clear bucket
			R := make([]int, len(nodes))
			copy(R, nodes)
			ds.buckets[currentBucket] = ds.buckets[currentBucket][:0]

			// Filter out already settled and stale nodes
			active := R[:0]
			for _, u := range R {
				if ds.settled[u] {
					continue
				}
				// Skip stale: node's current distance puts it in a different bucket
				if int(ds.getTent(u)/ds.delta) != currentBucket {
					continue
				}
				active = append(active, u)
			}
			if len(active) == 0 {
				break
			}

			allSettled = append(allSettled, active...)

			// Parallel: scan light edges
			requests := ds.parallelScanEdges(active, true)

			// Apply requests
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

		// Mark settled
		for _, node := range allSettled {
			ds.settled[node] = true
		}

		// Early termination
		if dstId >= 0 && ds.settled[dstId] {
			break
		}

		// Heavy phase: deduplicate settled nodes then scan heavy edges
		seen := make(map[int]bool, len(allSettled))
		unique := make([]int, 0, len(allSettled))
		for _, node := range allSettled {
			if !seen[node] {
				seen[node] = true
				unique = append(unique, node)
			}
		}

		requests := ds.parallelScanEdges(unique, false)
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

// parallelScanEdges scans edges from the given nodes in parallel.
// If isLight=true, only scans edges with weight < delta. Otherwise weight >= delta.
func (ds *deltaStepState) parallelScanEdges(nodes []int, isLight bool) []relaxRequest {
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

					v := edge.To
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

// computeDelta calculates an adaptive delta based on straight-line distance between src and dst
func computeDelta(srcNode, dstNode *graph.Node, fallback float64) float64 {
	const targetBuckets = 50
	estimatedTime := heuristic(srcNode, dstNode) / V_REF
	delta := estimatedTime / targetBuckets
	// Minimum delta floor to avoid thousands of tiny buckets
	const minDelta = 0.001 // ~3.6 seconds of travel time
	if delta < minDelta {
		delta = minDelta
	}
	return delta
}

// FindPathDeltaStepping finds shortest path using parallel Delta-Stepping algorithm
func FindPathDeltaStepping(g *graph.Graph, srcId, dstId int) (*PathResult, error) {
	srcNode, ok1 := g.Nodes[srcId]
	dstNode, ok2 := g.Nodes[dstId]
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("one of the nodes does not exist inside the graph")
	}

	delta := computeDelta(srcNode, dstNode, g.DefaultDelta)

	nCPUs := runtime.GOMAXPROCS(0)
	if nCPUs < 1 {
		nCPUs = 1
	}

	ds := newDeltaStepState(g, srcId, delta, nCPUs)

	// start := time.Now()
	ds.run(dstId)
	// elapsed := time.Since(start)

	// Memory stats
	// var mem runtime.MemStats
	// runtime.ReadMemStats(&mem)

	// fmt.Printf("[DELTA-STEP] src=%d dst=%d delta=%.6f settled=%d buckets=%d time=%v heapMB=%.1f\n",
	// 	srcId, dstId, delta, len(ds.settled), len(ds.buckets), elapsed, float64(mem.HeapAlloc)/(1024*1024))

	// Check if destination was reached
	if _, reached := ds.tent[dstId]; !reached {
		return nil, fmt.Errorf("No path found between %d and %d", srcId, dstId)
	}

	// Reconstruct path using existing functions
	route := reconstructRoute(ds.cameFrom, dstId, g)
	routeNodes := reconstructRouteNodes(ds.cameFrom, dstId)
	distance := calcDist(g, route)
	eta := ds.tent[dstId] * 60 // hours to minutes

	return &PathResult{
		Route:      route,
		RouteNodes: routeNodes,
		Distance:   distance,
		ETA:        eta,
	}, nil
}
