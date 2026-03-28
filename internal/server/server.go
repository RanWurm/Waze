package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"waze/internal/config"
	"waze/internal/graph"
	"waze/internal/types"
)

const MinBatchSizeForParallelism = 500
const MinReportsPerWorker = 500

type Server struct {
	Graph *graph.Graph

	Cache *RouteCache
}

type EdgeAggregator struct {
	SpeedSum float64
	Count    int
}

func NewServer(mapFile string) *Server {
	g, err := graph.LoadGraph(mapFile)
	if err != nil {
		log.Fatal(err)
	}
	return &Server{
		Graph: g,
		Cache: NewRouteCache(config.Global.Server.CacheTtl),
	}
}

func (s *Server) HandleTrafficBatch(w http.ResponseWriter, r *http.Request) {
	// defer config.TimeTrack(time.Now(), "HandleTrafficBatch")

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST request allowed", http.StatusMethodNotAllowed)
		return
	}

	var reports []types.TrafficReport
	if err := json.NewDecoder(r.Body).Decode(&reports); err != nil {
		http.Error(w, "Invalid Json", http.StatusBadRequest)
		return
	}
	// safety check
	reportsCount := len(reports)
	if reportsCount == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	// check if the batch size justifies parallelism
	shouldParallel := reportsCount >= MinBatchSizeForParallelism && config.Global.MaxCPUs > 1
	if shouldParallel {

		neededWorkers := reportsCount / MinReportsPerWorker

		activeWorkers := min(neededWorkers, config.Global.MaxCPUs)

		s.processReportsParallel(reports, activeWorkers)
	} else {

		s.processReportsSerial(reports)
	}
	// // מקביליות בעדכון
	// numWorkers := 8
	// reportsCount := len(reports)

	// if reportsCount == 0 {
	// 	w.WriteHeader(http.StatusOK)
	// 	return
	// }

	// if reportsCount < numWorkers {
	// 	numWorkers = 1
	// }

	// chunkSize := (reportsCount + numWorkers - 1) / numWorkers

	// var wg sync.WaitGroup
	// wg.Add(numWorkers)

	// for i := 0; i < numWorkers; i++ {
	// 	start := i * chunkSize
	// 	end := min(start+chunkSize, reportsCount)

	// 	go func(startIdx, endIdx int) {
	// 		defer wg.Done()
	// 		for j := startIdx; j < endIdx; j++ {
	// 			report := reports[j]
	// 			if edge, exists := s.Graph.Edges[report.EdgeID]; exists && report.CarID != -1 {
	// 				edge.UpdateSpeed(report.Speed)
	// 			}
	// 		}
	// 	}(start, end)
	// }

	// wg.Wait()

	// שליחת עדכון ל-GUI
	if GlobalHub != nil {
		carPositions := s.calculateCarPositions(reports)
		GlobalHub.BroadcastUpdate("cars", carPositions)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) processReportsSerial(reports []types.TrafficReport) {
	aggMap := make(map[int]*EdgeAggregator)

	for _, rep := range reports {
		if _, exists := s.Graph.Edges[rep.EdgeID]; exists && rep.CarID != -1 {
			if aggMap[rep.EdgeID] == nil {
				aggMap[rep.EdgeID] = &EdgeAggregator{}
			}
			aggMap[rep.EdgeID].SpeedSum += rep.Speed
			aggMap[rep.EdgeID].Count++
		}
	}

	s.applyAggregationToGraph(aggMap)
}

func (s *Server) processReportsParallel(reports []types.TrafficReport, numWorkers int) {
	chunckSize := (len(reports) + numWorkers - 1) / numWorkers
	var wg sync.WaitGroup

	partialResults := make([]map[int]*EdgeAggregator, numWorkers)

	for i := 0; i < numWorkers; i++ {
		start := i * chunckSize
		end := min(start+chunckSize, len(reports))

		wg.Add(1)
		go func(idx int, slice []types.TrafficReport) {
			defer wg.Done()
			localMap := make(map[int]*EdgeAggregator)

			for _, rep := range slice {
				if _, exists := s.Graph.Edges[rep.EdgeID]; exists && rep.CarID != -1 {
					if _, seen := localMap[rep.EdgeID]; !seen {
						localMap[rep.EdgeID] = &EdgeAggregator{}
					}
					localMap[rep.EdgeID].SpeedSum += rep.Speed
					localMap[rep.EdgeID].Count++
				}
			}
			partialResults[idx] = localMap
		}(i, reports[start:end])
	}
	wg.Wait()

	for _, partialMap := range partialResults {
		s.applyAggregationToGraph(partialMap)
	}
}

