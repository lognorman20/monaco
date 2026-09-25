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
	"time"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// Pyth Benchmarks serves a whole series in one call, as OHLC bars, through a
// TradingView-compatible shim. Hermes can only answer "what was the price at this
// exact second", so a chart there costs one request per point — 25 hourly samples
// for a day, and nothing dense enough to look like a real chart. Benchmarks is the
// right source for history; Hermes stays the source for the latest mark.
const (
	defaultBenchmarksBaseURL = "https://benchmarks.pyth.network"
	defaultBenchmarksTimeout = 12 * time.Second
	// maxChartPoints bounds a response so one asset screen cannot ship a megabyte
	// of JSON to a phone. Every range fits under this today; the guard is for the
	// day someone widens a resolution.
	maxChartPoints = 500
)

// SeriesSource fetches a whole chart range in one upstream call.
type SeriesSource interface {
	Series(ctx context.Context, symbol string, chartRange ChartRange, now time.Time) (AssetChartSeries, error)
}

// KeylessHistorySource is implemented by chart clients that can serve history
// without a Hermes feed entitlement. Callers that gate charts on entitlement —
// because the Hermes sampler would otherwise fire thirty requests at a feed that
// is going to refuse all of them — must skip that gate for these, or a crypto-only
// API key would lose charts it can perfectly well serve.
type KeylessHistorySource interface {
	HasKeylessHistory() bool
}

// HasKeylessHistory reports whether Benchmarks is wired up. Benchmarks is a public
// endpoint and takes no key, so a Hermes entitlement says nothing about it.
func (c *HermesClient) HasKeylessHistory() bool {
	return c != nil && c.seriesSource != nil && c.seriesBreaker.allows(time.Now())
}

// BenchmarksClient reads OHLC history from the Pyth Benchmarks TradingView shim.
type BenchmarksClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewBenchmarksClient returns a Benchmarks client against the public host.
func NewBenchmarksClient() *BenchmarksClient {
	return NewBenchmarksClientWithHTTP(defaultBenchmarksBaseURL, nil)
}

// NewBenchmarksClientWithHTTP injects a base URL and HTTP client, for tests and
// for a self-hosted Benchmarks deployment.
func NewBenchmarksClientWithHTTP(baseURL string, httpClient *http.Client) *BenchmarksClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultBenchmarksBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultBenchmarksTimeout}
	}
	return &BenchmarksClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: telemetry.InstrumentClient(telemetry.UpstreamPyth, httpClient),
	}
}

type benchmarksHistoryResponse struct {
	Status    string    `json:"s"`
	ErrMsg    string    `json:"errmsg"`
	Timestamp []int64   `json:"t"`
	Open      []float64 `json:"o"`
	High      []float64 `json:"h"`
	Low       []float64 `json:"l"`
	Close     []float64 `json:"c"`
}

// Series fetches one range. It asks for a little more history than the window so
// the bar immediately before the window can supply the previous close the chart
// draws its baseline from.
func (c *BenchmarksClient) Series(ctx context.Context, symbol string, chartRange ChartRange, now time.Time) (AssetChartSeries, error) {
	window := chartWindow(chartRange, now.UTC())
	bars, err := c.history(ctx, EquityQuerySymbol(symbol), window.resolution, window.fetchFrom, window.to)
	if err != nil {
		return AssetChartSeries{}, err
	}
	series := assembleSeries(bars, window)
	series.Range = chartRange
	series.Source = ChartSourceBenchmarks
	// Benchmarks is queried for the underlying equity feed, so these candles are
	// Apple on NASDAQ — not AAPLx on Solana. Record it rather than let a caller
	// assume the series is about the thing the user can actually buy.
	series.Basis = PriceBasisUnderlying
	series.BasisSymbol = UnderlyingTicker(symbol)
	return series, nil
}

func (c *BenchmarksClient) history(ctx context.Context, querySymbol, resolution string, from, to time.Time) ([]OHLCBar, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/shims/tradingview/history?symbol=%s&resolution=%s&from=%d&to=%d",
		c.baseURL, url.QueryEscape(querySymbol), url.QueryEscape(resolution), from.Unix(), to.Unix(),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pyth benchmarks history %s: %w", querySymbol, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("pyth benchmarks history %s: %w", querySymbol, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, hermesRequestError(fmt.Sprintf("pyth benchmarks history %s", querySymbol), resp.StatusCode, body)
	}

	var payload benchmarksHistoryResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode pyth benchmarks history %s: %w", querySymbol, err)
	}
	switch strings.ToLower(strings.TrimSpace(payload.Status)) {
	case "ok":
	case "no_data":
		// The feed exists but has nothing in this window. That is an answer, not a
		// failure, so it must not trip the fallback into 30 Hermes calls that would
		// also return nothing.
		return nil, nil
	default:
		detail := strings.TrimSpace(payload.ErrMsg)
		if detail == "" {
			detail = strings.TrimSpace(payload.Status)
		}
		return nil, fmt.Errorf("pyth benchmarks history %s: status %q", querySymbol, detail)
	}

	return decodeBars(payload)
}

