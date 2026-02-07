# Waze - Parallel Navigation System

A high-performance navigation system built with Go, demonstrating advanced parallelism and distributed systems concepts for real-time route planning with dynamic traffic across the Gush Dan metropolitan area (~113K nodes, ~300K edges).

## 🎯 Project Overview

This project implements a Waze-like navigation system focusing on:
- **Parallel Delta-Stepping algorithm** for shortest path computation
- **Entry-point based routing** for efficient inter-city navigation
- **Shared backward search computation** between users to minimize redundant work
- **Real-time traffic processing** with parallel batch updates
- **Dynamic graph updates** reflecting current traffic conditions
- **Live GUI** with car visualization and route rendering via WebSocket

Built for a course on Parallelism and Distributed Systems.

## 🏗️ Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────────┐
│                      HTTP Server (:8080)                     │
├──────────────────┬──────────────────┬──────────────────────┤
│  API Endpoints   │  WebSocket Hub   │   Static File Server │
└────────┬─────────┴────────┬─────────┴──────────────────────┘
         │                  │
         ▼                  ▼
    ┌────────────┐    ┌──────────┐
    │ Job Queue  │    │   Hub    │
    │  Workers   │    │Broadcast │
    └─────┬──────┘    └──────────┘
          │
          ▼
    ┌──────────────────────────────────────┐
    │   Entry Point Router                 │
    │  ┌────────────────────────────────┐  │
    │  │ Entry Point Manager            │  │
    │  │ - City boundaries (10 cities)  │  │
    │  │ - Entry point identification   │  │
    │  └────────────────────────────────┘  │
    │  ┌────────────────────────────────┐  │
    │  │ Backward Search Cache (TTL)    │  │
    │  │ - Delta-Stepping backward      │  │
    │  │ - Shared between users         │  │
    │  └────────────────────────────────┘  │
    └───────────────┬──────────────────────┘
                    │
                    ▼
            ┌──────────────────┐
            │ Graph (RWMutex)  │
            │ - Nodes (~113K)  │
            │ - Edges (~300K)  │
            │ - Adj Lists      │
            │ - Dense Index    │
            └──────────────────┘
```

### Worker Pool

```
Request → JobQueue → [Worker 1] → Entry-Point Routing / Δ-Stepping
                   → [Worker 2] → Entry-Point Routing / Δ-Stepping
                   → [Worker 3] → Entry-Point Routing / Δ-Stepping
                   → ...
                   → [Worker N] → Entry-Point Routing / Δ-Stepping
```

Workers run concurrently, processing navigation requests in parallel. Each worker uses entry-point routing when the destination is in a known city, falling back to Delta-Stepping for same-city or unmapped routes.

## 🚀 Key Features

### 1. Entry-Point Based Routing

**Problem:** Multiple users going to the same city require redundant computation.

**Solution:**
- Identify city boundaries and entry points (major intersections)
- Compute backward searches FROM entry points once
- Cache results (TTL: 2 minutes)
- Share cached results between users

**Example:**
```
User 1: Ramat Gan → Petah Tikva
  ├─ Compute 4 backward searches from Petah Tikva entries (parallel)
  ├─ Cache results
  └─ Time: 100ms

User 2: Different location → Different location in Petah Tikva
  ├─ Reuse cached backward searches ✓
  └─ Time: 30ms (3x faster!)
```

**Cities Supported:**
- Tel Aviv (5 entry points)
- Ramat Gan (4 entry points)
- Petah Tikva (4 entry points)
- Bnei Brak (3 entry points)
- Holon, Bat Yam, Herzliya, Rishon LeZion, and more

### 2. Delta-Stepping Algorithm (Parallel SSSP)

The system uses the Delta-Stepping algorithm for shortest path computation, both forward and backward:

- **Bucket-based relaxation** with configurable delta (auto-computed from average edge weight)
- **Parallel light-edge relaxation** within each bucket using goroutines
- **Map-based state** to avoid allocating arrays for all ~113K nodes
- **Backward variant** used for entry-point backward searches, restricted to city nodes

### 3. Parallel Traffic Processing

Traffic reports are processed in batches with automatic parallelization:

```go
if batchSize >= 500 && CPUs > 1:
    // Parallel processing
    workers = min(batchSize/500, maxCPUs)
    partition reports into chunks
    process chunks in parallel
    aggregate results
else:
    // Serial processing
    process sequentially
