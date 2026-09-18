package xstocks

import (
	"context"
	"sync"
	"time"
)

// DefaultRoutabilityCacheTTL is how long Jupiter routability probe results are reused.
const DefaultRoutabilityCacheTTL = 30 * time.Minute

// RoutabilityProber checks whether USDC→xStock Jupiter routing exists for a catalog asset.
type RoutabilityProber interface {
	IsRoutable(ctx context.Context, asset CatalogAsset) bool
}

// RoutabilityCache stores mint→routable probe results with TTL expiry.
type RoutabilityCache struct {
	ttl     time.Duration
	mu      sync.RWMutex
	entries map[string]routabilityCacheEntry
}

type routabilityCacheEntry struct {
	routable  bool
	expiresAt time.Time
}

// NewRoutabilityCache returns a TTL cache for Jupiter routability probes.
func NewRoutabilityCache(ttl time.Duration) *RoutabilityCache {
	if ttl <= 0 {
		ttl = DefaultRoutabilityCacheTTL
	}
	return &RoutabilityCache{
		ttl:     ttl,
		entries: make(map[string]routabilityCacheEntry),
	}
}

// Get returns a cached routability result when present and not expired.
func (c *RoutabilityCache) Get(mint string) (routable bool, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, found := c.entries[mint]
	if !found || time.Now().After(entry.expiresAt) {
		return false, false
	}
	return entry.routable, true
}

// Set stores a routability probe result until TTL expiry.
func (c *RoutabilityCache) Set(mint string, routable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[mint] = routabilityCacheEntry{
		routable:  routable,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// CachedRoutabilityProber wraps a prober with mint-keyed TTL caching.
type CachedRoutabilityProber struct {
	inner RoutabilityProber
	cache *RoutabilityCache
}

// NewCachedRoutabilityProber caches inner probe results per Solana mint.
func NewCachedRoutabilityProber(inner RoutabilityProber, cache *RoutabilityCache) *CachedRoutabilityProber {
	if cache == nil {
		cache = NewRoutabilityCache(DefaultRoutabilityCacheTTL)
	}
	return &CachedRoutabilityProber{inner: inner, cache: cache}
}

func (p *CachedRoutabilityProber) IsRoutable(ctx context.Context, asset CatalogAsset) bool {
	mint := asset.SolanaMint
	if routable, ok := p.cache.Get(mint); ok {
		return routable
	}
	routable := p.inner.IsRoutable(ctx, asset)
	p.cache.Set(mint, routable)
	return routable
}

type fakeRoutabilityProber struct {
	mu       sync.Mutex
	routable map[string]bool
	defaultR bool
}

// NewFakeRoutabilityProber returns a test prober keyed by Solana mint.
func NewFakeRoutabilityProber(defaultRoutable bool) *fakeRoutabilityProber {
	return &fakeRoutabilityProber{
		routable: make(map[string]bool),
		defaultR: defaultRoutable,
	}
}

// SetRoutable configures routability for a mint on the fake prober.
func SetRoutable(prober RoutabilityProber, mint string, routable bool) {
	fake, ok := prober.(*fakeRoutabilityProber)
	if !ok {
		panic("xstocks: SetRoutable requires NewFakeRoutabilityProber")
	}
	fake.mu.Lock()
	fake.routable[mint] = routable
	fake.mu.Unlock()
}

func (f *fakeRoutabilityProber) IsRoutable(_ context.Context, asset CatalogAsset) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if routable, ok := f.routable[asset.SolanaMint]; ok {
		return routable
	}
	return f.defaultR
}

// rankCatalogAssets probes routability when prober is set, then sorts matches.
func rankCatalogAssets(ctx context.Context, prober RoutabilityProber, matches []CatalogAsset) {
	for i := range matches {
		if prober == nil {
			matches[i].Routable = true
			continue
		}
		matches[i].Routable = prober.IsRoutable(ctx, matches[i])
	}
	sortCatalogMatches(matches)
}
