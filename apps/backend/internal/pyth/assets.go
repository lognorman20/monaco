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

// ChartRange is a supported asset chart window.
type ChartRange string

const (
	ChartRange1D ChartRange = "1D"
	ChartRange1W ChartRange = "1W"
	ChartRange1M ChartRange = "1M"
)

// AssetMark is a standalone equity mark for catalog pricing.
type AssetMark struct {
	PriceUsdcMicros int64
	Change24h       *string
}

// ChartPoint is one chart sample.
type ChartPoint struct {
	Timestamp       int64 `json:"timestamp"`
	PriceUsdcMicros int64 `json:"priceUsdcMicros"`
}

// AssetChartSeries is a time series for Swift Charts.
type AssetChartSeries struct {
	Points      []ChartPoint `json:"points"`
	EmptyReason string       `json:"emptyReason,omitempty"`
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

	feedID, err := c.resolveFeedIDForChart(ctx, symbol)
	if err != nil {
		logChartSeries(symbol, chartRange, 0, 0, 0)
		return AssetChartSeries{EmptyReason: "price history unavailable"}, nil
	}

	samples := chartSampleTimes(chartRange, time.Now().UTC())
	points, failed := c.fetchChartPoints(ctx, symbol, feedID, samples)
	series := buildChartSeries(points, len(samples), failed)
	logChartSeries(symbol, chartRange, series.RequestedSamples, series.FailedSamples, len(series.Points))

	if len(series.Points) >= 2 && c.chartCache != nil {
		c.chartCache.Set(symbol, chartRange, series)
	}
	return series, nil
}

func buildChartSeries(points []ChartPoint, requested, failed int) AssetChartSeries {
	if len(points) == 0 {
		return AssetChartSeries{
			EmptyReason:      "price history unavailable",
			RequestedSamples: requested,
			FailedSamples:    failed,
		}
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].Timestamp < points[j].Timestamp
	})
	return AssetChartSeries{
		Points:           points,
		RequestedSamples: requested,
		FailedSamples:    failed,
	}
}

func (c *HermesClient) resolveFeedIDForChart(ctx context.Context, symbol string) (string, error) {
	if cachedID, ok := lookupFeedID(symbol); ok && cachedID != "" {
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

func chartSampleTimes(chartRange ChartRange, now time.Time) []time.Time {
	switch chartRange {
	case ChartRange1W:
		return dailySamples(now, 7)
	case ChartRange1M:
		return dailySamples(now, 30)
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
	out := make([]time.Time, 0, days)
	start := now.AddDate(0, 0, -days).Truncate(24 * time.Hour)
	for i := 0; i <= days; i++ {
		out = append(out, start.AddDate(0, 0, i))
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

// ParseChartRange validates a chart range query param.
func ParseChartRange(raw string) (ChartRange, error) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(ChartRange1D), "":
		return ChartRange1D, nil
	case string(ChartRange1W):
		return ChartRange1W, nil
	case string(ChartRange1M):
		return ChartRange1M, nil
	default:
		return "", fmt.Errorf("invalid chart range %q", raw)
	}
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
