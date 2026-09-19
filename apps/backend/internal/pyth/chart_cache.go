package pyth

import (
	"sync"
	"time"
)

// DefaultChartSeriesCacheTTL is how long asset chart series are reused per symbol+range.
// Hourly/daily chart windows do not need per-request Hermes fan-out; 10m balances freshness
// with rate-limit headroom when users toggle 1D/1W/1M or revisit asset detail.
const DefaultChartSeriesCacheTTL = 10 * time.Minute

// ChartSeriesCache stores symbol+range chart results with TTL expiry.
type ChartSeriesCache struct {
	ttl     time.Duration
	mu      sync.RWMutex
	entries map[string]chartSeriesCacheEntry
}

type chartSeriesCacheEntry struct {
	series    AssetChartSeries
	expiresAt time.Time
}

// NewChartSeriesCache returns a TTL cache for Hermes chart series.
func NewChartSeriesCache(ttl time.Duration) *ChartSeriesCache {
	if ttl <= 0 {
		ttl = DefaultChartSeriesCacheTTL
	}
	return &ChartSeriesCache{
		ttl:     ttl,
		entries: make(map[string]chartSeriesCacheEntry),
	}
}

func chartCacheKey(symbol string, chartRange ChartRange) string {
	return normalizeSymbol(symbol) + ":" + string(chartRange)
}

// Get returns a cached chart series when present and not expired.
func (c *ChartSeriesCache) Get(symbol string, chartRange ChartRange) (AssetChartSeries, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, found := c.entries[chartCacheKey(symbol, chartRange)]
	if !found || time.Now().After(entry.expiresAt) {
		return AssetChartSeries{}, false
	}
	return entry.series, true
}

// Set stores a chart series until TTL expiry. Only successful series (>=2 points) should be cached.
func (c *ChartSeriesCache) Set(symbol string, chartRange ChartRange, series AssetChartSeries) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[chartCacheKey(symbol, chartRange)] = chartSeriesCacheEntry{
		series:    series,
		expiresAt: time.Now().Add(c.ttl),
	}
}
