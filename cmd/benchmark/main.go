package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strconv"
	"time"
	"waze/internal/graph"
	"waze/internal/navigation"
)

const (
	rounds        = 1
	pairsPerRound = 1000
)

type result struct {
	round   int
	pair    int
	src     int
	dst     int
	astarMs float64
	deltaMs float64
}

type pair struct {
	src, dst int
}

func euclideanDist(g *graph.Graph, src, dst int) float64 {
	ns := g.Nodes[src]
	nd := g.Nodes[dst]
	dx := ns.X - nd.X
	dy := ns.Y - nd.Y
	return math.Sqrt(dx*dx + dy*dy)
}

func main() {
	longMode := flag.Bool("long", false, "Only benchmark the top longest pairs by Euclidean distance")
	longPct := flag.Float64("long-pct", 10, "Percentage of longest pairs to keep when --long is set")
	shortMode := flag.Bool("short", false, "Only benchmark the top shortest pairs by Euclidean distance")
	shortPct := flag.Float64("short-pct", 10, "Percentage of shortest pairs to keep when --short is set")
	midMode := flag.Bool("mid", false, "Only benchmark the middle pairs by Euclidean distance")
	midPct := flag.Float64("mid-pct", 10, "Percentage of middle pairs to keep when --mid is set")
	allMode := flag.Bool("all", false, "Run on all pairs and write CSVs for all/long/short/mid")
	pct := flag.Float64("pct", 10, "Percentage to use for long/short/mid filtering when --all is set")
	flag.Parse()

	modeCount := 0
	if *longMode {
		modeCount++
	}
	if *shortMode {
		modeCount++
	}
	if *midMode {
		modeCount++
	}
	if *allMode {
		modeCount++
	}
	if modeCount > 1 {
		fmt.Fprintln(os.Stderr, "Cannot use --long, --short, --mid, and --all at the same time")
		os.Exit(1)
	}

	fmt.Println("Loading graph...")
	g, err := graph.LoadGraph("data/gush_dan.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load graph: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Graph loaded: %d nodes, %d edges\n", len(g.Nodes), len(g.Edges))
	fmt.Printf("CPUs: %d\n", runtime.GOMAXPROCS(0))

	// Initialize Entry Point Router (same as real server)
	router := navigation.NewEntryPointRouter(g, 2*time.Minute)
	navigation.InitializeGushDanCities(router)

	nodes := g.NodesArr
	if len(nodes) < 2 {
		fmt.Fprintln(os.Stderr, "Not enough nodes in graph")
		os.Exit(1)
	}

	// Redirect stdout during delta-stepping calls to suppress its internal print
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open /dev/null: %v\n", err)
		os.Exit(1)
	}
	defer devNull.Close()

	var allResults []result
	// var mismatches int  // uncomment if tracking ETA mismatches
	// Failure tracking - uncomment to use
	// var astarFailed []pair
	// var epFailed []pair

	for r := 1; r <= rounds; r++ {
		fmt.Printf("\n=== Round %d/%d ===\n", r, rounds)

		// Pre-generate all pairs for this round
		var pairs []pair
		for len(pairs) < pairsPerRound {
			src := nodes[rand.Intn(len(nodes))]
			dst := nodes[rand.Intn(len(nodes))]
			if src == dst {
				continue
			}
			pairs = append(pairs, pair{src, dst})
		}

		// If --long, sort by Euclidean distance and keep only the top longest
		if *longMode {
			sort.Slice(pairs, func(i, j int) bool {
				return euclideanDist(g, pairs[i].src, pairs[i].dst) > euclideanDist(g, pairs[j].src, pairs[j].dst)
			})
			keep := int(math.Ceil(float64(len(pairs)) * (*longPct) / 100.0))
			if keep < 1 {
				keep = 1
			}
			pairs = pairs[:keep]
			fmt.Printf("  --long: kept top %d pairs (%.0f%% longest by Euclidean distance)\n", keep, *longPct)
		}

		// If --short, sort by Euclidean distance and keep only the top shortest
		if *shortMode {
			sort.Slice(pairs, func(i, j int) bool {
				return euclideanDist(g, pairs[i].src, pairs[i].dst) < euclideanDist(g, pairs[j].src, pairs[j].dst)
			})
			keep := int(math.Ceil(float64(len(pairs)) * (*shortPct) / 100.0))
			if keep < 1 {
				keep = 1
			}
			pairs = pairs[:keep]
			fmt.Printf("  --short: kept top %d pairs (%.0f%% shortest by Euclidean distance)\n", keep, *shortPct)
		}

		// If --mid, sort by Euclidean distance and keep only the middle slice
		if *midMode {
			sort.Slice(pairs, func(i, j int) bool {
				return euclideanDist(g, pairs[i].src, pairs[i].dst) < euclideanDist(g, pairs[j].src, pairs[j].dst)
			})
			n := len(pairs)
			trimEachSide := (100.0 - *midPct) / 2.0
			lo := int(math.Floor(float64(n) * trimEachSide / 100.0))
			hi := int(math.Ceil(float64(n) * (100.0 - trimEachSide) / 100.0))
			if lo >= hi {
				lo = n/2 - 1
				hi = n / 2
			}
			pairs = pairs[lo:hi]
			fmt.Printf("  --mid: kept %d pairs (middle %.0f%% by Euclidean distance)\n", len(pairs), *midPct)
		}

		// Run A* on ALL pairs first
		fmt.Printf("  Running A* on %d pairs...\n", len(pairs))
		astarTimes := make([]float64, len(pairs))
		astarETAs := make([]float64, len(pairs))
		astarOk := make([]bool, len(pairs))
		for i, p := range pairs {
			start := time.Now()
			res, err := navigation.FindPathAstar(g, p.src, p.dst)
			astarTimes[i] = float64(time.Since(start).Microseconds()) / 1000.0
			if err == nil {
				astarETAs[i] = res.ETA
				astarOk[i] = true
			}
		}

		// Run EntryPoint routing on ALL pairs second
		fmt.Printf("  Running EntryPoint routing on %d pairs...\n", len(pairs))
		deltaTimes := make([]float64, len(pairs))
		deltaETAs := make([]float64, len(pairs))
		deltaOk := make([]bool, len(pairs))
		origStdout := os.Stdout
		os.Stdout = devNull
		for i, p := range pairs {
			start := time.Now()
			res, err := router.FindPathWithEntryPoints(p.src, p.dst)
			deltaTimes[i] = float64(time.Since(start).Microseconds()) / 1000.0
			if err == nil {
				deltaETAs[i] = res.ETA
				deltaOk[i] = true
			}
		}
		os.Stdout = origStdout

		// Collect results where both succeeded
		var roundResults []result
		valid := 0
		for i, p := range pairs {
			// Track failures - uncomment to use
			// if !astarOk[i] {
			// 	astarFailed = append(astarFailed, p)
			// }
			// if !deltaOk[i] {
			// 	epFailed = append(epFailed, p)
			// }
			if !deltaOk[i] || !astarOk[i] {
				continue
			}
			valid++

			// Correctness check - uncomment to track ETA mismatches
			// etaDiff := math.Abs(astarETAs[i] - deltaETAs[i])
			// tolerance := math.Max(astarETAs[i], deltaETAs[i]) * 0.01
			// if etaDiff > tolerance && etaDiff > 0.01 {
			// 	mismatches++
			// 	fmt.Fprintf(os.Stderr, "  MISMATCH pair %d: src=%d dst=%d A*=%.4f Delta=%.4f diff=%.4f min\n",
			// 		valid, p.src, p.dst, astarETAs[i], deltaETAs[i], etaDiff)
			// }

			roundResults = append(roundResults, result{
				round:   r,
				pair:    valid,
				src:     p.src,
				dst:     p.dst,
				astarMs: astarTimes[i],
				deltaMs: deltaTimes[i],
			})
		}

		allResults = append(allResults, roundResults...)
		printRoundSummary(r, roundResults)
	}

	// Write CSV(s)
	if *allMode {
		// Sort all results by Euclidean distance
		sorted := make([]result, len(allResults))
		copy(sorted, allResults)
		sort.Slice(sorted, func(i, j int) bool {
			return euclideanDist(g, sorted[i].src, sorted[i].dst) < euclideanDist(g, sorted[j].src, sorted[j].dst)
		})
		n := len(sorted)

		// All pairs
		writeCSV("benchmarks/benchmark_results.csv", allResults)

		// Short: top pct% shortest (beginning of sorted)
		keepShort := int(math.Ceil(float64(n) * (*pct) / 100.0))
		if keepShort < 1 {
			keepShort = 1
		}
		writeCSV("benchmarks/short_benchmark_results.csv", sorted[:keepShort])

		// Long: top pct% longest (end of sorted)
		keepLong := int(math.Ceil(float64(n) * (*pct) / 100.0))
		if keepLong < 1 {
			keepLong = 1
		}
		writeCSV("benchmarks/long_benchmark_results.csv", sorted[n-keepLong:])

		// Mid: middle pct%
		trimEachSide := (100.0 - *pct) / 2.0
		lo := int(math.Floor(float64(n) * trimEachSide / 100.0))
		hi := int(math.Ceil(float64(n) * (100.0 - trimEachSide) / 100.0))
		if lo >= hi {
			lo = n/2 - 1
			hi = n / 2
		}
		writeCSV("benchmarks/mid_benchmark_results.csv", sorted[lo:hi])

		fmt.Printf("\n--all: wrote 4 CSVs (all=%d, short=%d, mid=%d, long=%d rows) at %.0f%%\n",
			len(allResults), keepShort, hi-lo, keepLong, *pct)
	} else {
		csvPath := "benchmarks/benchmark_results.csv"
		if *longMode {
			csvPath = "long_benchmark_results.csv"
		} else if *shortMode {
			csvPath = "short_benchmark_results.csv"
		} else if *midMode {
			csvPath = "mid_benchmark_results.csv"
		}
		writeCSV(csvPath, allResults)
		fmt.Printf("\nCSV written to %s (%d rows)\n", csvPath, len(allResults))
	}

	// Overall summary
	fmt.Println("\n=== Overall Summary ===")
	printOverallSummary(allResults)
	// if mismatches > 0 {
	// 	fmt.Printf("\nWARNING: %d ETA mismatches detected (see stderr)\n", mismatches)
	// }

	// Print failures - uncomment to use
	// if len(astarFailed) > 0 {
	// 	fmt.Printf("\n=== A* Failures (%d pairs) ===\n", len(astarFailed))
	// 	for _, p := range astarFailed {
	// 		fmt.Printf("  src=%d dst=%d\n", p.src, p.dst)
	// 	}
	// }
	// if len(epFailed) > 0 {
	// 	fmt.Printf("\n=== EntryPoint Failures (%d pairs) ===\n", len(epFailed))
	// 	for _, p := range epFailed {
	// 		fmt.Printf("  src=%d dst=%d\n", p.src, p.dst)
	// 	}
	// }
}

