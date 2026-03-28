package server

import (
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"sync"
	"time"
	"waze/internal/graph"
	"waze/internal/navigation"
	"waze/internal/types"
)

const (
	shortDistThreshold = 0.03 // Below this → A* is faster
	longDistThreshold  = 0.10 // At or above this → A* is faster
)

// RoutingMode controls which algorithm is used
// "hybrid" = original A* + EntryPoint hybrid
// "bidir" = plain Bidirectional A*
// "bidir_ep" = Bidirectional A* with EntryPoint routing
var RoutingMode = "hybrid"
var CacheEnabled = true

var JobQueue chan PathRequest
var GlobalRouter *navigation.EntryPointRouter
var GlobalBidirRouter *navigation.BidirEntryPointRouter

// Timing tracking
var routeTimings []RouteTimingRecord
var timingMu sync.Mutex

type RouteTimingRecord struct {
	QueryID   int
	Src       int
	Dst       int
	Ms        float64
	Algorithm string
}

var queryCounter int

func WakeWorkers(numWorkers int, g *graph.Graph) {
	JobQueue = make(chan PathRequest, 100)

	// Initialize Entry Point Router with 2 minute cache TTL
	GlobalRouter = navigation.NewEntryPointRouter(g, 2*time.Minute)
	navigation.InitializeGushDanCities(GlobalRouter)

	// Initialize Bidir Entry Point Router
	GlobalBidirRouter = navigation.NewBidirEntryPointRouter(g, 2*time.Minute)
	navigation.InitializeBidirGushDanCities(GlobalBidirRouter)

	for i := 0; i < numWorkers; i++ {
		go worker(g)
	}
	log.Printf("JobQueue started with %d workers, routing mode: %s", numWorkers, RoutingMode)
}

func worker(g *graph.Graph) {
	for req := range JobQueue {
		// Use entry point routing if available
		var pathRes *navigation.PathResult
		var err error

		// Validate that both nodes exist
		if g.Nodes[req.StartNodeId] == nil || g.Nodes[req.EndNodeId] == nil {
			result := PathResult{Err: fmt.Errorf("invalid node ID: %d or %d does not exist", req.StartNodeId, req.EndNodeId)}
			req.ResponseChannel <- result
			continue
		}

		// Start timing
		startTime := time.Now()

		// Route based on RoutingMode
		switch RoutingMode {
		case "bidir":
			// Plain Bidirectional A*
			pathRes, err = navigation.FindPathBidirectionalAstar(g, req.StartNodeId, req.EndNodeId)
		case "bidir_ep":
			// Bidirectional A* with EntryPoint routing
			pathRes, err = GlobalBidirRouter.FindPathWithEntryPoints(req.StartNodeId, req.EndNodeId)
		case "bidir_hybrid":
			// Hybrid: Bidirectional A* for short, Bidirectional EntryPoint otherwise
			dist := euclideanDist(g, req.StartNodeId, req.EndNodeId)
			if dist < shortDistThreshold {
				pathRes, err = navigation.FindPathBidirectionalAstar(g, req.StartNodeId, req.EndNodeId)
			} else {
				pathRes, err = GlobalBidirRouter.FindPathWithEntryPoints(req.StartNodeId, req.EndNodeId)
			}
		default:
			// "hybrid" - original behavior: A* for short/long, EntryPoint for mid
			dist := euclideanDist(g, req.StartNodeId, req.EndNodeId)
			if dist < shortDistThreshold || dist >= longDistThreshold {
				pathRes, err = navigation.FindPathAstar(g, req.StartNodeId, req.EndNodeId)
			} else if GlobalRouter != nil {
				pathRes, err = GlobalRouter.FindPathWithEntryPoints(req.StartNodeId, req.EndNodeId)
			} else {
				pathRes, err = navigation.FindPathDeltaStepping(g, req.StartNodeId, req.EndNodeId)
			}
		}

		// Record timing
		elapsed := float64(time.Since(startTime).Microseconds()) / 1000.0
		timingMu.Lock()
		queryCounter++
		routeTimings = append(routeTimings, RouteTimingRecord{
			QueryID:   queryCounter,
			Src:       req.StartNodeId,
			Dst:       req.EndNodeId,
			Ms:        elapsed,
			Algorithm: RoutingMode,
		})
		timingMu.Unlock()

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

// SaveTimingsToCSV saves all route timing records to a CSV file
func SaveTimingsToCSV() {
	timingMu.Lock()
	defer timingMu.Unlock()

	if len(routeTimings) == 0 {
		log.Println("No route timings to save")
		return
	}

	// Create dedicated folder for server timings
	os.MkdirAll("benchmarks/server", 0755)
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	cacheStatus := "cache"
	if !CacheEnabled {
		cacheStatus = "nocache"
	}
	filename := fmt.Sprintf("benchmarks/server/%s_%s_%s.csv", RoutingMode, cacheStatus, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		log.Printf("Failed to create timings CSV: %v", err)
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"query_id", "src", "dst", "ms", "algorithm"})
	for _, r := range routeTimings {
		w.Write([]string{
			strconv.Itoa(r.QueryID),
			strconv.Itoa(r.Src),
			strconv.Itoa(r.Dst),
			fmt.Sprintf("%.3f", r.Ms),
			r.Algorithm,
		})
	}
	w.Flush()

	log.Printf("Saved %d route timings to %s", len(routeTimings), filename)
}

// GetTimingStats returns summary statistics
func GetTimingStats() (count int, totalMs float64, avgMs float64) {
	timingMu.Lock()
	defer timingMu.Unlock()

	count = len(routeTimings)
	for _, r := range routeTimings {
		totalMs += r.Ms
	}
	if count > 0 {
		avgMs = totalMs / float64(count)
	}
	return
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