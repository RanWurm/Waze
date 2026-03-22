package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
	"waze/internal/config"
	"waze/internal/server"
)

func main() {
	if err := config.Load("config.json"); err != nil {
		panic(err)
	}

	reader := bufio.NewReader(os.Stdin)

	// Algorithm selection
	fmt.Println("\n=== Server Configuration ===")
	fmt.Println("\nSelect routing algorithm:")
	fmt.Println("  1. hybrid      (A* + EntryPoint)")
	fmt.Println("  2. bidir       (Bidirectional A*)")
	fmt.Println("  3. bidir_ep    (Bidirectional A* + EntryPoint)")
	fmt.Println("  4. bidir_hybrid (Bidir for short, Bidir+EP otherwise)")
	fmt.Print("\nChoice [1-4]: ")

	algoInput, _ := reader.ReadString('\n')
	algoInput = strings.TrimSpace(algoInput)

	switch algoInput {
	case "1":
		server.RoutingMode = "hybrid"
	case "2":
		server.RoutingMode = "bidir"
	case "3":
		server.RoutingMode = "bidir_ep"
	case "4":
		server.RoutingMode = "bidir_hybrid"
	default:
		server.RoutingMode = "hybrid"
	}

	// Cache selection
	fmt.Println("\nEnable route caching?")
	fmt.Println("  1. Yes (cache enabled)")
	fmt.Println("  2. No  (cache disabled)")
	fmt.Print("\nChoice [1-2]: ")

	cacheInput, _ := reader.ReadString('\n')
	cacheInput = strings.TrimSpace(cacheInput)

	if cacheInput == "2" {
		config.Global.Server.CacheTtl = 0
		server.CacheEnabled = false
	}

	fmt.Printf("\n>>> Starting server with: %s, cache=%v\n\n", server.RoutingMode, server.CacheEnabled)
	// fmt.Printf("Hello Waze!, map file is: %s\n", config.Global.Server.MapFile)
	srv := server.NewServer(config.Global.Server.MapFile)
	var numCPU int
	// boundary check for CPU size
	if config.Global.MaxCPUs > 0 && config.Global.MaxCPUs <= runtime.NumCPU() {
		// set number of CPUs
		numCPU = config.Global.MaxCPUs
		fmt.Printf("System Limit: Running on %d logical cores\n", numCPU)
	} else {
		numCPU = runtime.NumCPU()
		fmt.Printf("System Limit: Running on ALL available cores (%d)\n", numCPU)
	}

	// set number of CPU
	runtime.GOMAXPROCS(numCPU)

	// wake 'numCPU' workers
	server.WakeWorkers(numCPU, srv.Graph)

	// הפעלת WebSocket Hub
	server.GlobalHub = server.NewHub()
	go server.GlobalHub.Run()

	// API endpoints
	http.HandleFunc("/api/traffic", srv.HandleTrafficBatch)
	http.HandleFunc("/api/navigate", srv.HandleNavigation)
	http.HandleFunc("/api/save-timings", srv.HandleSaveTimings)
	http.HandleFunc("/ws", srv.HandleWebSocket)

	// הגשת קבצי GUI סטטיים
	http.Handle("/", http.FileServer(http.Dir("web")))

	// Memory watchdog: kill process if heap exceeds 4GB
	go func() {
		const maxHeapBytes = 4 * 1024 * 1024 * 1024 // 4 GB
		for {
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			if mem.HeapAlloc > maxHeapBytes {
				log.Printf("MEMORY WATCHDOG: heap=%dMB exceeded 4GB limit, shutting down",
					mem.HeapAlloc/(1024*1024))
				os.Exit(1)
			}
			time.Sleep(2 * time.Second)
		}
	}()

	log.Printf("Server running on: %s\n", config.Global.Server.Port)
	log.Printf("GUI available at: http://localhost%s\n", config.Global.Server.Port)
	log.Fatal(http.ListenAndServe(config.Global.Server.Port, nil))
}
