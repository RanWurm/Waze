package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
	"waze/internal/graph"
	"waze/internal/navigation"
)

type routeResult struct {
	queryID  int
	src      int
	dst      int
	bidirMs  float64
	bidirOk  bool
	bidirEpMs float64
	bidirEpOk bool
}

func main() {
	numQueries := flag.Int("queries", 1000, "Number of route queries to simulate")
	numWorkers := flag.Int("workers", 0, "Number of parallel workers (0 = NumCPU)")
	flag.Parse()

	if *numWorkers <= 0 {
		*numWorkers = runtime.NumCPU()
	}
	runtime.GOMAXPROCS(*numWorkers)

	fmt.Println("Loading graph...")
	g, err := graph.LoadGraph("data/gush_dan.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load graph: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Graph loaded: %d nodes, %d edges\n", len(g.Nodes), len(g.Edges))
	fmt.Printf("Workers: %d\n", *numWorkers)
	fmt.Printf("Queries: %d\n", *numQueries)

	// Initialize Bidir Entry Point Router
	fmt.Println("Initializing BidirEntryPointRouter...")
	bidirEpRouter := navigation.NewBidirEntryPointRouter(g, 2*time.Minute)
	navigation.InitializeBidirGushDanCities(bidirEpRouter)

	// Generate random src/dst pairs
	fmt.Printf("Generating %d random query pairs...\n", *numQueries)
	nodes := g.NodesArr
	pairs := make([][2]int, *numQueries)
	for i := 0; i < *numQueries; i++ {
		src := nodes[rand.Intn(len(nodes))]
		dst := nodes[rand.Intn(len(nodes))]
		for dst == src {
			dst = nodes[rand.Intn(len(nodes))]
		}
		pairs[i] = [2]int{src, dst}
	}

	// Suppress stdout during routing
	devNull, _ := os.Open(os.DevNull)
	defer devNull.Close()

	results := make([]routeResult, *numQueries)

	// ============ Benchmark Bidirectional A* ============
	fmt.Println("\n=== Benchmarking Bidirectional A* ===")
	startBidir := time.Now()

	var wg sync.WaitGroup
	jobs := make(chan int, *numQueries)

	for w := 0; w < *numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				src, dst := pairs[i][0], pairs[i][1]
				start := time.Now()
				_, err := navigation.FindPathBidirectionalAstar(g, src, dst)
				results[i].queryID = i
				results[i].src = src
				results[i].dst = dst
				results[i].bidirMs = float64(time.Since(start).Microseconds()) / 1000.0
				results[i].bidirOk = (err == nil)
			}
		}()
	}

	for i := 0; i < *numQueries; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	totalBidir := time.Since(startBidir)
	fmt.Printf("Bidirectional A* total time: %v\n", totalBidir)

	// ============ Benchmark Bidir EntryPoint ============
	fmt.Println("\n=== Benchmarking Bidir EntryPoint ===")
	startBidirEp := time.Now()

	jobs2 := make(chan int, *numQueries)

	for w := 0; w < *numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			origStdout := os.Stdout
			os.Stdout = devNull
			for i := range jobs2 {
				src, dst := pairs[i][0], pairs[i][1]
				start := time.Now()
				_, err := bidirEpRouter.FindPathWithEntryPoints(src, dst)
				results[i].bidirEpMs = float64(time.Since(start).Microseconds()) / 1000.0
				results[i].bidirEpOk = (err == nil)
			}
			os.Stdout = origStdout
		}()
	}

	for i := 0; i < *numQueries; i++ {
		jobs2 <- i
	}
	close(jobs2)
	wg.Wait()

	totalBidirEp := time.Since(startBidirEp)
	fmt.Printf("Bidir EntryPoint total time: %v\n", totalBidirEp)

	// ============ Summary ============
	fmt.Println("\n=== Summary ===")
	fmt.Printf("Queries: %d\n", *numQueries)
	fmt.Printf("Workers: %d\n", *numWorkers)
	fmt.Printf("Bidirectional A* total: %v\n", totalBidir)
	fmt.Printf("Bidir EntryPoint total: %v\n", totalBidirEp)

	speedup := float64(totalBidir) / float64(totalBidirEp)
	if totalBidirEp > totalBidir {
		speedup = float64(totalBidirEp) / float64(totalBidir)
		fmt.Printf("Bidirectional A* is %.2fx faster\n", speedup)
	} else {
		fmt.Printf("Bidir EntryPoint is %.2fx faster\n", speedup)
	}

	// ============ Write CSV ============
	csvPath := fmt.Sprintf("benchmarks/simulation_benchmark_%dq_%dw.csv", *numQueries, *numWorkers)
	f, err := os.Create(csvPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create CSV: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"query_id", "src", "dst", "bidir_ms", "bidir_ok", "bidir_ep_ms", "bidir_ep_ok"})
	for _, r := range results {
		w.Write([]string{
			strconv.Itoa(r.queryID),
			strconv.Itoa(r.src),
			strconv.Itoa(r.dst),
			fmt.Sprintf("%.3f", r.bidirMs),
			strconv.FormatBool(r.bidirOk),
			fmt.Sprintf("%.3f", r.bidirEpMs),
			strconv.FormatBool(r.bidirEpOk),
		})
	}
	w.Flush()

	// Write summary CSV
	summaryPath := fmt.Sprintf("benchmarks/simulation_benchmark_summary_%dq_%dw.csv", *numQueries, *numWorkers)
	sf, _ := os.Create(summaryPath)
	defer sf.Close()
	sw := csv.NewWriter(sf)
	sw.Write([]string{"algorithm", "total_ms", "queries", "workers"})
	sw.Write([]string{"bidir", fmt.Sprintf("%.3f", float64(totalBidir.Milliseconds())), strconv.Itoa(*numQueries), strconv.Itoa(*numWorkers)})
	sw.Write([]string{"bidir_ep", fmt.Sprintf("%.3f", float64(totalBidirEp.Milliseconds())), strconv.Itoa(*numQueries), strconv.Itoa(*numWorkers)})
	sw.Flush()

	fmt.Printf("\nCSV written to %s\n", csvPath)
	fmt.Printf("Summary written to %s\n", summaryPath)
}
