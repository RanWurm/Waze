package server

import (
	"sync"
	"time"
	"waze/internal/types"
)

type RouteKey struct {
	From int
	To   int
}

type CachedRouted struct {
	// Response     []int
	Response  types.NavigationResponse
	CreatedAt time.Time
}

type RouteCache struct {
	store       map[RouteKey]CachedRouted // check if the route is already in the cache
	mu          sync.RWMutex
	ttl         time.Duration
	lastCleanup time.Time
}

func NewRouteCache(ttlSeconds int) *RouteCache {
	return &RouteCache{
		store:       make(map[RouteKey]CachedRouted),
		ttl:         time.Duration(ttlSeconds) * time.Second,
		lastCleanup: time.Now(),
	}
}

// get route from the cache
func (cache *RouteCache) Get(from, to int) (types.NavigationResponse, bool) {
	key := RouteKey{From: from, To: to}

	cache.mu.RLock()
	defer cache.mu.RUnlock()

	// check if the route exists in the cache
	entry, exist := cache.store[key]
	if !exist {
		return types.NavigationResponse{}, false
	}

	// check if the route is still relevant
	if time.Since(entry.CreatedAt) > cache.ttl {
		return types.NavigationResponse{}, false
	}

	//return the route
	return entry.Response, true
}

// set route to cache
func (cache *RouteCache) Set(from, to int, response types.NavigationResponse) {
	key := RouteKey{From: from, To: to}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.store[key] = CachedRouted{
		Response:  response,
		CreatedAt: time.Now(),
	}

	// Periodic cleanup to prevent unbounded growth
	if time.Since(cache.lastCleanup) > 30*time.Second {
		cache.cleanupExpired()
		cache.lastCleanup = time.Now()
	}
}

// cleanupExpired removes expired cache entries (must be called with lock held)
func (cache *RouteCache) cleanupExpired() {
	for key, entry := range cache.store {
		if time.Since(entry.CreatedAt) > cache.ttl {
			delete(cache.store, key)
		}
	}
}
