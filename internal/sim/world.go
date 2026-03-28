package sim

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"
	"waze/internal/config"
	"waze/internal/graph"
	"waze/internal/types"
)

const TIME_TO_REPORT = 2

const densityWorkers = 8

var fakeJamInjected bool = false

type World struct {
	Graph         *graph.Graph
	Cars          []*Car
	SimTime       float64
	ReportsBuffer []types.TrafficReport
	Client        *Client
	EdgeDensity   map[int]int
	Rng           *rand.Rand
	GlobalCarId   int64

	RouteTokens chan struct{}

	VirtualStartTime time.Time

	// Reusable maps for density calculation to avoid per-tick allocations
	densityLocalMaps []map[int]int
	densityResult    map[int]int
}

func NewWorld(mapFile, serverUrl string) (*World, error) {
	g, err := graph.LoadGraph(mapFile)
	if err != nil {
		log.Fatal(err)
	}

	source := rand.NewSource(42)
	rng := rand.New(source)

	localMaps := make([]map[int]int, densityWorkers)
	for i := range localMaps {
		localMaps[i] = make(map[int]int)
	}

	return &World{
		Graph:            g,
		Cars:             make([]*Car, 0),
		SimTime:          0,
		VirtualStartTime: time.Now(),
		Client:           NewClient(serverUrl),
		Rng:              rng,
		densityLocalMaps: localMaps,
		densityResult:    make(map[int]int),
		RouteTokens:      make(chan struct{}, int(config.Global.Simulation.MaxRouteRequest)),
	}, nil
}

func (world *World) GetCurrentTime() int64 {
	currentTime := world.VirtualStartTime.Add(time.Duration(world.SimTime) * time.Second)
	return currentTime.Unix()
}

func (world *World) AddCar(id, userId int) *Car {
	car := NewCar(id, userId, world.SimTime)
	world.Cars = append(world.Cars, car)
	return car
}

func (world *World) HasActiveCars() bool {
	return len(world.Cars) > 0
}

func (world *World) CleanArrivedCars() {
	activeCars := world.Cars[:0]

	for _, car := range world.Cars {
		if car.State != Arrived && car.State != Waiting {
			activeCars = append(activeCars, car)
		}
	}
	world.Cars = activeCars
}

func (world *World) GenarateTrafficReportsParallel() []types.TrafficReport {
	carsCount := len(world.Cars)
	if carsCount == 0 {
		return nil
	}

	if cap(world.ReportsBuffer) < carsCount {
		world.ReportsBuffer = make([]types.TrafficReport, carsCount)
	} else {
		world.ReportsBuffer = world.ReportsBuffer[:carsCount]
	}

	numWorkers := 6

	if carsCount < numWorkers {
		numWorkers = 1
	}

	chunkSize := (carsCount + numWorkers - 1) / numWorkers

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		start := i * chunkSize
		end := min(start+chunkSize, carsCount)
		go func(startIdx, endIdx int) {
			defer wg.Done()

			for j := startIdx; j < endIdx; j++ {
				car := world.Cars[j]

				if car.State == Driving && car.ActiveRoute != nil {
					currentEdge := car.ActiveRoute.RouteEdges[car.ActiveRoute.CurrentEdgeIndex]

					world.ReportsBuffer[j] = types.TrafficReport{
						CarID:     car.Id,
						EdgeID:    currentEdge,
						Speed:     car.CurrentSpeed,
						Timestamp: world.GetCurrentTime(),
					}
				} else {
					world.ReportsBuffer[j].CarID = -1
				}
			}
		}(start, end)

	}
	wg.Wait()
	return world.ReportsBuffer
}

func (world *World) GenarateTrafficReports() []types.TrafficReport {
	carsCount := len(world.Cars)
	if carsCount == 0 {
		return nil
	}

	if cap(world.ReportsBuffer) < carsCount {
		world.ReportsBuffer = make([]types.TrafficReport, carsCount)
	} else {
		world.ReportsBuffer = world.ReportsBuffer[:carsCount]
	}
	for i := range carsCount {
		car := world.Cars[i]
		if car.State == Driving && car.ActiveRoute != nil {
			currentEdge := car.ActiveRoute.RouteEdges[car.ActiveRoute.CurrentEdgeIndex]

			world.ReportsBuffer[i] = types.TrafficReport{
				CarID:     car.Id,
				EdgeID:    currentEdge,
				Speed:     car.CurrentSpeed,
				Timestamp: world.GetCurrentTime(),
			}
		} else {
			world.ReportsBuffer[i].CarID = -1
		}
	}
	return world.ReportsBuffer
}