```

**Features:**
- Automatic parallelism threshold
- Dynamic worker allocation
- Lock-free local aggregation with reusable maps (avoids per-tick allocations)
- Synchronized graph updates

### 4. Route Caching

Two-level caching system:

**Level 1: Full Route Cache**
- Stores complete route calculations
- TTL: 5 seconds (configurable)
- Key: (from_node, to_node)
- Periodic cleanup of expired entries to prevent unbounded growth

**Level 2: Backward Search Cache**
- Stores entry-point backward searches (Delta-Stepping results)
- TTL: 2 minutes
- Key: (city, entry_node)
- Shared between users

### 5. Parallel Path Computation

When using entry-point routing:

```
For destination city with N entry points:
  Spawn N goroutines:
    - Goroutine 1: Δ-Stepping forward to entry 1
    - Goroutine 2: Δ-Stepping forward to entry 2
    ...
    - Goroutine N: Δ-Stepping forward to entry N
  Wait for all
  Combine forward + cached backward search
  Select best path
```

## 📊 Parallelism & Distribution

### Parallelism Demonstrated

1. **Worker Pool Concurrency**
   - Multiple workers process requests simultaneously
   - Buffered job queue (capacity: 100)
   - Configurable worker count (default: 8)

2. **Delta-Stepping Parallel Relaxation**
   - Light edges relaxed in parallel within each bucket
   - Worker goroutines with chunk-based partitioning
   - Used for both forward and backward searches

3. **Parallel Backward Search Computation**
   - Multiple entry points computed simultaneously
   - Each in separate goroutine with WaitGroup synchronization

4. **Parallel Traffic Batch Processing**
   - Automatic partitioning of large batches
   - Reusable local maps to minimize GC pressure
   - Lock-free local aggregation, synchronized merge

5. **Parallel Density Calculation**
   - 8 workers partition car list for edge density computation
   - Reusable map pools to avoid per-tick allocations

6. **Concurrent Cache Access**
   - RWMutex allows multiple concurrent readers
   - Write locks only for cache updates
   - Minimal contention on cache hits

### Distribution Ready

The architecture supports distributed deployment:

**City-Based Partitioning:**
```
Server 1: Handles Tel Aviv backward searches
Server 2: Handles Petah Tikva backward searches
Server 3: Handles Ramat Gan backward searches
...
Load Balancer: Routes users to appropriate servers
```

**Cache Distribution:**
- Current: In-memory cache per server
- Upgrade path: Redis/Memcached for shared distributed cache
- Allows horizontal scaling

## 🔧 Installation & Running

### Prerequisites

- Go 1.21+
- Map data file (included: `data/gush_dan.json`, ~113K nodes covering the Gush Dan area)

### Quick Start

**1. Start the Server**

```bash
make run_server
```

Or manually:
```bash
go run cmd/server/main.go
```

Server starts on `http://localhost:8080`. On startup it loads the graph, computes the Delta-Stepping delta parameter, and initializes entry points for 10 Gush Dan cities. A memory watchdog kills the process if heap usage exceeds 4 GB.

**2. Open the GUI**

Navigate to `http://localhost:8080` in a browser. Enter node IDs manually (range 1–113157) to compute and visualize routes. The GUI shows:
- Computed routes on an OpenStreetMap tile layer
- Simulation cars as colored dots with their routes
- A car emoji for user-driven navigation

**3. (Optional) Run Traffic Simulation**

In a separate terminal:
```bash
make run_sim
```

Or manually:
```bash
go run ./cmd/simulation
```

This spawns up to 1000 simulated cars that navigate using the server's routing API and report traffic data. The simulation logs periodic status updates and exits automatically when all cars arrive.

**4. Test Entry-Point Routing**

```bash
go run cmd/test_entry_points/main.go
```

Shows cache performance with multiple users going to the same city.

### Build

```bash
# Build server binary
make build

# Clean build artifacts
make clean
```

## 📡 API Endpoints

### Navigation

**GET** `/api/navigate?from={node_id}&to={node_id}`

Request a route between two nodes.

**Response:**
```json
{
  "route": [123, 456, 789],
  "route_edges": [
    {
      "id": 123,
      "from": 100,
      "to": 200,
      "from_x": 34.7818,
      "from_y": 32.0853,
      "to_x": 34.7820,
      "to_y": 32.0855,
      "length": 0.25,
      "speed_limit": 50
    }
  ],
  "eta": 12.5,
  "distance": 8.2
}
```

### Traffic Updates

**POST** `/api/traffic`

Submit traffic reports (batch).

**Request:**
```json
[
  {
    "car_id": 1,
    "edge_id": 123,
    "speed": 45.5,
    "timestamp": 1234567890
  }
]
```