func writeCSV(path string, results []result) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create CSV %s: %v\n", path, err)
		return
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"round", "pair", "src", "dst", "astar_ms", "entrypoint_ms"})
	for _, res := range results {
		w.Write([]string{
			strconv.Itoa(res.round),
			strconv.Itoa(res.pair),
			strconv.Itoa(res.src),
			strconv.Itoa(res.dst),
			fmt.Sprintf("%.3f", res.astarMs),
			fmt.Sprintf("%.3f", res.deltaMs),
		})
	}
	w.Flush()
}

func printRoundSummary(round int, results []result) {
	if len(results) == 0 {
		fmt.Printf("Round %d: no valid pairs\n", round)
		return
	}

	astarTimes := make([]float64, len(results))
	deltaTimes := make([]float64, len(results))
	for i, r := range results {
		astarTimes[i] = r.astarMs
		deltaTimes[i] = r.deltaMs
	}

	sort.Float64s(astarTimes)
	sort.Float64s(deltaTimes)

	fmt.Printf("Round %d (%d pairs):\n", round, len(results))
	fmt.Printf("  A*:    mean=%.2f  median=%.2f  min=%.2f  max=%.2f ms\n",
		mean(astarTimes), median(astarTimes), astarTimes[0], astarTimes[len(astarTimes)-1])
	fmt.Printf("  EP: mean=%.2f  median=%.2f  min=%.2f  max=%.2f ms\n",
		mean(deltaTimes), median(deltaTimes), deltaTimes[0], deltaTimes[len(deltaTimes)-1])

	speedup := mean(astarTimes) / mean(deltaTimes)
	if mean(deltaTimes) > mean(astarTimes) {
		speedup = mean(deltaTimes) / mean(astarTimes)
		fmt.Printf("  A* is %.2fx faster on average\n", speedup)
	} else {
		fmt.Printf("  EntryPoint is %.2fx faster on average\n", speedup)
	}
}

