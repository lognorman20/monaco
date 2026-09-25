// Package yahoocharts serves a stock's price history from Yahoo Finance's public chart
// endpoint, which takes no key and costs nothing.
//
// It is a stand-in. Every paid history source for tokenized stocks either wants an
// equity entitlement (Pyth) or draws the thin on-chain history of the token itself
// (Jupiter). For the demo, and until history is worth paying for, the curve under a
// stock is the underlying equity on its home exchange — which is also the number
// every stats grid on the screen already describes. The series says so: its basis is
// the underlying, and the hero price above it stays the token's.
package yahoocharts

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

	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

const (
	// DefaultBaseURL is Yahoo's chart API. It answers plain GETs with a browser-ish
	// user agent; the default Go one is refused.
	DefaultBaseURL = "https://query1.finance.yahoo.com"
	userAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15"
	maxUsdPrice    = 1e9
	requestTimeout = 12 * time.Second
)

// Client fetches one chart range in one call.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a client against Yahoo's public endpoint.
func New() *Client {
	return NewWithBaseURL(DefaultBaseURL, nil)
}

// NewWithBaseURL is for tests and operators fronting the endpoint themselves.
func NewWithBaseURL(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// Series implements pyth.SeriesSource for the equity behind an xStock symbol.
func (c *Client) Series(ctx context.Context, symbol string, chartRange pyth.ChartRange, now time.Time) (pyth.AssetChartSeries, error) {
	ticker := UnderlyingTicker(symbol)
	if ticker == "" {
		return pyth.AssetChartSeries{}, fmt.Errorf("yahoo charts: no equity ticker for %q", symbol)
	}
	yRange, interval := params(chartRange)
	endpoint := fmt.Sprintf("%s/v8/finance/chart/%s?range=%s&interval=%s&includePrePost=true&events=div%%2Csplit",
		c.baseURL, url.PathEscape(ticker), yRange, interval)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return pyth.AssetChartSeries{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return pyth.AssetChartSeries{}, fmt.Errorf("yahoo charts %s: %w", ticker, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return pyth.AssetChartSeries{}, fmt.Errorf("yahoo charts %s: read: %w", ticker, err)
	}
	if resp.StatusCode != http.StatusOK {
		return pyth.AssetChartSeries{}, fmt.Errorf("yahoo charts %s: status %d", ticker, resp.StatusCode)
	}
	bars, err := DecodeBars(body)
	if err != nil {
		return pyth.AssetChartSeries{}, fmt.Errorf("yahoo charts %s: %w", ticker, err)
	}
	series := pyth.AssembleRangeSeries(chartRange, now, bars)
	series.Range = chartRange
	series.Source = pyth.ChartSourceYahoo
	// This is Apple on NASDAQ, not AAPLx on Solana: the screen labels it as such.
	series.Basis = pyth.PriceBasisUnderlying
	series.BasisSymbol = ticker
	return series, nil
}

// HasKeylessHistory marks the source as one that needs no Pyth entitlement.
func (c *Client) HasKeylessHistory() bool { return c != nil }

// UnderlyingTicker maps an xStock symbol to the equity's ticker as Yahoo spells it:
// AAPLx is AAPL, BRK.Bx is BRK-B. Only the xStock form maps, an upper-case ticker
// with a lower-case x on the end. It used to pass anything else through upper-cased
// so a bare AAPL would work, which also sent every pre-IPO token in the catalog
// (tKalshi, tSpaceX, ANDURIL) to Yahoo as a ticker it has never heard of, one 404
// per chart range. Empty when there is nothing to chart.
func UnderlyingTicker(symbol string) string {
	s := strings.TrimSpace(symbol)
	if len(s) < 2 || !strings.HasSuffix(s, "x") {
		return ""
	}
	base := strings.TrimSuffix(s, "x")
	for _, r := range base {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.') {
			return ""
		}
	}
	return strings.ReplaceAll(base, ".", "-")
}

// params picks Yahoo's range and interval for one of the app's chart ranges: fine
// enough that the curve has shape, coarse enough that one response covers it.
func params(chartRange pyth.ChartRange) (yRange, interval string) {
	switch chartRange {
	case pyth.ChartRange1W:
		return "5d", "30m"
	case pyth.ChartRange1M:
		return "1mo", "1d"
	case pyth.ChartRange3M:
		return "3mo", "1d"
	case pyth.ChartRange1Y:
		return "1y", "1d"
	case pyth.ChartRangeAll:
		return "max", "1wk"
	default:
		// Two days of five-minute bars, so the previous close is in the payload.
		return "2d", "5m"
	}
}

type chartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open  []*float64 `json:"open"`
					High  []*float64 `json:"high"`
					Low   []*float64 `json:"low"`
					Close []*float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// DecodeBars turns Yahoo's columnar chart payload into bars. A bar with no close
// (a halted minute, a holiday) is skipped; a missing open, high or low falls back to
// the close so the bar still counts.
func DecodeBars(body []byte) ([]pyth.OHLCBar, error) {
	var payload chartResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if payload.Chart.Error != nil {
		return nil, fmt.Errorf("%s: %s", payload.Chart.Error.Code, payload.Chart.Error.Description)
	}
	if len(payload.Chart.Result) == 0 || len(payload.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, fmt.Errorf("no series in response")
	}
	result := payload.Chart.Result[0]
	quote := result.Indicators.Quote[0]
	bars := make([]pyth.OHLCBar, 0, len(result.Timestamp))
	for i, ts := range result.Timestamp {
		closeMicros, ok := usdToMicros(at(quote.Close, i))
		if !ok {
			continue
		}
		bars = append(bars, pyth.OHLCBar{
			Timestamp: ts,
			Open:      microsOr(at(quote.Open, i), closeMicros),
			High:      microsOr(at(quote.High, i), closeMicros),
			Low:       microsOr(at(quote.Low, i), closeMicros),
			Close:     closeMicros,
		})
	}
	return bars, nil
}

func at(values []*float64, i int) float64 {
	if i >= len(values) || values[i] == nil {
		return math.NaN()
	}
	return *values[i]
}

func microsOr(value float64, fallback int64) int64 {
	if micros, ok := usdToMicros(value); ok {
		return micros
	}
	return fallback
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