**Response:** `200 OK`

### WebSocket

**WS** `/ws`

Real-time updates for GUI.

**Messages:**
- `init`: Graph data (nodes, edges)
- `cars`: Car positions update

## ⚙️ Configuration

Edit `config.json`:

```json
{
  "server": {
    "server_port": ":8080",
    "map_file": "data/gush_dan.json",
    "cache_ttl": 5
  },
  "simulation": {
    "server_url": "http://localhost",
    "num_cars": 1000,
    "spawn_rate": 5.0,
    "report_interval": 5,
    "end_spawn": 60.0,
    "delta_time": 1.0
  },
  "physics": {
    "car_length_km": 0.005,
    "density_threshold": 0.85,
    "speed_factor": 0.2,
    "alpha": 0.2,
    "edge_density": 0.3
  },
  "max_cpus": 8
}
```

### Key Parameters

- `server_port`: HTTP server port
- `map_file`: Path to graph JSON file (`data/gush_dan.json` for the full Gush Dan map)
- `cache_ttl`: Route cache TTL in seconds
- `num_cars`: Number of simulated cars
- `spawn_rate`: Seconds between car spawns
- `end_spawn`: Stop spawning cars after this many simulation seconds
- `max_cpus`: Maximum CPU cores to use

## 📁 Project Structure

```
waze/
├── cmd/
│   ├── server/              # HTTP server (+ memory watchdog)
│   │   └── main.go
│   ├── simulation/          # Traffic simulator
│   │   ├── main.go
│   │   └── scenarios.go
│   └── test_entry_points/   # Demo entry-point routing & cache
│       └── main.go
├── internal/
│   ├── config/              # Configuration management
│   ├── graph/               # Graph data structures
│   │   ├── graph.go         # Graph struct (+ dense index for Δ-Stepping)
│   │   ├── node.go
│   │   ├── edge.go
│   │   └── loader.go        # Loads JSON graph, builds dense index & delta
│   ├── navigation/          # Routing algorithms
│   │   ├── astar.go                   # A* implementation
│   │   ├── bidirectional_astar.go
│   │   ├── delta_stepping.go          # Parallel Δ-Stepping (forward)
│   │   ├── delta_stepping_backward.go # Parallel Δ-Stepping (backward, city-scoped)
│   │   ├── entry_points.go            # City & entry point identification
│   │   ├── backward_cache.go          # Backward search caching (TTL)
│   │   ├── entry_point_routing.go     # Main routing logic
│   │   ├── gush_dan_cities.go         # 10 Gush Dan city definitions
│   │   ├── heuristic.go
│   │   ├── queue.go
│   │   └── types.go
│   ├── server/              # HTTP handlers & workers
│   │   ├── server.go        # Request handlers (+ car sampling for GUI)
│   │   ├── worker.go        # Worker pool (entry-point routing + Δ-Stepping)
│   │   ├── cache.go         # Route cache (+ periodic cleanup)
│   │   ├── websocket.go     # WebSocket hub (lightweight init)
│   │   └── types.go
│   ├── sim/                 # Simulation logic
│   │   ├── client.go
│   │   ├── world.go         # World (+ reusable density maps)
│   │   └── random.go
│   └── types/               # Shared types (EdgeInfo, TrafficReport)
├── data/
│   ├── gush_dan.json        # Main map (~113K nodes, Gush Dan area)
│   ├── gush_dan.osm         # Raw OSM data
│   └── overpass_query.txt   # Overpass API query used to fetch the data
├── web/                     # GUI (MapLibre + WebSocket)
│   └── index.html
├── config.json              # Configuration
├── Makefile
└── README.md
```

## 🧪 Testing & Benchmarking

### Test Entry-Point Routing

```bash
go run cmd/test_entry_points/main.go
```

**Expected Output:**
```
=== Initializing Entry Point Router ===
Identifying entry points for Tel Aviv...
  Found 5 entry points for Tel Aviv: [123, 456, ...]

=== Simulating User Queries ===
--- User 1 Query ---
[COMPUTING] Computing 4 backward searches in parallel for Petah Tikva...
User 1: 1523 -> 2847 | Distance: 8.2 km | ETA: 12.5 min | Time: 85ms

--- User 2 Query (should use cache) ---
[CACHE HIT] Reusing 4/4 cached backward searches for Petah Tikva
User 2: 1621 -> 2954 | Distance: 7.8 km | ETA: 11.2 min | Time: 28ms
Speedup from caching: 3.04x
```

