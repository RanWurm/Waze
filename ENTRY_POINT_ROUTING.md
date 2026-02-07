# Entry Point Routing System

## Overview

This implementation showcases **shared computation** and **parallel processing** for navigation queries - perfect for a distributed systems and parallelism course project.

## How It Works

### 1. Preprocessing (Offline)

Cities in Gush Dan are identified with their entry points:

```
Tel Aviv: 5 entry points
Ramat Gan: 4 entry points
Petah Tikva: 4 entry points
Bnei Brak: 3 entry points
... and more
```

Entry points are automatically identified as boundary nodes with high connectivity to external roads.

### 2. Query Time (Online with Caching)

When User A requests a route from **Ramat Gan → Petah Tikva**:

```
Step 1: Identify destination city (Petah Tikva)
Step 2: Get entry points for Petah Tikva (4 nodes)
Step 3: Compute backward searches FROM each entry point (IN PARALLEL)
        - 4 threads compute simultaneously
        - Each computes distances to ALL nodes in Petah Tikva
        - Results cached for 2 minutes
Step 4: Compute forward path from Ramat Gan to each entry
        - 4 parallel A* searches
Step 5: Combine: forward + cached backward, pick best
```

### 3. Shared Computation

When User B requests a route from **Different location in Ramat Gan → Different location in Petah Tikva**:

```
Step 1: Identify destination city (Petah Tikva)
Step 2: Get entry points for Petah Tikva (4 nodes)
Step 3: Check cache - FOUND! Reuse User A's backward searches ✓
Step 4: Compute forward path from new starting point
Step 5: Combine: forward + REUSED backward, pick best
```

**Result:** User B's query is ~2-3x faster because backward searches are shared!

## Key Features for Course Project

### Parallelism
- **Parallel backward search computation** (Step 3): Multiple threads compute different entry points simultaneously
- **Parallel forward path computation** (Step 4): Multiple candidate paths computed in parallel
- **Lock-free cache reads**: Many users can read cached results simultaneously

### Distribution
- Different servers can handle different cities
- Cache can be distributed (e.g., Redis)
- Entry points can be partitioned across workers

### Shared Computation
- Backward searches computed ONCE, used by MANY users
- Cache TTL allows dynamic updates with traffic changes
- Demonstrates real-world optimization technique

## Files

- `internal/navigation/entry_points.go` - Entry point identification
- `internal/navigation/backward_cache.go` - Caching system with TTL
- `internal/navigation/entry_point_routing.go` - Main routing algorithm
- `internal/navigation/gush_dan_cities.go` - City definitions
- `cmd/test_entry_points/main.go` - Demo showing cache benefits

## Running the Demo

```bash
# Run the test to see cache performance
go run cmd/test_entry_points/main.go
```

Output shows:
- How entry points are identified
- First query: computes backward searches
- Second query: reuses cached backward searches (faster!)
- Third query: also reuses cache (faster!)

## Integration with Server

The server automatically uses entry point routing:

```bash
make run_server
```

Check logs for cache hit/miss messages:
```
[COMPUTING] Computing 4 backward searches in parallel for Petah Tikva...
[CACHED] Stored 4 new backward searches for Petah Tikva (TTL: 2m0s)
[CACHE HIT] Reusing 4/4 cached backward searches for Petah Tikva
```

## Configuration

In `config.json`:
- Cache TTL: Hardcoded to 2 minutes (can be made configurable)
- Number of entry points per city: Defined in `gush_dan_cities.go`

## Performance Benefits

**Without entry points:**
- Every query: Full A* from scratch
- 1000 users to Petah Tikva = 1000 independent computations

**With entry points:**
- First query: Compute + cache backward searches
- Next 999 users: Reuse cached backward searches
- Effective sharing: ~50-70% computation saved

## Trade-offs

**Pros:**
- Massive speedup for common destination cities
- Handles dynamic traffic (cache expires and recomputes)
- Easy to distribute across servers

**Cons:**
- Less effective for intra-city trips
- Cache misses on first query or after expiry
- Memory overhead for cache storage
