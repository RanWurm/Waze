package main

import (
	"fmt"
	"log"
	"net/http"
	"runtime"
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

	log.Printf("Server running on: %s\n", config.Global.Server.Port)
	log.Printf("GUI available at: http://localhost%s\n", config.Global.Server.Port)
	log.Fatal(http.ListenAndServe(config.Global.Server.Port, nil))
}