### Monitor Cache Performance

When running the server, watch for:
```
[CACHE HIT] Reusing 4/4 cached backward searches for Petah Tikva
[COMPUTING] Computing 4 backward searches in parallel for Tel Aviv...
[CACHED] Stored 4 new backward searches for Tel Aviv (TTL: 2m0s)
```

### Performance Metrics

On Gush Dan map (~150k nodes, ~300k edges):

| Scenario | Time | Cache |
|----------|------|-------|
| First query to city | ~80-100ms | Miss |
| Subsequent queries | ~25-35ms | Hit |
| **Speedup** | **~3x** | |

## 🎓 Academic Value

### Parallelism Concepts Demonstrated

- ✅ Parallel algorithm design (Delta-Stepping, entry-point routing)
- ✅ Thread synchronization (RWMutex, WaitGroups, channels)
- ✅ Lock-free data structures (local aggregation, reusable map pools)
- ✅ Load balancing (worker pool, chunk-based partitioning)
- ✅ Performance analysis (cache speedup, memory optimization)

### Distributed Systems Concepts

- ✅ Shared computation patterns
- ✅ Caching strategies with TTL
- ✅ Horizontal scalability design
- ✅ Partition strategies (city-based)
- ✅ Trade-offs (memory vs computation, consistency vs performance)

### Real-World Techniques

- Based on actual navigation system optimizations
- Real map data from OpenStreetMap (Gush Dan area via Overpass API)
- Handles dynamic data (traffic updates from simulated cars)
- Production-ready patterns (worker pools, caching, memory watchdog)
- Measurable performance improvements (cache hit speedup ~3x)

## 🔍 Algorithm Details

### Delta-Stepping (Parallel SSSP)

The primary pathfinding algorithm, replacing A* for inter-city routes:
- **Bucket-based relaxation:** Nodes are placed in buckets by tentative distance (bucket index = ⌊dist/delta⌋)
- **Parallel light-edge processing:** Within each bucket, light edges (weight < delta) are relaxed in parallel across multiple goroutines
- **Heavy edges:** Processed sequentially after a bucket is drained
- **Map-based state:** Only allocates for visited nodes, not the full 113K-node graph
- **Auto-computed delta:** Calculated as the average edge weight (time in hours) at graph load time
- **Backward variant:** Operates on reverse adjacency lists, optionally restricted to a set of city nodes

### A* Search

Used as a fallback and for comparison:
- Haversine distance heuristic
- Priority queue (min-heap)
- Current traffic-aware edge weights
- Time-based cost function (distance/speed)

### Entry-Point Routing

1. **Identify destination city** (by node-to-city mapping)
2. **Check cache** for backward Delta-Stepping results from entry points
3. **If cache miss:**
   - Compute backward Delta-Stepping from each entry point (parallel goroutines)
   - Store in cache (TTL: 2 minutes)
4. **Compute forward Delta-Stepping** to each entry point (parallel goroutines)
5. **Combine:** forward path + cached backward distances
6. **Select best path** (lowest total cost)

**Fallback:** If destination is not in a mapped city or source and destination are in the same city, use direct Delta-Stepping.

### Traffic Updates

**Serial (small batches):**
```
For each report:
  Find edge
  Update speed (exponential moving average)
```

**Parallel (large batches ≥500):**
```
Partition into chunks
For each chunk (parallel):
  Aggregate locally (map[edge_id] -> avg_speed)
Merge aggregations
Apply to graph (single lock)
```

## 🚦 Traffic Simulation

The simulator generates realistic traffic:
- Up to 1000 cars spawn at random nodes over 60 simulation seconds
- Navigate using the server's routing API (entry-point routing + Delta-Stepping)
- Report speed based on edge density (congestion), speed limits, and random variation
- ~10% of cars (selected by ID) include their full route in reports for GUI display
- Periodic status logs show car counts by state (driving, waiting, idle)
- Simulation exits automatically when spawning is done and all cars have arrived
- Parallel density calculation with 8 workers and reusable map pools

## 📈 Future Enhancements

- [ ] Add Redis for distributed cache
- [ ] Metrics dashboard (cache hit rate, latency, throughput)
- [ ] Machine learning for entry point optimization
- [ ] Traffic prediction based on historical data
- [ ] Multi-modal routing (car, public transit, walking)

## 📄 License

Academic project for parallelism and distributed systems course.

## 🤝 Contributing

This is a course project. For questions or suggestions, please open an issue.

---

**Built with Go • Powered by Parallelism • Optimized for Real-Time Navigation**
