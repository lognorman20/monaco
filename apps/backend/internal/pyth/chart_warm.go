package pyth

import (
	"context"
	"log/slog"
	"time"
)

// The asset screen asks for one chart range at a time, the one whose chip is
// selected, so the first tap on any other chip went upstream on demand. With Yahoo
// rate limiting a burst, one of those taps sat on a spinner for fifteen seconds
// while every range already in the cache answered in milliseconds. Once someone is
// looking at a symbol's chart, its other ranges are the likeliest next request, and
// they can be fetched while the person is still reading the first one.
//
// The warm is deliberately polite, because the whole point is to stay under the
// vendor's rate limit. One symbol is warmed by one goroutine at a time, and only a
// couple of symbols warm at once. Ranges are fetched one after another with a
// pause between them rather than in a burst. Only the one-call history source is
// asked: the Hermes sampler costs about thirty requests per range, which is a price
// worth paying for a chart someone is waiting on and never for one they might tap.
// The first failure ends the warm and trips the same breaker a foreground fetch
// would, so a source that is down is not asked again by the warm either.

const (
	// chartRangeWarmTimeout bounds one symbol's warm. The warm outlives the request
	// that asked for it, so it cannot borrow that request's deadline.
	chartRangeWarmTimeout = 45 * time.Second
	// defaultChartRangeWarmPause spaces a warm's requests out. Five back-to-back
	// chart calls from one client is the shape of burst that got rate limited.
	defaultChartRangeWarmPause = 250 * time.Millisecond
	// maxConcurrentChartRangeWarms bounds how many symbols warm at once across the
	// process. A warm that cannot get a slot is dropped rather than queued: the next
	// chart request for that symbol asks again, and a chip that is not warm yet
	// still loads on demand as it always did.
	maxConcurrentChartRangeWarms = 2
)

// ChartRangeWarmer is implemented by chart clients that can fill a symbol's other
// ranges ahead of the tap that would ask for them. It returns at once; the work
// happens in the background.
type ChartRangeWarmer interface {
	WarmChartRanges(symbol string)
}

// WithoutChartRangeWarm turns range warming off. Tests that count upstream calls
// use it so a background fill cannot land in the middle of their arithmetic.
func (c *HermesClient) WithoutChartRangeWarm() *HermesClient {
	c.rangeWarmOff = true
	return c
}

// WarmChartRanges fetches, in the background, every range of symbol the chart
// cache does not hold yet. A symbol already warming is left alone, and so is one
// whose ranges are all cached, which is what every request after the first finds.
func (c *HermesClient) WarmChartRanges(symbol string) {
	c.startChartRangeWarm(symbol)
}

// startChartRangeWarm starts a warm and returns a channel that is closed when it
// ends, or nil when no warm was started. Tests wait on the channel; production
// callers do not need to.
func (c *HermesClient) startChartRangeWarm(symbol string) <-chan struct{} {
	if c.rangeWarmOff || c.seriesSource == nil || c.chartCache == nil {
		return nil
	}
	if !c.seriesBreaker.allows(time.Now()) {
		// The source failed recently. The chart path is falling back to the sampler
		// for now, and the warm has no fallback it is willing to pay for.
		return nil
	}
	key := normalizeSymbol(symbol)
	if key == "" || !c.hasUncachedChartRange(symbol) {
		return nil
	}

	c.rangeWarmMu.Lock()
	if c.rangeWarming == nil {
		c.rangeWarming = make(map[string]struct{})
	}
	if _, busy := c.rangeWarming[key]; busy || len(c.rangeWarming) >= maxConcurrentChartRangeWarms {
		c.rangeWarmMu.Unlock()
		return nil
	}
	c.rangeWarming[key] = struct{}{}
	c.rangeWarmMu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			c.rangeWarmMu.Lock()
			delete(c.rangeWarming, key)
			c.rangeWarmMu.Unlock()
		}()
		// Detached from the request on purpose: the person who opened the chart is
		// not waiting on this, and their request ending must not cancel it.
		ctx, cancel := context.WithTimeout(context.Background(), chartRangeWarmTimeout)
		defer cancel()
		c.warmChartRanges(ctx, symbol)
	}()
	return done
}

func (c *HermesClient) hasUncachedChartRange(symbol string) bool {
	for _, chartRange := range ChartRanges {
		if _, cached := c.chartCache.Get(symbol, chartRange); !cached {
			return true
		}
	}
	return false
}

// warmChartRanges fetches the uncached ranges one at a time. The cache is checked
// again before every fetch, not only once up front, because the person may tap a
// chip while the warm is running and that request fills its range itself.
func (c *HermesClient) warmChartRanges(ctx context.Context, symbol string) {
	warmed := 0
	var stopped error
	for _, chartRange := range ChartRanges {
		if _, cached := c.chartCache.Get(symbol, chartRange); cached {
			continue
		}
		if warmed > 0 {
			if err := pause(ctx, c.rangeWarmPause); err != nil {
				stopped = err
				break
			}
		}
		series, err := c.sourceSeries(ctx, symbol, chartRange, time.Now().UTC())
		if err != nil {
			stopped = err
			break
		}
		c.cacheChartSeries(symbol, chartRange, series)
		warmed++
	}
	logChartRangeWarm(symbol, warmed, stopped)
}

// pause waits for d or until ctx ends, whichever comes first.
func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func logChartRangeWarm(symbol string, rangeCount int, stopped error) {
	if stopped != nil {
		slog.Debug("pyth chart range warm stopped", "symbol", symbol, "range_count", rangeCount, "err", stopped)
		return
	}
	slog.Debug("pyth chart range warm", "symbol", symbol, "range_count", rangeCount)
}