func (w *World) TrafficReport(reports []types.TrafficReport) {
	numReports := len(reports)
	if numReports == 0 {
		return
	}
	neededWorkers := numReports / minReportsPerRequest
	numWorkers := max(min(config.Global.MaxCPUs, neededWorkers), 1)

	chunkSize := (numReports + numWorkers - 1) / numWorkers
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		startIdx := i * chunkSize
		endIdx := min(startIdx+chunkSize, numReports)
		if startIdx >= endIdx {
			break
		}

		chunk := reports[startIdx:endIdx]

		wg.Add(1)
		go func(chunk []types.TrafficReport) {
			defer wg.Done()

			err := w.Client.SendTrafficBatch(chunk)
			if err != nil {
				fmt.Println("Error sending batch:", err)
			}
		}(chunk)
	}
	wg.Wait()
}

func (world *World) collectActiveReports() []types.TrafficReport {
	reports := make([]types.TrafficReport, 0, len(world.Cars))

	for _, car := range world.Cars {
		if car == nil {
			continue
		}

		if car.State == Driving && car.ActiveRoute != nil {
			currentEdgeId := car.ActiveRoute.RouteEdges[car.ActiveRoute.CurrentEdgeIndex]

			// check if the edge or speed has changed, or the maximum time after the last report has past
			edgeChanged := (currentEdgeId != car.LastReportedEdgeID)
			speedChanged := math.Abs(car.CurrentSpeed-car.LastReportedSpeed) > config.Global.Simulation.SpeedThreshold
			maxTime := world.SimTime-car.LastReportTime >= config.Global.Simulation.MaxTime

			// if any of the three happened
			if edgeChanged || speedChanged || maxTime {

				progress := 0.0
				if car.ActiveRoute.CurrentEdgeLen > 0 {
					progress = car.ActiveRoute.EdgeProgress / car.ActiveRoute.CurrentEdgeLen
					if progress > 1 {
						progress = 1
					}
				}

				report := types.TrafficReport{
					CarID:        car.Id,
					EdgeID:       currentEdgeId,
					Speed:        car.CurrentSpeed,
					Timestamp:    world.GetCurrentTime(),
					EdgeProgress: progress,
				}

				car.LastReportedEdgeID = currentEdgeId
				car.LastReportedSpeed = car.CurrentSpeed
				car.LastReportTime = world.SimTime

				// Include full route for sampled cars (GUI display)
				if car.Id%10 == 0 {
					report.RouteEdges = car.ActiveRoute.RouteEdges
				}
				reports = append(reports, report)
			}
		}
	}

	return reports
}

func (world *World) Tick(dt float64) {
	world.SimTime += dt

	world.EdgeDensity = world.calculateDensityParallel()
	MoveCarsParallel(world.Cars, dt, world.Graph, world.EdgeDensity)

	currentTime := int(world.SimTime * 10)
	reportTime := int(config.Global.Simulation.ReportInterval * 10)

	// time for traffic report
	if reportTime > 0 && currentTime%reportTime == 0 {
		reports := world.collectActiveReports()
		if len(reports) > 0 {
			go func(reports []types.TrafficReport) {
				world.TrafficReport(reports)
				// fmt.Printf("Sent %d Traffic reports\n", len(reports))
			}(reports)
		}
	}

	// Check rerouting every tick - each car has its own schedule
	world.triggerSmartNavigation()

}

