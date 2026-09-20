package pyth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	chartHistoricalConcurrency = 6
	chartHistoricalMaxRetries  = 3
)

// AssetMark is a standalone equity mark for catalog pricing.
type AssetMark struct {
	PriceUsdcMicros int64
	Change24h       *string
}

// ChartPoint is one chart sample. The OHLC fields are populated by sources that
// serve candles (Benchmarks) and left at zero by sources that only know a price at
// an instant (the Hermes fallback), so a caller can tell the two apart.
type ChartPoint struct {
	Timestamp       int64 `json:"timestamp"`
	PriceUsdcMicros int64 `json:"priceUsdcMicros"`
	OpenUsdcMicros  int64 `json:"openUsdcMicros,omitempty"`
	HighUsdcMicros  int64 `json:"highUsdcMicros,omitempty"`
	LowUsdcMicros   int64 `json:"lowUsdcMicros,omitempty"`
}

// AssetChartSeries is a time series for Swift Charts.
type AssetChartSeries struct {
	Points      []ChartPoint `json:"points"`
	EmptyReason string       `json:"emptyReason,omitempty"`
	// PreviousCloseUsdcMicros is the last close before the window opened — the
	// baseline a day chart measures its change against. Nil when the source could
	// not reach back far enough to know one.
	PreviousCloseUsdcMicros *int64 `json:"previousCloseUsdcMicros,omitempty"`
	// Range echoes the window this series was built for, so a late response can be
	// matched to the chip that asked for it.
	Range ChartRange `json:"range,omitempty"`
	// Source names which upstream produced the series.
	Source string `json:"source,omitempty"`
	// Handler logging only; omitted from JSON responses.
	RequestedSamples int `json:"-"`
	FailedSamples    int `json:"-"`
}

// AssetPriceClient fetches standalone marks and chart history.
type AssetPriceClient interface {
	AssetMark(ctx context.Context, symbol string) (AssetMark, error)
	ChartSeries(ctx context.Context, symbol string, chartRange ChartRange) (AssetChartSeries, error)
}

func (c *HermesClient) AssetMark(ctx context.Context, symbol string) (AssetMark, error) {
	marked, err := c.markHolding(ctx, CostBasis{
		Symbol: symbol,
		Units:  1,
		Price:  0,
		Amount: 1,
	})
	if err != nil {
		return AssetMark{}, err
	}

	mark := AssetMark{PriceUsdcMicros: marked.MarkUsdc}
	if change, ok, err := c.change24h(ctx, symbol, marked.MarkUsdc); err == nil && ok {
		mark.Change24h = &change
	}
	return mark, nil
}

func (c *HermesClient) ChartSeries(ctx context.Context, symbol string, chartRange ChartRange) (AssetChartSeries, error) {
	if c.chartCache != nil {
		if cached, ok := c.chartCache.Get(symbol, chartRange); ok {
			logChartSeries(symbol, chartRange, cached.RequestedSamples, cached.FailedSamples, len(cached.Points))
			return cached, nil
		}
	}

	now := time.Now().UTC()
	if series, ok := c.seriesFromSource(ctx, symbol, chartRange, now); ok {
		c.cacheChartSeries(symbol, chartRange, series)
		return series, nil
	}

	series := c.seriesFromHermes(ctx, symbol, chartRange, now)
	c.cacheChartSeries(symbol, chartRange, series)
	return series, nil
}

// seriesFromSource asks the one-call history source (Benchmarks) for the range. A
// failure opens a short breaker so a Benchmarks outage does not cost every chart
// load a timeout before it falls back.
func (c *HermesClient) seriesFromSource(ctx context.Context, symbol string, chartRange ChartRange, now time.Time) (AssetChartSeries, bool) {
	if c.seriesSource == nil || !c.seriesBreaker.allows(now) {
		return AssetChartSeries{}, false
	}
	series, err := c.seriesSource.Series(ctx, symbol, chartRange, now)
	if err != nil {
		c.seriesBreaker.trip(now)
		logSeriesSource(symbol, chartRange, err)
		return AssetChartSeries{}, false
	}
	c.seriesBreaker.reset()
	if len(series.Points) == 0 {
		// The source answered and had nothing. Fanning out to Hermes for the same
		// window would only spend 30 requests to learn the same thing.
		series.Range = chartRange
		series.Source = ChartSourceBenchmarks
		series.EmptyReason = EmptyReasonNoHistory
		logChartSeries(symbol, chartRange, series.RequestedSamples, 0, 0)
		return series, true
	}
	logChartSeries(symbol, chartRange, series.RequestedSamples, 0, len(series.Points))
	return series, true
}