func printOverallSummary(results []result) {
	if len(results) == 0 {
		fmt.Println("No results")
		return
	}

	astarTimes := make([]float64, len(results))
	deltaTimes := make([]float64, len(results))
	for i, r := range results {
		astarTimes[i] = r.astarMs
		deltaTimes[i] = r.deltaMs
	}

	sort.Float64s(astarTimes)
	sort.Float64s(deltaTimes)

	fmt.Printf("Total pairs: %d\n", len(results))
	fmt.Printf("  A*:    mean=%.2f  median=%.2f  stddev=%.2f  min=%.2f  max=%.2f ms\n",
		mean(astarTimes), median(astarTimes), stddev(astarTimes), astarTimes[0], astarTimes[len(astarTimes)-1])
	fmt.Printf("  EP: mean=%.2f  median=%.2f  stddev=%.2f  min=%.2f  max=%.2f ms\n",
		mean(deltaTimes), median(deltaTimes), stddev(deltaTimes), deltaTimes[0], deltaTimes[len(deltaTimes)-1])

	speedup := mean(astarTimes) / mean(deltaTimes)
	if mean(deltaTimes) > mean(astarTimes) {
		speedup = mean(deltaTimes) / mean(astarTimes)
		fmt.Printf("  A* is %.2fx faster on average\n", speedup)
	} else {
		fmt.Printf("  EntryPoint is %.2fx faster on average\n", speedup)
	}
}

func mean(vals []float64) float64 {
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func median(vals []float64) float64 {
	n := len(vals)
	if n%2 == 0 {
		return (vals[n/2-1] + vals[n/2]) / 2.0
	}
	return vals[n/2]
}

func stddev(vals []float64) float64 {
	m := mean(vals)
	sum := 0.0
	for _, v := range vals {
		d := v - m
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(vals)))
}
