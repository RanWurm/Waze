package server

import (
	"log"
	"math"
	"time"
	"waze/internal/graph"
	"waze/internal/navigation"
	"waze/internal/types"
)

const (
	shortDistThreshold = 0.03 // Below this → A* is faster
	longDistThreshold  = 0.10 // At or above this → A* is faster
)

var JobQueue chan PathRequest
var GlobalRouter *navigation.EntryPointRouter

func WakeWorkers(numWorkers int, g *graph.Graph) {
	JobQueue = make(chan PathRequest, 100)

	// Initialize Entry Point Router with 2 minute cache TTL
	GlobalRouter = navigation.NewEntryPointRouter(g, 2*time.Minute)

	// Initialize cities for Gush Dan area
	navigation.InitializeGushDanCities(GlobalRouter)

	for i := 0; i < numWorkers; i++ {
		go worker(g)
	}
	log.Printf("JobQueue started with %d workers using entry point routing", numWorkers)
}

func worker(g *graph.Graph) {
	for req := range JobQueue {
		// Use entry point routing if available
		var pathRes *navigation.PathResult
		var err error

		// Hybrid approach: use A* for short and long distances, EntryPoint for mid
		dist := euclideanDist(g, req.StartNodeId, req.EndNodeId)
		if dist < shortDistThreshold || dist >= longDistThreshold {
			pathRes, err = navigation.FindPathAstar(g, req.StartNodeId, req.EndNodeId)
		} else if GlobalRouter != nil {
			pathRes, err = GlobalRouter.FindPathWithEntryPoints(req.StartNodeId, req.EndNodeId)
		} else {
			pathRes, err = navigation.FindPathDeltaStepping(g, req.StartNodeId, req.EndNodeId)
		}

		result := PathResult{}
		if err != nil {
			result.Err = err
		} else {
			// Handle both edge-based routes (new) and node-based routes (fallback)
			if len(pathRes.Route) > 0 {
				result.Response.RouteNodes = pathRes.Route
			} else if len(pathRes.RouteNodes) > 0 {
				// Convert node path to edge path
				result.Response.RouteNodes = convertNodePathToEdgePath(g, pathRes.RouteNodes)
			}

			result.Response.ETA = pathRes.ETA
			result.Response.Distance = pathRes.Distance

			// Populate full edge data for frontend
			result.Response.RouteEdges = make([]types.EdgeInfo, len(result.Response.RouteNodes))
			for i, edgeID := range result.Response.RouteNodes {
				edge := g.Edges[edgeID]
				fromNode := g.Nodes[edge.From]
				toNode := g.Nodes[edge.To]
				result.Response.RouteEdges[i] = types.EdgeInfo{
					ID:         edge.Id,
					From:       edge.From,
					To:         edge.To,
					FromX:      fromNode.X,
					FromY:      fromNode.Y,
					ToX:        toNode.X,
					ToY:        toNode.Y,
					Length:     edge.Length,
					SpeedLimit: edge.SpeedLimit,
				}
			}
		}
		req.ResponseChannel <- result
	}
}

func euclideanDist(g *graph.Graph, src, dst int) float64 {
	ns := g.Nodes[src]
	nd := g.Nodes[dst]
	dx := ns.X - nd.X
	dy := ns.Y - nd.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// convertNodePathToEdgePath converts a path of node IDs to a path of edge IDs
func convertNodePathToEdgePath(g *graph.Graph, nodePath []int) []int {
	if len(nodePath) < 2 {
		return []int{}
	}

	edgePath := make([]int, 0, len(nodePath)-1)
	for i := 0; i < len(nodePath)-1; i++ {
		u := nodePath[i]
		v := nodePath[i+1]

		// Find edge from u to v
		for _, edge := range g.GetNeighbors(u) {
			if edge.To == v {
				edgePath = append(edgePath, edge.Id)
				break
			}
		}
	}

	return edgePath
}