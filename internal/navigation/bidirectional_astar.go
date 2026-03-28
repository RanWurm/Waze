package navigation

import (
	"container/heap"
	"fmt"
	"slices"
	"waze/internal/graph"
)

// FindPathBidirectionalAstar performs bidirectional A* search
// It runs A* from both source and destination simultaneously,
// meeting in the middle for improved performance on long routes
func FindPathBidirectionalAstar(g *graph.Graph, srcId, dstId int) (*PathResult, error) {
	srcNode, ok1 := g.Nodes[srcId]
	dstNode, ok2 := g.Nodes[dstId]

	if !ok1 || !ok2 {
		return nil, fmt.Errorf("one of the nodes does not exist inside the graph")
	}

	// Forward search structures (from source)
	fwdPQ := newPriorityQueue()
	heap.Init(fwdPQ)
	fwdGScore := make(map[int]float64)
	fwdGScore[srcId] = 0
	fwdCameFrom := make(map[int]int)
	fwdClosed := make(map[int]bool)

	// Backward search structures (from destination)
	bwdPQ := newPriorityQueue()
	heap.Init(bwdPQ)
	bwdGScore := make(map[int]float64)
	bwdGScore[dstId] = 0
	bwdCameFrom := make(map[int]int)
	bwdClosed := make(map[int]bool)

	// Initialize forward search
	heap.Push(fwdPQ, &AstarNode{
		NodeId:   srcId,
		Gscore:   0,
		Priority: heuristic(srcNode, dstNode) / V_REF,
	})

	// Initialize backward search
	heap.Push(bwdPQ, &AstarNode{
		NodeId:   dstId,
		Gscore:   0,
		Priority: heuristic(dstNode, srcNode) / V_REF,
	})

	// Best meeting point found so far
	bestMu := float64(1e18) // best total path cost
	meetNode := -1

	for fwdPQ.Len() > 0 && bwdPQ.Len() > 0 {
		// Get minimum f-values from both queues
		fwdMin := fwdPQ.items[0].Priority
		bwdMin := bwdPQ.items[0].Priority

		// Termination condition: if the best path found is <= min of both queues
		if bestMu <= fwdMin+bwdMin {
			break
		}

		// Expand the search with smaller f-value
		if fwdMin <= bwdMin {
			// Expand forward
			current := heap.Pop(fwdPQ).(*AstarNode)
			u := current.NodeId

			if fwdClosed[u] {
				continue
			}
			fwdClosed[u] = true

			// Check if backward search has reached this node
			if bwdClosed[u] {
				totalCost := fwdGScore[u] + bwdGScore[u]
				if totalCost < bestMu {
					bestMu = totalCost
					meetNode = u
				}
			}

			// Expand neighbors (forward direction)
			for _, edge := range g.GetNeighbors(u) {
				v := edge.To
				if fwdClosed[v] {
					continue
				}

				speed := edge.GetCurrentSpeed()
				if speed <= 0 {
					speed = 1.0
				}
				timeCost := edge.Length / speed
				newGscore := fwdGScore[u] + timeCost

				oldScore, exists := fwdGScore[v]
				if !exists || newGscore < oldScore {
					fwdGScore[v] = newGscore
					fwdCameFrom[v] = u

					h := heuristic(g.Nodes[v], dstNode) / V_REF
					f := newGscore + h

					if _, exists := fwdPQ.index[v]; exists {
						fwdPQ.Update(v, f, newGscore)
					} else {
						heap.Push(fwdPQ, &AstarNode{
							NodeId:   v,
							Gscore:   newGscore,
							Priority: f,
						})
					}

					// Check if backward search has reached this node
					if bwdScore, found := bwdGScore[v]; found {
						totalCost := newGscore + bwdScore
						if totalCost < bestMu {
							bestMu = totalCost
							meetNode = v
						}
					}
				}
			}
		} else {
			// Expand backward
			current := heap.Pop(bwdPQ).(*AstarNode)
			u := current.NodeId

			if bwdClosed[u] {
				continue
			}
			bwdClosed[u] = true

			// Check if forward search has reached this node
			if fwdClosed[u] {
				totalCost := fwdGScore[u] + bwdGScore[u]
				if totalCost < bestMu {
					bestMu = totalCost
					meetNode = u
				}
			}

			// Expand neighbors (backward direction - use reverse adjacency list)
			for _, edge := range g.ReverseAdjList[u] {
				v := edge.From // go backwards
				if bwdClosed[v] {
					continue
				}

				speed := edge.GetCurrentSpeed()
				if speed <= 0 {
					speed = 1.0
				}
				timeCost := edge.Length / speed
				newGscore := bwdGScore[u] + timeCost

				oldScore, exists := bwdGScore[v]
				if !exists || newGscore < oldScore {
					bwdGScore[v] = newGscore
					bwdCameFrom[v] = u

					h := heuristic(g.Nodes[v], srcNode) / V_REF
					f := newGscore + h

					if _, exists := bwdPQ.index[v]; exists {
						bwdPQ.Update(v, f, newGscore)
					} else {
						heap.Push(bwdPQ, &AstarNode{
							NodeId:   v,
							Gscore:   newGscore,
							Priority: f,
						})
					}

					// Check if forward search has reached this node
					if fwdScore, found := fwdGScore[v]; found {
						totalCost := newGscore + fwdScore
						if totalCost < bestMu {
							bestMu = totalCost
							meetNode = v
						}
					}
				}
			}
		}
	}

	if meetNode == -1 {
		return nil, fmt.Errorf("no path found between %d and %d", srcId, dstId)
	}

	// Reconstruct the path (both edge IDs and node IDs)
	route, routeNodes := reconstructBidirectionalRoute(fwdCameFrom, bwdCameFrom, meetNode, srcId, dstId, g)
	distance := calcDist(g, route)

	return &PathResult{
		Route:      route,
		RouteNodes: routeNodes,
		ETA:        bestMu * 60, // convert to minutes
		Distance:   distance,
	}, nil
}

// reconstructBidirectionalRoute builds the full path from src to dst through the meeting point
// Returns both edge IDs and node IDs
func reconstructBidirectionalRoute(fwdCameFrom, bwdCameFrom map[int]int, meetNode, srcId, dstId int, g *graph.Graph) ([]int, []int) {
	// Build forward path: src -> meetNode (nodes and edges)
	fwdEdges := []int{}
	fwdNodes := []int{}
	current := meetNode
	for current != srcId {
		fwdNodes = append(fwdNodes, current)
		prev, ok := fwdCameFrom[current]
		if !ok {
			break
		}
		// Find edge from prev to current
		for _, edge := range g.GetNeighbors(prev) {
			if edge.To == current {
				fwdEdges = append(fwdEdges, edge.Id)
				break
			}
		}
		current = prev
	}
	fwdNodes = append(fwdNodes, srcId)
	slices.Reverse(fwdEdges)
	slices.Reverse(fwdNodes)

	// Build backward path: meetNode -> dst (nodes and edges)
	bwdEdges := []int{}
	bwdNodes := []int{}
	current = meetNode
	for current != dstId {
		next, ok := bwdCameFrom[current]
		if !ok {
			break
		}
		// Find edge from current to next
		for _, edge := range g.GetNeighbors(current) {
			if edge.To == next {
				bwdEdges = append(bwdEdges, edge.Id)
				break
			}
		}
		bwdNodes = append(bwdNodes, next)
		current = next
	}

	// Combine paths
	edges := append(fwdEdges, bwdEdges...)
	nodes := append(fwdNodes, bwdNodes...)
	return edges, nodes
}
