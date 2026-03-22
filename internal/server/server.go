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

// calculateCarPositions samples ~10% of cars for GUI display
func (s *Server) calculateCarPositions(reports []types.TrafficReport) []CarPosition {
	positions := make([]CarPosition, 0, len(reports)/10+1)

	for _, report := range reports {
		if report.CarID == -1 {
			continue
		}
		// Show ~10% of cars consistently by ID
		if len(reports) >= 100 && report.CarID%10 != 0 {
			continue
		}

		edge, exists := s.Graph.Edges[report.EdgeID]
		if !exists {
			continue
		}

		fromNode := s.Graph.Nodes[edge.From]
		toNode := s.Graph.Nodes[edge.To]

		progress := 0.5
		x := fromNode.X + (toNode.X-fromNode.X)*progress
		y := fromNode.Y + (toNode.Y-fromNode.Y)*progress

		pos := CarPosition{
			CarID:    report.CarID,
			EdgeID:   report.EdgeID,
			Progress: progress,
			Speed:    report.Speed,
			X:        x,
			Y:        y,
		}

		// Convert route edges to coordinates for GUI
		if len(report.RouteEdges) > 0 {
			route := make([][]float64, 0, len(report.RouteEdges)+1)
			for _, edgeID := range report.RouteEdges {
				if e, ok := s.Graph.Edges[edgeID]; ok {
					fn := s.Graph.Nodes[e.From]
					if len(route) == 0 {
						route = append(route, []float64{fn.X, fn.Y})
					}
					tn := s.Graph.Nodes[e.To]
					route = append(route, []float64{tn.X, tn.Y})
				}
			}
			pos.Route = route
		}

		positions = append(positions, pos)
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