// seriesFromHermes is the fallback: one request per sample against the historical
// price endpoint. It is coarse, but it keeps charts alive when Benchmarks is down.
func (c *HermesClient) seriesFromHermes(ctx context.Context, symbol string, chartRange ChartRange, now time.Time) AssetChartSeries {
	feedID, err := c.resolveFeedIDForChart(ctx, symbol)
	if err != nil {
		logChartSeries(symbol, chartRange, 0, 0, 0)
		return AssetChartSeries{EmptyReason: EmptyReasonNoHistory, Range: chartRange}
	}

	samples := chartSampleTimes(chartRange, now)
	points, failed := c.fetchChartPoints(ctx, symbol, feedID, samples)
	series := buildChartSeries(points, len(samples), failed)
	series.Range = chartRange
	series.Source = ChartSourceHermes
	logChartSeries(symbol, chartRange, series.RequestedSamples, series.FailedSamples, len(series.Points))
	return series
}

func (c *HermesClient) cacheChartSeries(symbol string, chartRange ChartRange, series AssetChartSeries) {
	if len(series.Points) >= 2 && c.chartCache != nil {
		c.chartCache.Set(symbol, chartRange, series)
	}
}

func buildChartSeries(points []ChartPoint, requested, failed int) AssetChartSeries {
	if len(points) == 0 {
		return AssetChartSeries{
			EmptyReason:      EmptyReasonNoHistory,
			RequestedSamples: requested,
			FailedSamples:    failed,
		}
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].Timestamp < points[j].Timestamp
	})
	series := AssetChartSeries{
		Points:           points,
		RequestedSamples: requested,
		FailedSamples:    failed,
	}
	// The fallback samples a fixed grid, so the first point is the closest thing it
	// has to a previous close: it is the price at the instant the window opened.
	if len(points) >= 2 {
		previousClose := points[0].PriceUsdcMicros
		series.PreviousCloseUsdcMicros = &previousClose
	}
	return series
}

func (c *HermesClient) resolveFeedIDForChart(ctx context.Context, symbol string) (string, error) {
	if cachedID, ok := lookupFeedID(EquityQuerySymbol(symbol)); ok && cachedID != "" {
		return cachedID, nil
	}
	feedID, _, err := c.resolveFeedSession(ctx, symbol)
	return feedID, err
}

type chartSampleResult struct {
	timestamp int64
	price     int64
}

func (c *HermesClient) fetchChartPoints(ctx context.Context, symbol, feedID string, samples []time.Time) ([]ChartPoint, int) {
	if len(samples) == 0 {
		return nil, 0
	}

	results := make([]chartSampleResult, len(samples))
	sem := make(chan struct{}, chartHistoricalConcurrency)
	var wg sync.WaitGroup
	var failed atomicInt

	for i, ts := range samples {
		wg.Add(1)
		go func(i int, ts time.Time) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			price, err := c.fetchHistoricalPriceWithRetry(ctx, symbol, feedID, ts)
			if err != nil {
				failed.add(1)
				return
			}
			results[i] = chartSampleResult{timestamp: ts.Unix(), price: price}
		}(i, ts)
	}
	wg.Wait()

	points := make([]ChartPoint, 0, len(samples))
	for _, result := range results {
		if result.price <= 0 {
			continue
		}
		points = append(points, ChartPoint{
			Timestamp:       result.timestamp,
			PriceUsdcMicros: result.price,
		})
	}
	return points, failed.load()
}

type atomicInt struct {
	mu sync.Mutex
	n  int
}

func (a *atomicInt) add(delta int) {
	a.mu.Lock()
	a.n += delta
	a.mu.Unlock()
}

func (a *atomicInt) load() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}

// chartSampleTimes is the fallback's sampling grid. Every range is capped at
// roughly 30 samples because each one is a separate Hermes request; the long
// ranges are deliberately coarse rather than expensive.
func chartSampleTimes(chartRange ChartRange, now time.Time) []time.Time {
	switch chartRange {
	case ChartRange1W:
		return dailySamples(now, 7)
	case ChartRange1M:
		return dailySamples(now, 30)
	case ChartRange3M:
		return strideSamples(now, 90, 3)
	case ChartRange1Y:
		return strideSamples(now, 365, 14)
	case ChartRangeAll:
		return strideSamples(now, 365*allRangeYears, 70)
	default:
		return hourlySamples(now, 24)
	}
}

