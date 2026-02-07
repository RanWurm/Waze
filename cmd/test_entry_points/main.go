package main

import (
	"log"
	"time"
	"waze/internal/config"
	"waze/internal/graph"
	"waze/internal/navigation"
)

func main() {
	// Load configuration
	if err := config.Load("config.json"); err != nil {
		panic(err)
	}

	// Load graph
	log.Printf("Loading graph from %s...", config.Global.Server.MapFile)
	g, err := graph.LoadGraph(config.Global.Server.MapFile)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Graph loaded: %d nodes, %d edges", len(g.Nodes), len(g.Edges))

	// Initialize Entry Point Router
	log.Println("\n=== Initializing Entry Point Router ===")
	router := navigation.NewEntryPointRouter(g, 2*time.Minute)
	navigation.InitializeGushDanCities(router)

	// Display entry points
	log.Println("\n=== Entry Points Summary ===")
	for cityName, city := range router.EntryPointManager.Cities {
		log.Printf("%s: %d entry points at nodes %v", cityName, len(city.EntryPoints), city.EntryPoints)
	}

	// Simulate multiple users going to Petah Tikva from different locations in Ramat Gan
	log.Println("\n=== Simulating User Queries ===")
	log.Println("Scenario: 3 users from different parts of Ramat Gan going to different locations in Petah Tikva")

	// Find some nodes in Ramat Gan and Petah Tikva
	ramatGanNodes := findNodesInCity(router, "Ramat Gan")
	petahTikvaNodes := findNodesInCity(router, "Petah Tikva")

	if len(ramatGanNodes) < 3 || len(petahTikvaNodes) < 3 {
		log.Println("Not enough nodes found in cities for demo")
		log.Printf("Ramat Gan nodes: %d, Petah Tikva nodes: %d", len(ramatGanNodes), len(petahTikvaNodes))
		return
	}

	// User 1
	log.Println("\n--- User 1 Query ---")
	start := time.Now()
	result1, err := router.FindPathWithEntryPoints(ramatGanNodes[0], petahTikvaNodes[0])
	elapsed1 := time.Since(start)
	if err != nil {
		log.Printf("User 1 error: %v", err)
	} else {
		log.Printf("User 1: %d -> %d | Distance: %.2f km | ETA: %.1f min | Time: %v",
			ramatGanNodes[0], petahTikvaNodes[0], result1.Distance, result1.ETA, elapsed1)
	}

	// User 2 (should reuse cached backward searches)
	log.Println("\n--- User 2 Query (should use cache) ---")
	start = time.Now()
	result2, err := router.FindPathWithEntryPoints(ramatGanNodes[1], petahTikvaNodes[1])
	elapsed2 := time.Since(start)
	if err != nil {
		log.Printf("User 2 error: %v", err)
	} else {
		log.Printf("User 2: %d -> %d | Distance: %.2f km | ETA: %.1f min | Time: %v",
			ramatGanNodes[1], petahTikvaNodes[1], result2.Distance, result2.ETA, elapsed2)
		log.Printf("Speedup from caching: %.2fx", float64(elapsed1)/float64(elapsed2))
	}

	// User 3 (should also reuse cached backward searches)
	log.Println("\n--- User 3 Query (should use cache) ---")
	start = time.Now()
	result3, err := router.FindPathWithEntryPoints(ramatGanNodes[2], petahTikvaNodes[2])
	elapsed3 := time.Since(start)
	if err != nil {
		log.Printf("User 3 error: %v", err)
	} else {
		log.Printf("User 3: %d -> %d | Distance: %.2f km | ETA: %.1f min | Time: %v",
			ramatGanNodes[2], petahTikvaNodes[2], result3.Distance, result3.ETA, elapsed3)
		log.Printf("Speedup from caching: %.2fx", float64(elapsed1)/float64(elapsed3))
	}

	log.Println("\n=== Demo Complete ===")
	log.Println("Notice how User 2 and User 3 are faster because they reuse the cached backward searches")
	log.Println("from Petah Tikva entry points that were computed for User 1.")
}

func findNodesInCity(router *navigation.EntryPointRouter, cityName string) []int {
	nodes := make([]int, 0)
	for nodeID, city := range router.EntryPointManager.NodeToCity {
		if city == cityName {
			nodes = append(nodes, nodeID)
			if len(nodes) >= 10 { // Get at least 10 nodes
				break
			}
		}
	}
	return nodes
}
