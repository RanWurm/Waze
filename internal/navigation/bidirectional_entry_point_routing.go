package navigation

import (
	"fmt"
	"math"
	"sync"
	"time"
	"waze/internal/graph"
)

// BidirEntryPointRouter handles routing using entry points and cached searches
type BidirEntryPointRouter struct {
	Graph             *graph.Graph
	EntryPointManager *EntryPointManager
	BackwardCache     *BackwardSearchCache
	ForwardCache      *ForwardSearchCache
	InterCityCache    *InterCityCache
	CacheTTL          time.Duration
	CachingEnabled    bool
	mu                sync.RWMutex
}

func NewBidirEntryPointRouter(g *graph.Graph, cacheTTL time.Duration) *BidirEntryPointRouter {
	return &BidirEntryPointRouter{
		Graph:             g,
		EntryPointManager: NewEntryPointManager(),
		BackwardCache:     NewBackwardSearchCache(cacheTTL),
		ForwardCache:      NewForwardSearchCache(cacheTTL),
		InterCityCache:    NewInterCityCache(cacheTTL),
		CacheTTL:          cacheTTL,
		CachingEnabled:    cacheTTL > time.Second,
	}
}

// FindPathWithEntryPoints finds shortest path using entry point strategy
func (epr *BidirEntryPointRouter) FindPathWithEntryPoints(srcID, dstID int) (*PathResult, error) {
	_, ok1 := epr.Graph.Nodes[srcID]
	_, ok2 := epr.Graph.Nodes[dstID]

	if !ok1 || !ok2 {
		return nil, fmt.Errorf("one of the nodes does not exist inside the graph")
	}

	dstCity, dstInCity := epr.EntryPointManager.GetCity(dstID)
	srcCity, srcInCity := epr.EntryPointManager.GetCity(srcID)

	// Same city or not in a city: direct Delta-Stepping
	if !dstInCity || !srcInCity || srcCity == dstCity {
		return FindPathBidirectionalAstar(epr.Graph, srcID, dstID)
	}

	srcCityObj := epr.EntryPointManager.Cities[srcCity]
	dstCityObj := epr.EntryPointManager.Cities[dstCity]

	if srcCityObj.ForwardVirtualNodeID == 0 || dstCityObj.ForwardVirtualNodeID == 0 {
		return FindPathBidirectionalAstar(epr.Graph, srcID, dstID)
	}

	// Step 1: inter-city search on reverse graph
	// From dest_forward to src_reversed — finds shortest path between any src entry and any dest entry
	var interCity *InterCityResult
	if epr.CachingEnabled {
		if cached, found := epr.InterCityCache.Get(srcCity, dstCity); found {
			interCity = cached
		}
	}
	if interCity == nil {
		ds := ComputeInterCitySearchAstar(epr.Graph, dstCityObj.ForwardVirtualNodeID, srcCityObj.ReversedVirtualNodeID)
		if !ds.settled[srcCityObj.ReversedVirtualNodeID] {
			return FindPathBidirectionalAstar(epr.Graph, srcID, dstID)
		}
		interCity = &InterCityResult{
			SrcCity:    srcCity,
			DstCity:    dstCity,
			Tent:       ds.tent,
			CameFrom:   ds.cameFrom,
			ComputedAt: time.Now(),
			TTL:        epr.CacheTTL,
		}
		if epr.CachingEnabled {
			epr.InterCityCache.Set(interCity)
		}
	}

	// Step 2: dest node to dest_reversed on reverse graph
	// Finds shortest path from destination to its nearest entry point
	step2 := ComputeInterCitySearchAstar(epr.Graph, dstID, dstCityObj.ReversedVirtualNodeID)
	if !step2.settled[dstCityObj.ReversedVirtualNodeID] {
		return FindPathBidirectionalAstar(epr.Graph, srcID, dstID)
	}

	// Step 3: src_entries_forward to src node on reverse graph
	// Finds shortest path from source to its nearest entry, cached
	step3Key := srcCity + "|self"
	var step3 *InterCityResult
	if epr.CachingEnabled {
		if cached, found := epr.InterCityCache.Get(step3Key, srcCity); found {
			step3 = cached
		}
	}
	if step3 == nil {
		ds := ComputeInterCitySearchAstar(epr.Graph, srcCityObj.ForwardVirtualNodeID, srcID)
		step3 = &InterCityResult{
			SrcCity:    step3Key,
			DstCity:    srcCity,
			Tent:       ds.tent,
			CameFrom:   ds.cameFrom,
			ComputedAt: time.Now(),
			TTL:        epr.CacheTTL,
		}
		if epr.CachingEnabled {
			epr.InterCityCache.Set(step3)
		}
	}

	// Step 4: heuristic comparison
	costSrcToNearestEntry := step3.Tent[srcID]
	costDestToEntry := step2.tent[dstCityObj.ReversedVirtualNodeID]
	costBestCrossing := interCity.Tent[srcCityObj.ReversedVirtualNodeID]

	// Find which src entry the source naturally goes through
	nearestSrcEntry := traceToEntryBidir(step3.CameFrom, srcID)
	costNearestCrossing := interCity.Tent[nearestSrcEntry]

	routeA := costSrcToNearestEntry + costNearestCrossing + costDestToEntry
	routeB := costBestCrossing + costDestToEntry

	threshold := 100.0 / 3600.0 // 100 seconds in hours

	var fullPath []int

	if routeB < routeA-threshold {
		// Nearest exit isn't optimal, recalculate per entry point
		bestTotal := math.Inf(1)
		var bestSrcToEntry *PathResult
		bestEntry := -1

		for _, entry := range srcCityObj.EntryPoints {
			result, err := FindPathBidirectionalAstar(epr.Graph, srcID, entry)
			if err != nil {
				continue
			}
			srcToEntry := result.ETA / 60 // minutes to hours
			total := srcToEntry + interCity.Tent[entry] + costDestToEntry
			if total < bestTotal {
				bestTotal = total
				bestSrcToEntry = result
				bestEntry = entry
			}
		}

		if bestEntry == -1 {
			return FindPathBidirectionalAstar(epr.Graph, srcID, dstID)
		}

		seg1 := bestSrcToEntry.RouteNodes
		seg2 := tracePathSkipVirtualBidir(interCity.CameFrom, bestEntry)
		seg3 := tracePathSkipVirtualBidir(step2.cameFrom, dstCityObj.ReversedVirtualNodeID)
		fullPath = stitchPathsBidir(seg1, seg2, seg3)
	} else {
		// Natural route via nearest exit is good enough
		seg1 := tracePathSkipVirtualBidir(step3.CameFrom, srcID)
		seg2 := tracePathSkipVirtualBidir(interCity.CameFrom, srcCityObj.ReversedVirtualNodeID)
		seg3 := tracePathSkipVirtualBidir(step2.cameFrom, dstCityObj.ReversedVirtualNodeID)
		fullPath = stitchPathsBidir(seg1, seg2, seg3)
	}

	if len(fullPath) == 0 {
		return FindPathBidirectionalAstar(epr.Graph, srcID, dstID)
	}

	totalDistance := 0.0
	totalETA := 0.0
	for i := 0; i < len(fullPath)-1; i++ {
		for _, edge := range epr.Graph.GetNeighbors(fullPath[i]) {
			if edge.To == fullPath[i+1] {
				totalDistance += edge.Length
				speed := edge.GetCurrentSpeed()
				if speed <= 0 {
					speed = 1.0
				}
				totalETA += edge.Length / speed
				break
			}
		}
	}

	return &PathResult{
		RouteNodes: fullPath,
		Distance:   totalDistance,
		ETA:        totalETA * 60, // hours to minutes
	}, nil
}

