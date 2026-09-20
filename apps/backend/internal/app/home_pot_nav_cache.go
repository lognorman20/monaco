package app

import (
	"context"
	"sync"
)

type homePotNavCacheKey struct{}

type homePotNavCacheEntry struct {
	potNav      int64
	totalShares int64
	err         error
}

type homePotNavCache struct {
	mu       sync.Mutex
	entries  map[string]homePotNavCacheEntry
	inflight map[string]*sync.WaitGroup
	computes int
}

// HomeContextWithPotNavCache attaches a per-request pot NAV cache to ctx.
func HomeContextWithPotNavCache(ctx context.Context) context.Context {
	if _, ok := ctx.Value(homePotNavCacheKey{}).(*homePotNavCache); ok {
		return ctx
	}
	return context.WithValue(ctx, homePotNavCacheKey{}, &homePotNavCache{
		entries:  make(map[string]homePotNavCacheEntry),
		inflight: make(map[string]*sync.WaitGroup),
	})
}

func homePotNavCacheFrom(ctx context.Context) *homePotNavCache {
	cache, _ := ctx.Value(homePotNavCacheKey{}).(*homePotNavCache)
	return cache
}

// HomePotNavComputeCount returns uncached groupPotNavAndShares work in this request. Test hook.
func HomePotNavComputeCount(ctx context.Context) int {
	cache := homePotNavCacheFrom(ctx)
	if cache == nil {
		return 0
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.computes
}
