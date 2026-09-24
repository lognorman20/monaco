package pyth

import (
	"sync"
	"time"
)

// DefaultChartSeriesCacheTTL is how long asset chart series are reused per symbol+range.
// Hourly/daily chart windows do not need per-request Hermes fan-out; 10m balances freshness
// with rate-limit headroom when users toggle 1D/1W/1M or revisit asset detail.
const DefaultChartSeriesCacheTTL = 10 * time.Minute

// DefaultChartSeriesEmptyCacheTTL is how long an empty answer is reused. Not
// caching it at all meant a symbol with no history in the window went back
// upstream on every request — and the asset detail route, which the plan polls
// every 5-10s, asks for two ranges. "This feed has nothing here" is as much an
// answer as a series is, and it does not change minute to minute. It is kept far
// shorter than a successful series so a newly listed symbol starts drawing soon
// after its first bar exists.
const DefaultChartSeriesEmptyCacheTTL = 2 * time.Minute

// ChartSeriesCache stores symbol+range chart results with TTL expiry.
type ChartSeriesCache struct {
	ttl      time.Duration
	emptyTTL time.Duration
	mu       sync.RWMutex
	entries  map[string]chartSeriesCacheEntry
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
	emptyTTL := DefaultChartSeriesEmptyCacheTTL
	if ttl < emptyTTL {
		emptyTTL = ttl
	}
	return &ChartSeriesCache{
		ttl:      ttl,
		emptyTTL: emptyTTL,
		entries:  make(map[string]chartSeriesCacheEntry),
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

// Set stores a chart series until TTL expiry. An empty series is stored too, under
// the shorter empty TTL: it is an upstream answer, and re-asking for it on every
// request is what made a symbol with no history the most expensive one to look at.
func (c *ChartSeriesCache) Set(symbol string, chartRange ChartRange, series AssetChartSeries) {
	ttl := c.ttl
	if len(series.Points) == 0 {
		ttl = c.emptyTTL
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[chartCacheKey(symbol, chartRange)] = chartSeriesCacheEntry{
		series:    series,
		expiresAt: time.Now().Add(ttl),
	}
}
