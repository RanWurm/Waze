package main

import (
	"fmt"
	"log"
	"runtime"
	"time"
	"waze/internal/config"
	"waze/internal/sim"
)

var CONFIG_FILE string = "config.json"

func main() {
	if err := config.Load(CONFIG_FILE); err != nil {
		panic(err)
	}

	var numCPUs int
	// boundary check for CPUs num
	if config.Global.MaxCPUs > 0 && config.Global.MaxCPUs <= runtime.NumCPU() {
		// set number of CPUs
		numCPUs = config.Global.MaxCPUs
		fmt.Printf("System Limit: Running on %d logical cores\n", numCPUs)
	} else {
		numCPUs = runtime.NumCPU()
		fmt.Printf("System Limit: Running on ALL available cores (%d)\n", numCPUs)
	}

	world, err := sim.NewWorld(config.Global.Server.MapFile, (config.Global.Simulation.ServerURL + config.Global.Server.Port))
	if err != nil {
		log.Fatal(err)
	}

	sim.StartMoveWorkers(numCPUs)

	numCars := config.Global.Simulation.NumCars

	// fmt.Printf("Initializing %d Cars...\n", numCars)
	// var route []int
	// for i := range numCars {
	// 	for {
	// 		src, dst := randomRequest(world.Graph)
	// 		route, err = world.Client.RequestRoute(src, dst)
	// 		if err != nil {
	// 			fmt.Printf("Route does not exists. Error: %s\n", err)
	// 			continue
	// 		}
	// 		break
	// 	}
	// 	car := world.AddCar(i, i)
	// 	car.InitRoute(route, world.Graph)
	// }

	start := time.Now()

	dt := 1.0
	loop(numCars, dt, world)

	fmt.Println("Simulation Finished!")
	fmt.Printf("total run time: %v\n", time.Since(start))
}

func loop(carCounter int, dt float64, world *sim.World) {
	// lastLogTime := 0.0
	// lastSpawnTime := 0.0

	for {
		// if world.SimTime > 10 && !world.HasActiveCars() {
		// 	fmt.Println("All cars arrived. Stopping simulation.")
		// 	break
		// }
		if world.SimTime > 200 {
			fmt.Println("All cars arrived. Stopping simulation.")
			break
		}

		// if world.SimTime-lastLogTime >= 5.0 {
		// 	fmt.Printf("[SIM] Time: %.f, | Cars: %d\n", world.SimTime, len(world.Cars))
		// 	lastLogTime = world.SimTime
		// }

		world.Tick(dt)
		world.CleanArrivedCars()

		carCounter = RunStressTest(world, carCounter)

		// if world.SimTime-lastSpawnTime >= config.Global.Simulation.SpawnRate && world.SimTime < (120.0) {
		// 	lastSpawnTime = world.SimTime
		// 	var (
		// 		src, dst int
		// 		route    []int
		// 		err      error
		// 	)
		// 	spawnSuccess := false
		// 	for range 3 {
		// 		src, dst = randomRequest(world.Graph)
		// 		route, err = world.Client.RequestRoute(src, dst)
		// 		if err == nil {
		// 			spawnSuccess = true
		// 			break
		// 		}
		// 	}
		// 	if spawnSuccess {
		// 		lastSpawnTime = world.SimTime
		// 		carCounter++
		// 		newCar := world.AddCar(carCounter, carCounter)
		// 		newCar.InitRoute(route, world.Graph)
		// 	} else {
		// 		fmt.Println("Skipped spawn: could not find valid route after 3 attempts")
		// 	}
		// }
		time.Sleep(100 * time.Millisecond)
	}
}

// go func() {
// 	targetEdgeID := 150
// 	fakeCarID := 99999

// 	for {
// 		jamReport := []types.TrafficReport{
// 			{
// 				CarID:     fakeCarID,
// 				EdgeID:    targetEdgeID,
// 				Speed:     1.0,
// 				Timestamp: time.Now().Unix(),
// 			},
// 		}
// 		err := world.Client.SendTrafficBatch(jamReport)
// 		if err != nil {
// 			fmt.Printf("Error in jam report %s\n", err)
// 		}
// 		time.Sleep(2 * time.Second)
// 	}
// }()