func hourlySamples(now time.Time, hours int) []time.Time {
	out := make([]time.Time, 0, hours)
	start := now.Add(-time.Duration(hours) * time.Hour).Truncate(time.Hour)
	for i := 0; i <= hours; i++ {
		out = append(out, start.Add(time.Duration(i)*time.Hour))
	}
	return out
}

func dailySamples(now time.Time, days int) []time.Time {
	return strideSamples(now, days, 1)
}

// strideSamples walks back days from now and takes a sample every strideDays.
func strideSamples(now time.Time, days, strideDays int) []time.Time {
	if strideDays < 1 {
		strideDays = 1
	}
	start := now.AddDate(0, 0, -days).Truncate(24 * time.Hour)
	out := make([]time.Time, 0, days/strideDays+1)
	for offset := 0; offset <= days; offset += strideDays {
		out = append(out, start.AddDate(0, 0, offset))
	}
	return out
}

func (c *HermesClient) change24h(ctx context.Context, symbol string, currentMicros int64) (string, bool, error) {
	feedID, _, err := c.resolveFeedSession(ctx, symbol)
	if err != nil {
		return "", false, err
	}
	prior, err := c.fetchHistoricalPrice(ctx, feedID, time.Now().UTC().Add(-24*time.Hour))
	if err != nil || prior <= 0 {
		return "", false, err
	}
	delta := float64(currentMicros-prior) / float64(prior)
	return formatDecimalRatio(delta), true, nil
}

func formatDecimalRatio(ratio float64) string {
	return fmt.Sprintf("%.6f", ratio)
}

func (c *HermesClient) fetchHistoricalPrice(ctx context.Context, feedID string, at time.Time) (int64, error) {
	return c.fetchHistoricalPriceWithRetry(ctx, "", feedID, at)
}

func (c *HermesClient) fetchHistoricalPriceWithRetry(ctx context.Context, symbol, feedID string, at time.Time) (int64, error) {
	var lastErr error
	for attempt := 0; attempt < chartHistoricalMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*100) * time.Millisecond
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return 0, ctx.Err()
			case <-timer.C:
			}
		}

		price, status, err := c.fetchHistoricalPriceOnce(ctx, feedID, at)
		if err == nil {
			return price, nil
		}
		lastErr = err
		if !isRetryableHermesStatus(status) {
			break
		}
	}
	if symbol != "" {
		logHistoricalPrice(symbol, feedID, at.Unix(), lastErr)
	}
	return 0, lastErr
}

func isRetryableHermesStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func (c *HermesClient) fetchHistoricalPriceOnce(ctx context.Context, feedID string, at time.Time) (int64, int, error) {
	endpoint := fmt.Sprintf("%s/v2/updates/price/%d?ids[]=%s", c.baseURL, at.Unix(), url.QueryEscape(feedID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, err
	}
	if err := c.setHermesAuth(req); err != nil {
		return 0, 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, resp.StatusCode, hermesRequestError("pyth historical price", resp.StatusCode, body)
	}

	var payload latestPriceResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, resp.StatusCode, err
	}
	if len(payload.Parsed) == 0 {
		return 0, resp.StatusCode, fmt.Errorf("pyth historical price: empty parsed payload")
	}
	price, err := priceToUSDCMicros(payload.Parsed[0].Price.Price, payload.Parsed[0].Price.Expo)
	if err != nil {
		return 0, resp.StatusCode, err
	}
	return price, resp.StatusCode, nil
}

// MidSpreadBps compares a Jupiter implied price to a Pyth mid mark.
func MidSpreadBps(markMicros int64, jupiterOutAmount string, jupiterInMicros int64, outputDecimals int32) *int {
	if markMicros <= 0 || jupiterInMicros <= 0 {
		return nil
	}
	outRaw, err := parseDecimalInt(strings.TrimSpace(jupiterOutAmount))
	if err != nil || outRaw <= 0 {
		return nil
	}
	scale := int64(math.Pow10(int(outputDecimals)))
	if scale <= 0 {
		return nil
	}
	jupiterMicrosPerShare := (jupiterInMicros * scale) / outRaw
	if jupiterMicrosPerShare <= 0 {
		return nil
	}
	spread := float64(jupiterMicrosPerShare-markMicros) / float64(markMicros)
	bps := int(math.Round(spread * 10_000))
	return &bps
}
