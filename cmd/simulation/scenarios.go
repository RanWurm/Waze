package main

import (
	"fmt"
	"math/rand"
	"time"
	"waze/internal/config"
	"waze/internal/sim"
)

type EventAction func(w *sim.World)

type ScheduledEvent struct {
	Time     float64     // time to start event
	Name     string      // name of the event
	Action   EventAction // function to activate the event
	Executed bool        // was the event executed or not
}

func StartSim(w *sim.World, numCars int) {
	scenarioEvent := []*ScheduledEvent{
		// at t=30 concerts ends - every one wants to go home
		{
			Time: 30.0,
			Name: "Concert finish",
			Action: func(w *sim.World) {
				startNode := 500
				amount := 50

				baseId := 100000
				fmt.Printf("Generating %d cars from stadium,,\n", amount)

				// creating the cars
				for i := range amount {
					go trySpawnCarAtLoc(w, baseId+i, startNode)
				}
			},
		},
		// at time t = 5, cars want to go to same work place
		{
			Time: 5.0,
			Name: "Go to work",
			Action: func(w *sim.World) {
				src, dst := 10, 2000
				baseId := 200000
				// test for 5 cars asking the same route
				for i := range 5 {
					spawnSpecificCar(w, baseId+i, src, dst)
				}
			},
		},
	}

	RunSimulationLoop(w, scenarioEvent, numCars)
}

func RunSimulationLoop(w *sim.World, events []*ScheduledEvent, numCars int) {
	// init starting traffic
	initStartingTraffic(numCars, w)

	lastSpawnTime := 0.0

	fmt.Println("Starting Simulation Loop")

	simTime := 0.0
	// one second of the simulation
	dt := config.Global.Simulation.DeltaTime

	for {
		// check if any event is happening
		for _, event := range events {
			if !event.Executed && simTime >= event.Time {
				fmt.Printf("\n[SimTime %.1f] Executing Event: %s\n", simTime, event.Name)
				event.Action(w)
				event.Executed = true
			}
		}
		// dt seconds in the real world
		w.Tick(dt)
		w.CleanArrivedCars()
		simTime += dt

		// check if it is time to generate a new car
		if w.SimTime-lastSpawnTime >= config.Global.Simulation.SpawnRate && w.SimTime < config.Global.Simulation.EndSpawn {
			lastSpawnTime = w.SimTime
			numCars++
			genarateRandomCar(w, numCars)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func genarateRandomCar(w *sim.World, carId int) {
	var (
		route []int
		err   error
	)
	for {
		src, dst := sim.RandomRequest(w.Graph, w.Rng)
		// check if the nodes are valid
		route, err = w.Client.RequestRoute(src, dst)
		if err != nil {
			fmt.Printf("Route does not exists. Error: %s\n", err)
			continue
		}
		break
	}
	car := w.AddCar(carId, carId)
	car.InitRoute(route, w.Graph)
}

func initStartingTraffic(numCars int, w *sim.World) {
	fmt.Printf("Initializing %d Cars...\n", numCars)
	for i := range numCars {
		genarateRandomCar(w, i)
	}
}

func trySpawnCarAtLoc(w *sim.World, id int, src int) {
	dst := w.Rng.Intn(len(w.Graph.NodesArr))
	if src == dst {
		dst++
	}
	route, err := w.Client.RequestRoute(src, dst)
	if err == nil && len(route) > 0 {
		car := w.AddCar(id, id)
		car.InitRoute(route, w.Graph)
	}
}

func trySpawnRandomCar(w *sim.World, id int) {
	src := rand.Intn(1000)
	dst := rand.Intn(1000)
	if src == dst {
		return
	}
	route, err := w.Client.RequestRoute(src, dst)
	if err == nil && len(route) > 0 {
		car := w.AddCar(id, id)
		car.InitRoute(route, w.Graph)
	}
}

func RunStressTest(w *sim.World, currentCarId int) int {
	// // scenario one - a lot of cars leave from the same place to work
	// if int(w.SimTime) == 5 {
	// 	fmt.Println("Test cache hits")
	// 	src, dst := 10, 2000

	// 	// test for 5 cars asking the same route
	// 	for range 5 {
	// 		currentCarId++
	// 		spawnSpecificCar(w, currentCarId, src, dst)
	// 	}
	// }

	// scenario two - a lot of cars leave the stadium at once
	if int(w.SimTime) == 15 {
		fmt.Println("Stadium Burst (Testing Parallel Processing)...")

		for range 500 {
			currentCarId++
			src, dst := sim.RandomRequest(w.Graph, w.Rng)
			spawnSpecificCar(w, currentCarId, src, dst)
		}
	}

	if int(w.SimTime) == 20 {
		fmt.Println("Starting Real-World Cycle Benchmark...")

		carCount := 500000

		durationSingle := w.Client.MeasureTotalCycle(carCount, "single")
		fmt.Printf("Approach A (Generate & Send Huge): %v\n", durationSingle)

		time.Sleep(3 * time.Second)

		durationParallel := w.Client.MeasureTotalCycle(carCount, "parallel")
		fmt.Printf("Approach B (Pipeline Generate & Send): %v\n", durationParallel)

		fmt.Printf("Speedup Factor: %.2fx\n", float64(durationSingle)/float64(durationParallel))
	}
	return currentCarId

}

func spawnSpecificCar(w *sim.World, id, src, dst int) {
	var (
		route []int
		err   error
	)
	for {
		route, err = w.Client.RequestRoute(src, dst)
		if err != nil {
			fmt.Printf("Failed to spawn scenario car %d: %v\n", id, err)
			continue
		}
		break
	}
	car := w.AddCar(id, id)
	car.InitRoute(route, w.Graph)
}
