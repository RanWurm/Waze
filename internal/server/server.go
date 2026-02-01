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

type Server struct {
	Graph *graph.Graph

	Cache *RouteCache
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
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST request allowed", http.StatusMethodNotAllowed)
		return
	}

	var reports []types.TrafficReport
	if err := json.NewDecoder(r.Body).Decode(&reports); err != nil {
		http.Error(w, "Invalid Json", http.StatusBadRequest)
		return
	}

	// מקביליות בעדכון
	numWorkers := 8
	reportsCount := len(reports)

	if reportsCount == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	if reportsCount < numWorkers {
		numWorkers = 1
	}

	chunkSize := (reportsCount + numWorkers - 1) / numWorkers

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		start := i * chunkSize
		end := min(start+chunkSize, reportsCount)

		go func(startIdx, endIdx int) {
			defer wg.Done()
			for j := startIdx; j < endIdx; j++ {
				report := reports[j]
				if edge, exists := s.Graph.Edges[report.EdgeID]; exists && report.CarID != -1 {
					edge.UpdateSpeed(report.Speed)
				}
			}
		}(start, end)
	}

	wg.Wait()

	// שליחת עדכון ל-GUI
	if GlobalHub != nil {
		carPositions := s.calculateCarPositions(reports)
		GlobalHub.BroadcastUpdate("cars", carPositions)
	}

	w.WriteHeader(http.StatusOK)
}

// חישוב מיקומי מכוניות על המפה
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

		// נניח progress של 0.5 כברירת מחדל (אפשר לשפר בהמשך)
		progress := 0.5
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
		fmt.Println("The route is from the cache")
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
