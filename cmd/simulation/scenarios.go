package main

import (
	"fmt"
	"time"
	"waze/internal/sim"
)

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

	// בתוך RunStressTest
	if int(w.SimTime) == 20 {
		fmt.Println("🧪 Starting Real-World Cycle Benchmark...")

		// בודקים על 500,000 מכוניות כדי להרגיש את ההבדל ביצירה
		carCount := 500000

		// מדידה A
		durationSingle := w.Client.MeasureTotalCycle(carCount, "single")
		fmt.Printf("Approach A (Generate & Send Huge): %v\n", durationSingle)

		time.Sleep(3 * time.Second)

		// מדידה B
		durationParallel := w.Client.MeasureTotalCycle(carCount, "parallel")
		fmt.Printf("Approach B (Pipeline Generate & Send): %v\n", durationParallel)

		fmt.Printf("Speedup Factor: %.2fx\n", float64(durationSingle)/float64(durationParallel))
	}

	// if int(w.SimTime) == 40 {
	// 	fmt.Println("Starting Benchmark: Single vs Parallel")

	// 	// faking a lot of reports to check the time (test json bottleNeck)
	// 	hugeReport := make([]types.TrafficReport, 50000)
	// 	for i := range hugeReport {
	// 		hugeReport[i] = types.TrafficReport{
	// 			CarID:  i,
	// 			EdgeID: i % 1000,
	// 			Speed:  20.0,
	// 		}
	// 	}

	// 	// one big batch
	// 	durationSingle := w.Client.MeasurePerformance(hugeReport, "single")
	// 	fmt.Printf("Approach A (Single Giant Batch): %v\n", durationSingle)

	// 	// break between scenarios
	// 	time.Sleep(5 * time.Second)

	// 	// several messages
	// 	durationParallel := w.Client.MeasurePerformance(hugeReport, "parallel")
	// 	fmt.Printf("Approach B (Parallel Sharding):   %v\n", durationParallel)

	// 	improvement := float64(durationSingle) / float64(durationParallel)
	// 	fmt.Printf("Speedup Factor: %.2fx\n", improvement)
	// }

	return currentCarId

}

func spawnSpecificCar(w *sim.World, id, src, dst int) {

	route, err := w.Client.RequestRoute(src, dst)
	if err != nil {
		fmt.Printf("Failed to spawn scenario car %d: %v\n", id, err)
		return
	}
	car := w.AddCar(id, id)
	car.InitRoute(route, w.Graph)
}