func (w *World) triggerSmartNavigation() {
	for _, car := range w.Cars {
		if car == nil || car.State != Driving || car.ActiveRoute == nil {
			// fmt.Println("the car is nil?")
			continue
		}

		// Check if it's time for this car to reroute
		if w.SimTime < car.NextRerouteTime {
			continue
		}

		// Schedule next reroute check: 60 + random(10-60) seconds
		car.NextRerouteTime = w.SimTime + 60.0 + 10.0 + rand.Float64()*50.0

		car.Mu.RLock()

		// check if we are in the last edge
		currIdx := car.ActiveRoute.CurrentEdgeIndex
		routeLen := len(car.ActiveRoute.RouteEdges)

		if currIdx >= routeLen-1 {
			car.Mu.RUnlock()
			// fmt.Println("Current index is bigger the routeLen?")
			continue
		}

		accumulatedDistance := 0.0
		futureIdx := currIdx
		reachedLookAhead := false

		// add edges length until we reach the accumulatedDistance const
		for i := currIdx; i < routeLen-1; i++ {
			edgeID := car.ActiveRoute.RouteEdges[i]
			edge := w.Graph.Edges[edgeID]

			if edge == nil {
				// fmt.Println("The edge is nil?")
				continue
			}

			// add the length of the next edge
			accumulatedDistance += edge.Length
			futureIdx = i

			// check if we are past the LookAheadDistance const
			if accumulatedDistance >= config.Global.Simulation.LookAheadDistance {
				reachedLookAhead = true
				// fmt.Println("Are we past look ahead distance?")
				break
			}
		}

		// if the next edge is the last edge
		if !reachedLookAhead || futureIdx >= routeLen-1 {
			car.Mu.RUnlock()
			// fmt.Println("The next edge is the past edge?")
			continue
		}

		futureEdgeID := car.ActiveRoute.RouteEdges[futureIdx]
		futureEdge := w.Graph.Edges[futureEdgeID]

		lastEdgeID := car.ActiveRoute.RouteEdges[len(car.ActiveRoute.RouteEdges)-1]
		lastEdge := w.Graph.Edges[lastEdgeID]

		if futureEdge == nil || lastEdge == nil {
			car.Mu.RUnlock()
			// fmt.Println("future edge = nil?")
			continue
		}

		// the next node and dst node
		nextNode := futureEdge.To
		destNode := w.Graph.Edges[lastEdgeID].To

		car.Mu.RUnlock()

		// route request
		go func(c *Car, requestNode, dest, targetIdx int) {

			select {
			case w.RouteTokens <- struct{}{}:
				defer func() { <-w.RouteTokens }()
			default:
				return
			}

			newRoute, err := w.Client.RequestRoute(requestNode, dest)

			if err != nil {
				errString := strings.ToLower(err.Error())
				if strings.Contains(errString, "route does not exist") {
					panic(fmt.Sprintf("SANITY FAIL: No route exists between %d and %d!", requestNode, dest))
				}
				return
			}

			if len(newRoute) == 0 {
				// fmt.Println("the len is 0")
				return
			}

			c.Mu.RLock()

			if c.ActiveRoute == nil || c.State != Driving {
				// fmt.Println("active route is 0")
				c.Mu.RUnlock()
				return
			}

			carIndexNow := c.ActiveRoute.CurrentEdgeIndex
			routeEdgesCopy := c.ActiveRoute.RouteEdges

			c.Mu.RUnlock()

			// check if we are no past the target edge
			if carIndexNow > targetIdx {
				// fmt.Println("we r past the target edge")
				return
			}

			// check if the route is different
			if different(newRoute, routeEdgesCopy, targetIdx+1) {

				// Check if new route contains any edge already in the prefix (would create cycle)
				prefixEdges := make(map[int]bool)
				for _, edgeID := range routeEdgesCopy[:targetIdx+1] {
					prefixEdges[edgeID] = true
				}

				hasCycle := false
				for _, edgeID := range newRoute {
					if prefixEdges[edgeID] {
						hasCycle = true
						break
					}
				}

				if hasCycle {
					return
				}

				// append the new route to the old route
				updatedRoute := make([]int, 0)
				updatedRoute = append(updatedRoute, routeEdgesCopy[:targetIdx+1]...)
				updatedRoute = append(updatedRoute, newRoute...)

				// update new active route
				c.Mu.Lock()
				if c.ActiveRoute != nil {
					c.ActiveRoute.RouteEdges = updatedRoute
				}
				c.Mu.Unlock()
			}
		}(car, nextNode, destNode, futureIdx)
	}
}

func different(newRoute, currentRoute []int, currentIndex int) bool {
	if len(newRoute) != len(currentRoute)-currentIndex {
		return true
	}

	for i := 0; i < len(newRoute); i++ {
		if newRoute[i] != currentRoute[i+currentIndex] {
			return true
		}
	}
	return false
}

func contains(slice []int, item int) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

func (world *World) calculateDensityParallel() map[int]int {
	carsCount := len(world.Cars)

	// Clear the result map
	for k := range world.densityResult {
		delete(world.densityResult, k)
	}

	if carsCount == 0 {
		return world.densityResult
	}

	numWorkers := densityWorkers
	if carsCount < numWorkers {
		numWorkers = 1
	}

	chunkSize := (carsCount + numWorkers - 1) / numWorkers

	// Clear worker maps
	for i := 0; i < numWorkers; i++ {
		for k := range world.densityLocalMaps[i] {
			delete(world.densityLocalMaps[i], k)
		}
	}

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		start := i * chunkSize
		end := min(start+chunkSize, carsCount)

		go func(idx, startIdx, endIdx int) {
			defer wg.Done()
			for j := startIdx; j < endIdx; j++ {
				car := world.Cars[j]
				if car.State == Driving && car.ActiveRoute != nil {
					edgeId := car.ActiveRoute.RouteEdges[car.ActiveRoute.CurrentEdgeIndex]
					world.densityLocalMaps[idx][edgeId]++
				}
			}
		}(i, start, end)
	}

	wg.Wait()

	// מיזוג המפות
	for i := 0; i < numWorkers; i++ {
		for edgeId, count := range world.densityLocalMaps[i] {
			world.densityResult[edgeId] += count
		}
	}

	return world.densityResult
}