// decodeBars drops any bar whose price is missing or unusable rather than letting a
// partial payload become a chart with a zero-dollar spike in it.
func decodeBars(payload benchmarksHistoryResponse) ([]OHLCBar, error) {
	count := len(payload.Timestamp)
	if count == 0 {
		return nil, nil
	}
	if len(payload.Close) != count {
		return nil, fmt.Errorf("pyth benchmarks history: %d timestamps but %d closes", count, len(payload.Close))
	}

	bars := make([]OHLCBar, 0, count)
	for i := 0; i < count; i++ {
		closeMicros, ok := usdToMicros(payload.Close[i])
		if !ok || payload.Timestamp[i] <= 0 {
			continue
		}
		bar := OHLCBar{Timestamp: payload.Timestamp[i], Close: closeMicros}
		bar.Open = optionalMicros(payload.Open, i, closeMicros)
		bar.High = optionalMicros(payload.High, i, closeMicros)
		bar.Low = optionalMicros(payload.Low, i, closeMicros)
		bars = append(bars, bar)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Timestamp < bars[j].Timestamp })
	return bars, nil
}

func optionalMicros(values []float64, index int, fallback int64) int64 {
	if index >= len(values) {
		return fallback
	}
	micros, ok := usdToMicros(values[index])
	if !ok {
		return fallback
	}
	return micros
}

func usdToMicros(value float64) (int64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, false
	}
	micros := math.Round(value * 1_000_000)
	if micros <= 0 || micros > float64(math.MaxInt64) {
		return 0, false
	}
	return int64(micros), true
}

// assembleSeries splits the fetched bars into the ones inside the window and the
// one that supplies the previous close. Both boundaries come from the window — that
// is, from the exchange calendar — never from the bars, which keep arriving after
// the bell whether or not a session is running.
func assembleSeries(bars []OHLCBar, window chartRangeWindow) AssetChartSeries {
	if len(bars) == 0 {
		return AssetChartSeries{EmptyReason: EmptyReasonNoHistory}
	}

	fromUnix := window.from.Unix()
	var toUnix int64 = math.MaxInt64
	if !window.to.IsZero() {
		toUnix = window.to.Unix()
	}
	// Without an explicit instant, the previous close is the last bar before the
	// window opens. 1D sets one: the previous regular session's close.
	previousCloseUnix := fromUnix - 1
	if !window.previousCloseAt.IsZero() {
		previousCloseUnix = window.previousCloseAt.Unix()
	}

	inWindow := make([]OHLCBar, 0, len(bars))
	var previousClose int64
	for _, bar := range bars {
		if bar.Timestamp <= previousCloseUnix {
			previousClose = bar.Close
		}
		if bar.Timestamp >= fromUnix && bar.Timestamp <= toUnix {
			inWindow = append(inWindow, bar)
		}
	}
	if len(inWindow) == 0 {
		return AssetChartSeries{EmptyReason: EmptyReasonNoHistory}
	}

	inWindow = downsample(inWindow, maxChartPoints)
	points := make([]ChartPoint, 0, len(inWindow))
	for _, bar := range inWindow {
		points = append(points, ChartPoint{
			Timestamp:       bar.Timestamp,
			PriceUsdcMicros: bar.Close,
			OpenUsdcMicros:  bar.Open,
			HighUsdcMicros:  bar.High,
			LowUsdcMicros:   bar.Low,
		})
	}

	series := AssetChartSeries{
		Points:           points,
		RequestedSamples: len(inWindow),
		RegularOpen:      window.regularOpen,
		RegularClose:     window.regularClose,
	}
	if previousClose > 0 {
		series.PreviousCloseUsdcMicros = &previousClose
	}
	return series
}

// downsample keeps the first and last bar and spreads the rest evenly, so the
// endpoints a chart labels stay exact even when the middle is thinned.
func downsample(bars []OHLCBar, limit int) []OHLCBar {
	if limit <= 2 || len(bars) <= limit {
		return bars
	}
	out := make([]OHLCBar, 0, limit)
	step := float64(len(bars)-1) / float64(limit-1)
	for i := 0; i < limit; i++ {
		index := int(math.Round(float64(i) * step))
		if index >= len(bars) {
			index = len(bars) - 1
		}
		if len(out) > 0 && out[len(out)-1].Timestamp == bars[index].Timestamp {
			continue
		}
		out = append(out, bars[index])
	}
	return out
}
