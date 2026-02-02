package sim

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
	"waze/internal/config"
	"waze/internal/graph"
	"waze/internal/types"
)

const TIME_TO_REPORT = 2

type World struct {
	Graph         *graph.Graph
	Cars          []*Car
	SimTime       float64
	ReportsBuffer []types.TrafficReport
	Client        *Client
	EdgeDensity   map[int]int
	Rng           *rand.Rand

	VirtualStartTime time.Time
}

func NewWorld(mapFile, serverUrl string) (*World, error) {
	g, err := graph.LoadGraph(mapFile)
	if err != nil {
		log.Fatal(err)
	}

	source := rand.NewSource(42)
	rng := rand.New(source)

	return &World{
		Graph:            g,
		Cars:             make([]*Car, 0),
		SimTime:          0,
		VirtualStartTime: time.Now(),
		Client:           NewClient(serverUrl),
		Rng:              rng,
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
		if car.State != Arrived {
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

func (world *World) Tick(dt float64) {
	world.SimTime += dt

	world.EdgeDensity = world.calculateDensityParallel()
	MoveCarsParallel(world.Cars, dt, world.Graph, world.EdgeDensity)

	if int(world.SimTime)%int(config.Global.Simulation.ReportInterval) == 0 {
		reports := world.GenarateTrafficReports()
		reportsCopy := make([]types.TrafficReport, len(reports))
		copy(reportsCopy, reports)
		go func(batch []types.TrafficReport) {
			err := world.Client.SendTrafficBatch(batch)
			if err != nil {
				fmt.Println("Failed to send traffic batch: ", err)
			}
		}(reportsCopy)
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
	if carsCount == 0 {
		return make(map[int]int)
	}

	numWorkers := 8
	if carsCount < numWorkers {
		numWorkers = 1
	}

	chunkSize := (carsCount + numWorkers - 1) / numWorkers

	// כל עובד יוצר מפה מקומית
	localMaps := make([]map[int]int, numWorkers)

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		localMaps[i] = make(map[int]int)
		start := i * chunkSize
		end := min(start+chunkSize, carsCount)

		go func(idx, startIdx, endIdx int) {
			defer wg.Done()
			for j := startIdx; j < endIdx; j++ {
				car := world.Cars[j]
				if car.State == Driving && car.ActiveRoute != nil {
					edgeId := car.ActiveRoute.RouteEdges[car.ActiveRoute.CurrentEdgeIndex]
					localMaps[idx][edgeId]++
				}
			}
		}(i, start, end)
	}

	wg.Wait()

	// מיזוג המפות
	result := make(map[int]int)
	for _, m := range localMaps {
		for edgeId, count := range m {
			result[edgeId] += count
		}
	}

	return result
}