// getOrComputeBackwardSearches retrieves cached backward searches or computes them
func (epr *BidirEntryPointRouter) getOrComputeBackwardSearches(cityName string, entryPoints []int) []*BackwardSearchResult {
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

	// if cachedCount > 0 {
	// 	fmt.Printf("[CACHE HIT] Reusing %d/%d cached backward searches for %s\n", cachedCount, len(entryPoints), cityName)
	// }

	// Compute missing backward searches in parallel
	if len(needsCompute) > 0 {
		//fmt.Printf("[COMPUTING] Computing %d backward searches in parallel for %s from entries %v\n", len(needsCompute), cityName, needsCompute)
		cityNodes := epr.getCityNodes(cityName)
		computed := ComputeAllBackwardSearchesDelta(epr.Graph, cityName, needsCompute, cityNodes, epr.CacheTTL)

		// Store in cache and results
		for i, result := range computed {
			epr.BackwardCache.Set(result)
			results[needsComputeIdx[i]] = result
		}
		//fmt.Printf("[CACHED] Stored %d new backward searches for %s (TTL: %v)\n", len(computed), cityName, epr.CacheTTL)
	}

	return results
}

// getOrComputeForwardSearches retrieves cached forward searches or computes them
func (epr *BidirEntryPointRouter) getOrComputeForwardSearches(cityName string, entryPoints []int) []*ForwardSearchResult {
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

	// if cachedCount > 0 {
	// 	fmt.Printf("[CACHE HIT] Reusing %d/%d cached forward searches for %s\n", cachedCount, len(entryPoints), cityName)
	// }

	if len(needsCompute) > 0 {
		//fmt.Printf("[COMPUTING] Computing %d forward searches in parallel for %s from entries %v\n", len(needsCompute), cityName, needsCompute)
		cityNodes := epr.getCityNodes(cityName)
		computed := ComputeAllForwardSearchesDelta(epr.Graph, cityName, needsCompute, cityNodes, epr.CacheTTL)

		for i, result := range computed {
			epr.ForwardCache.Set(result)
			results[needsComputeIdx[i]] = result
		}
		//fmt.Printf("[CACHED] Stored %d new forward searches for %s (TTL: %v)\n", len(computed), cityName, epr.CacheTTL)
	}

	return results
}

