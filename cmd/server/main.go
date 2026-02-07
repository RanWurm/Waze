package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
	"waze/internal/config"
	"waze/internal/server"
)

func main() {
	if err := config.Load("config.json"); err != nil {
		panic(err)
	}
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