func (s *Server) applyAggregationToGraph(aggMap map[int]*EdgeAggregator) {
	for edgeID, agg := range aggMap {
		if edge, exists := s.Graph.Edges[edgeID]; exists {
			if edge == nil {
				fmt.Printf("Warning: Edge %d exists in map but is nil!\n", edgeID)
				continue
			}
			avgSpeed := agg.SpeedSum / float64(agg.Count)
			edge.UpdateSpeed(avgSpeed)
		}
	}
}

// calculateCarPositions converts traffic reports into positions for the 3D visualization
func (s *Server) calculateCarPositions(reports []types.TrafficReport) []CarPosition {
	positions := make([]CarPosition, 0, len(reports))

	for _, report := range reports {
		if report.CarID == -1 {
			continue
		}

		edge, exists := s.Graph.Edges[report.EdgeID]
		if !exists {
			continue
		}

		fromNode := s.Graph.Nodes[edge.From]
		toNode := s.Graph.Nodes[edge.To]

		progress := report.EdgeProgress
		if progress < 0 || progress > 1 {
			progress = 0.5
		}

		x := fromNode.X + (toNode.X-fromNode.X)*progress
		y := fromNode.Y + (toNode.Y-fromNode.Y)*progress

		positions = append(positions, CarPosition{
			CarID:    report.CarID,
			EdgeID:   report.EdgeID,
			Progress: progress,
			Speed:    report.Speed,
			X:        x,
			Y:        y,
		})
	}

	return positions
}

func (s *Server) HandleNavigation(w http.ResponseWriter, r *http.Request) {
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	fromId, err1 := strconv.Atoi(fromStr)
	toId, err2 := strconv.Atoi(toStr)

	if err1 != nil || err2 != nil {
		http.Error(w, "Invalid 'from' or 'to' parameters", http.StatusBadRequest)
		return
	}

	// check if the route is in cache
	if cachedResponse, exists := s.Cache.Get(fromId, toId); exists {
		// fmt.Printf("The route (%d -> %d) is from the cache\n", fromId, toId)
		// send response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cachedResponse)
		return
	} else {
		// else create a request to calculate the route
		req := PathRequest{
			StartNodeId:     fromId,
			EndNodeId:       toId,
			ResponseChannel: make(chan PathResult),
		}
		// send request
		JobQueue <- req

		// wait for result
		result := <-req.ResponseChannel

		// if no route was found
		if result.Err != nil {
			fmt.Printf("The Error is: %s\n", result.Err.Error())
			http.Error(w, result.Err.Error(), http.StatusNotFound)
			return
		}

		// store in cache
		s.Cache.Set(fromId, toId, result.Response)

		// send response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result.Response)
	}
}

// HandleSaveTimings saves route timing data to CSV
func (s *Server) HandleGraph(w http.ResponseWriter, r *http.Request) {
	type GraphResponse struct {
		Nodes []float64 `json:"nodes"`
		Edges []float64 `json:"edges"`
	}

	numNodes := len(s.Graph.IndexNode)
	nodes := make([]float64, numNodes*2)
	for i, nodeId := range s.Graph.IndexNode {
		node := s.Graph.Nodes[nodeId]
		nodes[i*2] = node.X
		nodes[i*2+1] = node.Y
	}

	edges := make([]float64, 0, len(s.Graph.Edges)*4)
	for _, edge := range s.Graph.Edges {
		fromIdx := s.Graph.NodeIndex[edge.From]
		toIdx := s.Graph.NodeIndex[edge.To]
		edges = append(edges, float64(fromIdx), float64(toIdx), edge.SpeedLimit, edge.Length)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(GraphResponse{Nodes: nodes, Edges: edges})
}

func (s *Server) HandleSaveTimings(w http.ResponseWriter, r *http.Request) {
	SaveTimingsToCSV()
	count, totalMs, avgMs := GetTimingStats()
	response := map[string]interface{}{
		"count":    count,
		"total_ms": totalMs,
		"avg_ms":   avgMs,
		"mode":     RoutingMode,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