// getCityNodes returns the pre-computed map of all nodes in a city
func (epr *BidirEntryPointRouter) getCityNodes(cityName string) map[int]bool {
	if city, exists := epr.EntryPointManager.Cities[cityName]; exists {
		return city.Nodes
	}
	return make(map[int]bool)
}

// findBestPathThroughEntries finds the best path going through entry points
func (epr *BidirEntryPointRouter) findBestPathThroughEntries(srcID, dstID int, srcNode, dstNode *graph.Node, entryPoints []int, backwardResults []*BackwardSearchResult, forwardResults []*ForwardSearchResult) ([]int, float64, float64, error) {
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
				//results[idx].err = fmt.Errorf("destination not reachable from entry %d", entry)
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
	totalDistance := calcDistFromEdgesBidir(epr.Graph, fullPath)

	return fullPath, totalDistance, bestCost, nil
}

// computePathToEntry computes path from source to entry point using Delta-Stepping
func (epr *BidirEntryPointRouter) computePathToEntry(srcID, entryID int, srcNode, entryNode *graph.Node) (*PathResult, error) {
	return FindPathBidirectionalAstar(epr.Graph, srcID, entryID)
}

// reconstructPathFromForward reconstructs path from entry to destination
// using the CameFrom predecessor map from the forward search
func (epr *BidirEntryPointRouter) reconstructPathFromForward(entryID, dstID int, forwardResult *ForwardSearchResult) []int {
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

// reconstructRouteNodesBidir reconstructs the route as node IDs
func reconstructRouteNodesBidir(cameFrom map[int]int, current int) []int {
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

// traceToEntryBidir follows cameFrom from nodeID until it hits a virtual node (negative ID),
// returns the last real node (the entry point)
func traceToEntryBidir(cameFrom map[int]int, nodeID int) int {
	current := nodeID
	for {
		prev, ok := cameFrom[current]
		if !ok {
			return current
		}
		if prev < 0 {
			return current
		}
		current = prev
	}
}

// tracePathSkipVirtualBidir traces cameFrom from nodeID and returns the forward path
// with virtual nodes (negative IDs) removed
func tracePathSkipVirtualBidir(cameFrom map[int]int, nodeID int) []int {
	path := []int{}
	current := nodeID
	for {
		if current >= 0 {
			path = append(path, current)
		}
		prev, ok := cameFrom[current]
		if !ok {
			break
		}
		current = prev
	}
	return path
}

// stitchPathsBidir joins path segments, removing duplicate nodes at junctions
func stitchPathsBidir(segments ...[]int) []int {
	if len(segments) == 0 {
		return nil
	}
	result := append([]int{}, segments[0]...)
	for _, seg := range segments[1:] {
		if len(seg) == 0 {
			continue
		}
		if len(result) > 0 && result[len(result)-1] == seg[0] {
			seg = seg[1:]
		}
		result = append(result, seg...)
	}
	return result
}

// calcDistFromEdgesBidir calculates distance from node path
func calcDistFromEdgesBidir(g *graph.Graph, nodePath []int) float64 {
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
