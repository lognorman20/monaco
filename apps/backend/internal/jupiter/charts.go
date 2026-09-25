package jupiter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/logsnippet"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

const (
	defaultChartsBaseURL = "https://datapi.jup.ag/v2/charts"
	defaultChartsTimeout = 12 * time.Second
	// maxCandlesPerRequest bounds one request. Jupiter returns everything it has
	// when asked for more than exists, so this is a ceiling on the response size,
	// not a promise about how much history a mint actually has.
	maxCandlesPerRequest = 1200
	// chartsUserAgent identifies this service to Jupiter.
	//
	// It is not optional: datapi.jup.ag sits behind Cloudflare, which answers Go's
	// default `Go-http-client/2.0` with a 403 challenge page because that string is
	// the signature of every unattended scraper. Any honest name is accepted — this
	// one says who is calling, which is what the header is for.
	chartsUserAgent = "monaco-backend/1.0 (+https://monacolabs.xyz)"
)

// MintResolver turns a catalog symbol into the Solana mint Jupiter prices.
//
// Declared here rather than taken from the xstocks package so this client depends
// on the one method it uses; `*xstocks.HTTPResolver` satisfies it as it stands.
type MintResolver interface {
	ResolveSolanaMint(ctx context.Context, symbol string) (string, error)
}

// ChartsClient reads candle history for an xStock from Jupiter.
//
// It exists because every Pyth history source prices the *underlying equity*, and
// reaching an equity feed needs an entitlement our key does not carry — Hermes and
// Benchmarks both refuse `Equity.US.AAPL/USD` and `Crypto.AAPLX/USD` outright, so
// the chart was empty no matter which of them answered. Jupiter has no such gate,
// and it prices the thing this app actually lets a cabal buy: the token on Solana,
// at the venue the swap will route through. A chart drawn from it is the instrument
// in the trade, not a proxy for it.
type ChartsClient struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
	mints      MintResolver
}

// NewChartsClient returns a production charts client. apiKey may be empty — the
// endpoint serves unauthenticated callers at a lower rate limit.
func NewChartsClient(mints MintResolver, apiKey string) *ChartsClient {
	return NewChartsClientWithBaseURL(defaultChartsBaseURL, nil, mints, apiKey)
}

// NewChartsClientWithBaseURL injects a base URL and HTTP client, for tests.
func NewChartsClientWithBaseURL(baseURL string, httpClient *http.Client, mints MintResolver, apiKey string) *ChartsClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultChartsBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultChartsTimeout}
	}
	return &ChartsClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		// Wrapped like every other Jupiter client: a test that reaches the live API
		// by accident fails loudly rather than depending on the network.
		httpClient: wrapHTTPClientForTests(telemetry.InstrumentClient(telemetry.UpstreamJupiter, httpClient)),
		apiKey:     strings.TrimSpace(apiKey),
		mints:      mints,
	}
}

// Series implements pyth.SeriesSource.
func (c *ChartsClient) Series(ctx context.Context, symbol string, chartRange pyth.ChartRange, now time.Time) (pyth.AssetChartSeries, error) {
	if c == nil || c.mints == nil {
		return pyth.AssetChartSeries{}, fmt.Errorf("jupiter charts: no mint resolver")
	}
	mint, err := c.mints.ResolveSolanaMint(ctx, symbol)
	if err != nil {
		return pyth.AssetChartSeries{}, fmt.Errorf("jupiter charts %s: resolve mint: %w", symbol, err)
	}

	from, to := pyth.RangeFetchWindow(chartRange, now)
	interval := chartInterval(chartRange)
	bars, err := c.candles(ctx, mint, interval, candleCount(from, to, interval), to)
	if err != nil {
		return pyth.AssetChartSeries{}, err
	}

	series := pyth.AssembleRangeSeries(chartRange, now, bars)
	series.Range = chartRange
	series.Source = pyth.ChartSourceJupiter
	// These candles are the xStock on Solana, so unlike every Pyth source the series
	// and the hero price above it are finally the same instrument.
	series.Basis = pyth.PriceBasisToken
	series.BasisSymbol = symbol
	return series, nil
}

