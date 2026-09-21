package pyth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
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

// SymbolHistoryError is a history source answering, for one symbol, that it has
// no series to give: Benchmarks' `"s":"error"` status on a 200. The source is up;
// the symbol is the problem. Callers must not treat it as an outage.
type SymbolHistoryError struct {
	Symbol string
	Detail string
}

func (e *SymbolHistoryError) Error() string {
	return fmt.Sprintf("pyth benchmarks history %s: status %q", e.Symbol, e.Detail)
}

// IsSymbolHistoryError reports whether err is an answer about one symbol rather
// than a failure of the source.
func IsSymbolHistoryError(err error) bool {
	var symbolErr *SymbolHistoryError
	return errors.As(err, &symbolErr)
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
		httpClient: httpClient,
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
	// Apple on NASDAQ — not the AAPLc token on Base. Record it rather than let a
	// caller assume the series is about the thing the user can actually buy.
	series.Basis = PriceBasisUnderlying
	series.BasisSymbol = UnderlyingTicker(symbol)
	return series, nil
}

// ohlcBar is one Benchmarks candle, already converted to USDC micros.
type ohlcBar struct {
	timestamp int64
	open      int64
	high      int64
	low       int64
	close     int64
}

func (c *BenchmarksClient) history(ctx context.Context, querySymbol, resolution string, from, to time.Time) ([]ohlcBar, error) {
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
		// Benchmarks answered 200 with an error about this one symbol (an unknown
		// ticker, a feed it does not serve). The service itself is up, so this is
		// an answer about the symbol, not an outage, and it must not be reported as
		// one.
		detail := strings.TrimSpace(payload.ErrMsg)
		if detail == "" {
			detail = strings.TrimSpace(payload.Status)
		}
		return nil, &SymbolHistoryError{Symbol: querySymbol, Detail: detail}
	}

	return decodeBars(payload)
}

// decodeBars drops any bar whose price is missing or unusable rather than letting a
// partial payload become a chart with a zero-dollar spike in it.
func decodeBars(payload benchmarksHistoryResponse) ([]ohlcBar, error) {
	count := len(payload.Timestamp)
	if count == 0 {
		return nil, nil
	}
	if len(payload.Close) != count {
		return nil, fmt.Errorf("pyth benchmarks history: %d timestamps but %d closes", count, len(payload.Close))
	}

	bars := make([]ohlcBar, 0, count)
	for i := 0; i < count; i++ {
		closeMicros, ok := usdToMicros(payload.Close[i])
		if !ok || payload.Timestamp[i] <= 0 {
			continue
		}
		bar := ohlcBar{timestamp: payload.Timestamp[i], close: closeMicros}
		bar.open = optionalMicros(payload.Open, i, closeMicros)
		bar.high = optionalMicros(payload.High, i, closeMicros)
		bar.low = optionalMicros(payload.Low, i, closeMicros)
		bars = append(bars, bar)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].timestamp < bars[j].timestamp })
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
func assembleSeries(bars []ohlcBar, window chartRangeWindow) AssetChartSeries {
	if len(bars) == 0 {
		return AssetChartSeries{EmptyReason: EmptyReasonNoHistory}
	}

	fromUnix := window.from.Unix()
	var toUnix int64 = math.MaxInt64
	if !window.to.IsZero() {
		toUnix = window.to.Unix()
	}
	// The previous close is the close of the last bar that opens strictly before a
	// cut-off. Without an explicit instant the cut-off is the window's first
	// instant. 1D sets one: the previous regular session's closing bell.
	//
	// Strictly before, because the TradingView shim stamps a bar with its open
	// time. The bar stamped 16:00 ET is 16:00-16:05, the first post-market bar, and
	// its close is an after-hours print; the regular session's close is the close
	// of the bar stamped 15:55. On a half day the same holds at 13:00. This matches
	// regularSessionPoints, which also counts the bar at the bell as after-hours.
	previousCloseCutoff := fromUnix
	if !window.previousCloseAt.IsZero() {
		previousCloseCutoff = window.previousCloseAt.Unix()
	}

	inWindow := make([]ohlcBar, 0, len(bars))
	var previousClose int64
	for _, bar := range bars {
		if bar.timestamp < previousCloseCutoff {
			previousClose = bar.close
		}
		if bar.timestamp >= fromUnix && bar.timestamp <= toUnix {
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
			Timestamp:       bar.timestamp,
			PriceUsdcMicros: bar.close,
			OpenUsdcMicros:  bar.open,
			HighUsdcMicros:  bar.high,
			LowUsdcMicros:   bar.low,
		})
	}

	series := AssetChartSeries{
		Points:       points,
		RegularOpen:  window.regularOpen,
		RegularClose: window.regularClose,
	}
	if previousClose > 0 {
		series.PreviousCloseUsdcMicros = &previousClose
	}
	return series
}

// downsample keeps the first and last bar and spreads the rest evenly, so the
// endpoints a chart labels stay exact even when the middle is thinned.
func downsample(bars []ohlcBar, limit int) []ohlcBar {
	if limit <= 2 || len(bars) <= limit {
		return bars
	}
	out := make([]ohlcBar, 0, limit)
	step := float64(len(bars)-1) / float64(limit-1)
	for i := 0; i < limit; i++ {
		index := int(math.Round(float64(i) * step))
		if index >= len(bars) {
			index = len(bars) - 1
		}
		if len(out) > 0 && out[len(out)-1].timestamp == bars[index].timestamp {
			continue
		}
		out = append(out, bars[index])
	}
	return out
}
