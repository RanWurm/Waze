package navigation

import (
	"container/heap"
	"waze/internal/graph"
)

// AstarSearchResult holds the result of a single-source A* search on the reversed graph.
type AstarSearchResult struct {
	tent     map[int]float64 // node ID -> tentative distance (time in hours)
	cameFrom map[int]int     // node ID -> predecessor node ID
	settled  map[int]bool    // node ID -> whether the node was settled
}

// ComputeInterCitySearchAstar runs A* on the reverse graph from srcNodeID to targetNodeID
// with no city filter. Used for inter-city routing via virtual nodes.
func ComputeInterCitySearchAstar(g *graph.Graph, srcNodeID int, targetNodeID int) *AstarSearchResult {
	result := &AstarSearchResult{
		tent:     make(map[int]float64),
		cameFrom: make(map[int]int),
		settled:  make(map[int]bool),
	}

	targetNode, ok := g.Nodes[targetNodeID]
	if !ok {
		return result
	}

	pq := newPriorityQueue()
	heap.Init(pq)

	result.tent[srcNodeID] = 0

	var h0 float64
	if srcNode, exists := g.Nodes[srcNodeID]; exists {
		h0 = heuristic(srcNode, targetNode) / V_REF
	}

	heap.Push(pq, &AstarNode{
		NodeId:   srcNodeID,
		Gscore:   0,
		Priority: h0,
	})

	for pq.Len() > 0 {
		current := heap.Pop(pq).(*AstarNode)
		u := current.NodeId

		if result.settled[u] {
			continue
		}
		result.settled[u] = true

		if u == targetNodeID {
			break
		}

		uDist := result.tent[u]

		reverseEdges, exists := g.ReverseAdjList[u]
		if !exists {
			continue
		}

		for _, edge := range reverseEdges {
			v := edge.From

			if result.settled[v] {
				continue
			}

			speed := edge.GetCurrentSpeed()
			if speed <= 0 {
				speed = 1.0
			}
			timeCost := edge.Length / speed
			newDist := uDist + timeCost

			oldDist, exists := result.tent[v]
			if !exists || newDist < oldDist {
				result.tent[v] = newDist
				result.cameFrom[v] = u

				var h float64
				if vNode, nodeExists := g.Nodes[v]; nodeExists {
					h = heuristic(vNode, targetNode) / V_REF
				}
				f := newDist + h

				if _, inQueue := pq.index[v]; inQueue {
					pq.Update(v, f, newDist)
				} else {
					heap.Push(pq, &AstarNode{
						NodeId:   v,
						Gscore:   newDist,
						Priority: f,
					})
				}
			}
		}
	}

	return result
}
