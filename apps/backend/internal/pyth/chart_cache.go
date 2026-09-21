package pyth

import (
	"sync"
	"time"
)

// Chart history is cached per symbol and range inside the chart client, so the
// chart route, the detail screen's stats and the list's day change share one
// upstream answer instead of each paying for it.
//
// The lifetimes follow how fast each answer can change. A 1D series is 5-minute
// bars and feeds the day change on every list row, so it lives a minute. The
// longer ranges are daily or weekly bars; ten minutes of reuse cannot move a
// visible point.
const (
	// ChartCacheDayTTL is how long a 1D series is reused.
	ChartCacheDayTTL = time.Minute
	// ChartCacheLongTTL is how long every longer range is reused.
	ChartCacheLongTTL = 10 * time.Minute
	// ChartCacheEmptyTTL caps how long an empty answer is reused. "This feed has
	// nothing in this window" is an upstream answer and does not change minute to
	// minute, but a newly listed symbol should start drawing soon after its first
	// bar exists, so an empty answer never outlives a real one of the same range.
	ChartCacheEmptyTTL = 2 * time.Minute
)

// chartCache stores resolved series with an expiry per entry. Keys come from the
// catalog (the asset routes resolve a symbol before they ask for a chart) times
// the six ranges, so the map is bounded without an eviction policy.
type chartCache struct {
	now func() time.Time

	mu      sync.RWMutex
	entries map[string]chartCacheEntry
}

type chartCacheEntry struct {
	series    AssetChartSeries
	expiresAt time.Time
}

func newChartCache(now func() time.Time) *chartCache {
	if now == nil {
		now = time.Now
	}
	return &chartCache{now: now, entries: make(map[string]chartCacheEntry)}
}

func chartCacheKey(symbol string, chartRange ChartRange) string {
	return normalizeSymbol(symbol) + ":" + string(chartRange)
}

// chartCacheTTL is the lifetime of one answer for one range.
func chartCacheTTL(chartRange ChartRange, empty bool) time.Duration {
	ttl := ChartCacheLongTTL
	if chartRange == ChartRange1D {
		ttl = ChartCacheDayTTL
	}
	if empty && ChartCacheEmptyTTL < ttl {
		ttl = ChartCacheEmptyTTL
	}
	return ttl
}

func (c *chartCache) get(symbol string, chartRange ChartRange) (AssetChartSeries, bool) {
	if c == nil {
		return AssetChartSeries{}, false
	}
	c.mu.RLock()
	entry, found := c.entries[chartCacheKey(symbol, chartRange)]
	c.mu.RUnlock()
	if !found || !c.now().Before(entry.expiresAt) {
		return AssetChartSeries{}, false
	}
	return entry.series, true
}

func (c *chartCache) set(symbol string, chartRange ChartRange, series AssetChartSeries) {
	if c == nil {
		return
	}
	ttl := chartCacheTTL(chartRange, len(series.Points) == 0)
	c.mu.Lock()
	c.entries[chartCacheKey(symbol, chartRange)] = chartCacheEntry{
		series:    series,
		expiresAt: c.now().Add(ttl),
	}
	c.mu.Unlock()
}