// chartInterval is the candle size for a range: fine enough that the curve has
// shape, coarse enough that one request covers the whole window.
func chartInterval(chartRange pyth.ChartRange) string {
	switch chartRange {
	case pyth.ChartRange1W:
		return "30_MINUTE"
	case pyth.ChartRange1M:
		return "1_HOUR"
	case pyth.ChartRange3M, pyth.ChartRange1Y, pyth.ChartRangeAll:
		return "1_DAY"
	default:
		return "5_MINUTE"
	}
}

// intervalSeconds is how long one candle of each interval covers.
var intervalSeconds = map[string]int64{
	"5_MINUTE":  300,
	"30_MINUTE": 1800,
	"1_HOUR":    3600,
	"1_DAY":     86400,
}

// candleCount asks for enough candles to cover the fetch window and no more.
// Jupiter counts backwards from `to`, so asking short would silently truncate the
// oldest end of the range — including the bar the previous close comes from.
func candleCount(from, to time.Time, interval string) int {
	seconds, ok := intervalSeconds[interval]
	if !ok || seconds <= 0 {
		return maxCandlesPerRequest
	}
	span := to.Sub(from).Seconds()
	if span <= 0 {
		return 1
	}
	// One spare candle so a window that starts mid-candle still reaches past its
	// own beginning.
	count := int(math.Ceil(span/float64(seconds))) + 1
	if count > maxCandlesPerRequest {
		return maxCandlesPerRequest
	}
	return count
}

type chartsResponse struct {
	Candles []chartsCandle `json:"candles"`
}

type chartsCandle struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

func (c *ChartsClient) candles(ctx context.Context, mint, interval string, candles int, to time.Time) ([]pyth.OHLCBar, error) {
	endpoint := fmt.Sprintf(
		"%s/%s?interval=%s&candles=%d&type=price&to=%s",
		c.baseURL,
		url.PathEscape(mint),
		url.QueryEscape(interval),
		candles,
		url.QueryEscape(to.UTC().Format(time.RFC3339)),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", chartsUserAgent)
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jupiter charts %s: %w", mint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("jupiter charts %s: %w", mint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jupiter charts %s: status %d: %s", mint, resp.StatusCode, logsnippet.Body(body))
	}

	var payload chartsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode jupiter charts %s: %w", mint, err)
	}
	return decodeCandles(payload.Candles), nil
}

// decodeCandles drops any candle whose close is missing or unusable rather than
// letting a partial payload become a chart with a zero-dollar spike in it. A mint
// with no history at all yields no bars, which the caller reports as an empty
// series — an answer, not a failure, so it must not look like an outage.
func decodeCandles(candles []chartsCandle) []pyth.OHLCBar {
	if len(candles) == 0 {
		return nil
	}
	bars := make([]pyth.OHLCBar, 0, len(candles))
	for _, candle := range candles {
		closeMicros, ok := usdToMicros(candle.Close)
		if !ok || candle.Time <= 0 {
			continue
		}
		bars = append(bars, pyth.OHLCBar{
			Timestamp: candle.Time,
			Open:      microsOr(candle.Open, closeMicros),
			High:      microsOr(candle.High, closeMicros),
			Low:       microsOr(candle.Low, closeMicros),
			Close:     closeMicros,
		})
	}
	return bars
}

func microsOr(value float64, fallback int64) int64 {
	micros, ok := usdToMicros(value)
	if !ok {
		return fallback
	}
	return micros
}

func usdToMicros(value float64) (int64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > maxUsdPrice {
		return 0, false
	}
	micros := math.Round(value * 1_000_000)
	if micros <= 0 || micros > float64(math.MaxInt64) {
		return 0, false
	}
	return int64(micros), true
}
