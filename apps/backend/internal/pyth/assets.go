package pyth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AssetMark is a standalone mark for catalog pricing.
type AssetMark struct {
	PriceUsdcMicros int64
	Change24h       *string
	// UpdatedAt is when the source struck this price, in UTC: a Chainlink round's
	// updatedAt. Zero when the source does not say, which is never read as "now".
	UpdatedAt time.Time
	// AfterHours is the source's own verdict that the price is older than its feed
	// heartbeat: for Chainlink, the same 25-hour rule NAV marks use.
	AfterHours bool
}

// ChartPoint is one chart sample. The OHLC fields are populated by sources that
// serve candles (Benchmarks) and left at zero by sources that only know a price at
// an instant (the Hermes sampler, Chainlink rounds), so a caller can tell the two
// apart.
type ChartPoint struct {
	Timestamp       int64 `json:"timestamp"`
	PriceUsdcMicros int64 `json:"priceUsdcMicros"`
	OpenUsdcMicros  int64 `json:"openUsdcMicros,omitempty"`
	HighUsdcMicros  int64 `json:"highUsdcMicros,omitempty"`
	LowUsdcMicros   int64 `json:"lowUsdcMicros,omitempty"`
}

// Chart series sources, reported so a caller can tell a dense candle series from a
// sparse fallback.
const (
	ChartSourceBenchmarks = "benchmarks"
	ChartSourceHermes     = "hermes"
	// ChartSourceChainlink is the token's own total-return rounds, the last
	// fallback when Pyth has nothing for the range.
	ChartSourceChainlink = "chainlink"
)

// AssetChartSeries is a time series for Swift Charts.
type AssetChartSeries struct {
	Points      []ChartPoint `json:"points"`
	EmptyReason string       `json:"emptyReason,omitempty"`
	// PreviousCloseUsdcMicros is the previous regular session's close — the
	// baseline a day chart measures its change against. Nil when the source could
	// not reach back far enough to know one; never a point from the series itself.
	PreviousCloseUsdcMicros *int64 `json:"previousCloseUsdcMicros,omitempty"`
	// Range echoes the window this series was built for.
	Range ChartRange `json:"range,omitempty"`
	// Source names which upstream produced the series.
	Source string `json:"source,omitempty"`
	// Basis names which instrument these prices are (PriceBasisUnderlying or
	// PriceBasisToken), and BasisSymbol names it in full ("AAPL", "AAPLc").
	Basis       string `json:"-"`
	BasisSymbol string `json:"-"`
	// RegularOpen and RegularClose bound the regular cash session this window
	// covers, in UTC, and are zero for the ranges that do not have one. The series
	// itself spans the extended session so the chart can draw pre- and post-market;
	// the stats grid folds only between these two.
	RegularOpen  time.Time `json:"-"`
	RegularClose time.Time `json:"-"`
}

// ChartSeriesClient serves chart history for one symbol and range.
type ChartSeriesClient interface {
	ChartSeries(ctx context.Context, symbol string, chartRange ChartRange) (AssetChartSeries, error)
}

// MarketDataClient is Pyth history for the stock screens: whole ranges, and the
// underlying's day change on its own.
type MarketDataClient interface {
	ChartSeriesClient
	// DayChange is the underlying's move against its previous regular-session
	// close, or nil when it cannot be known.
	DayChange(ctx context.Context, symbol string) *string
	// DaySeries is the underlying's 1D Benchmarks series, from the cache or from
	// Benchmarks and never from the Hermes sampler. A list row reads it once and
	// takes both its day change and its sparkline from it, so the two figures on a
	// row are always the same instrument over the same window. The second result
	// is false when Benchmarks could not answer (an outage, the caller's deadline).
	DaySeries(ctx context.Context, symbol string) (AssetChartSeries, bool)
}

// AssetPriceClient fetches standalone marks and chart history.
type AssetPriceClient interface {
	ChartSeriesClient
	AssetMark(ctx context.Context, symbol string) (AssetMark, error)
	AssetMarks(ctx context.Context, symbols []string) (map[string]AssetMark, error)
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
	// No change24h here: the day change is the underlying's move against its
	// previous regular-session close (DayChange), not against a Hermes print taken
	// 24 hours ago, which on a Monday morning is Sunday's frozen Friday price.
	return AssetMark{PriceUsdcMicros: marked.MarkUsdc}, nil
}

func (c *HermesClient) AssetMarks(ctx context.Context, symbols []string) (map[string]AssetMark, error) {
	out := make(map[string]AssetMark, len(symbols))
	for _, symbol := range symbols {
		mark, err := c.AssetMark(ctx, symbol)
		if err != nil || mark.PriceUsdcMicros <= 0 {
			continue
		}
		out[symbol] = mark
	}
	return out, nil
}

// ChartSeries serves one range of the underlying equity's history: from the cache,
// else from the one-call source (Benchmarks), else from the Hermes sampler. Every
// answer is Pyth's equity feed, so every series is labelled with the underlying
// basis. It never returns an error; a range nobody could serve comes back empty
// with EmptyReasonNoHistory.
func (c *HermesClient) ChartSeries(ctx context.Context, symbol string, chartRange ChartRange) (AssetChartSeries, error) {
	if cached, ok := c.charts.get(symbol, chartRange); ok {
		return cached, nil
	}

	now := c.clock()
	if series, ok := c.seriesFromSource(ctx, symbol, chartRange, now); ok {
		c.charts.set(symbol, chartRange, series)
		return series, nil
	}

	series, answered := c.seriesFromHermes(ctx, symbol, chartRange, now)
	if answered {
		c.charts.set(symbol, chartRange, series)
	}
	return series, nil
}

// DayChange reads the 1D series from the cache or from Benchmarks, and never from
// the Hermes sampler: the sampler cannot know a previous close, so a day change
// can only ever come from Benchmarks, and sampling thirteen points per stock for a
// list row that would then show nothing is pure cost.
func (c *HermesClient) DayChange(ctx context.Context, symbol string) *string {
	series, ok := c.DaySeries(ctx, symbol)
	if !ok {
		return nil
	}
	return DayChange(series)
}

// DaySeries is the 1D series DayChange reads, for callers that want the curve as
// well as the change: the cache, else Benchmarks, never the Hermes sampler. A
// Benchmarks answer (including "no bars in this window") is cached like any other
// 1D series, so the chart route, the list's day change and its sparkline share
// one upstream call per symbol per minute.
func (c *HermesClient) DaySeries(ctx context.Context, symbol string) (AssetChartSeries, bool) {
	if cached, ok := c.charts.get(symbol, ChartRange1D); ok {
		return cached, true
	}
	series, ok := c.seriesFromSource(ctx, symbol, ChartRange1D, c.clock())
	if !ok {
		return AssetChartSeries{}, false
	}
	c.charts.set(symbol, ChartRange1D, series)
	return series, true
}

// DaySeriesWarmer refreshes a symbol's cached 1D series ahead of its expiry.
type DaySeriesWarmer interface {
	WarmDaySeries(ctx context.Context, symbol string) bool
}

// WarmDaySeries fetches the 1D series from Benchmarks whether or not the cache
// still holds one, and replaces the cached entry on an answer. A warmer that only
// read through the cache would find a live entry, do nothing, and let it lapse
// between ticks; refreshing is what keeps the list's rows from ever meeting a cold
// cache. It reports whether Benchmarks answered. An outage leaves the cached entry
// alone, so a blip does not blank a series that was fine a minute ago.
func (c *HermesClient) WarmDaySeries(ctx context.Context, symbol string) bool {
	series, ok := c.seriesFromSource(ctx, symbol, ChartRange1D, c.clock())
	if !ok {
		return false
	}
	c.charts.set(symbol, ChartRange1D, series)
	return true
}

// seriesFromSource asks the one-call history source for the range. An outage of
// the source opens a short breaker so it costs one timeout rather than one per
// chart load. The second result is false when the source could not answer at all.
//
// Only an outage trips the breaker. The breaker is process-wide, so tripping it
// on a failure that says nothing about the source would blank the day change on
// every stock for everyone:
//   - the caller's own context ending (a list page's 4 s budget, a detail
//     fan-out's deadline, a client that hung up) is the caller running out of
//     time, not Benchmarks failing;
//   - a per-symbol error status is Benchmarks answering about that symbol, and is
//     returned as that symbol's empty answer.
func (c *HermesClient) seriesFromSource(ctx context.Context, symbol string, chartRange ChartRange, now time.Time) (AssetChartSeries, bool) {
	if c.seriesSource == nil || !c.seriesBreaker.allows(now) {
		return AssetChartSeries{}, false
	}
	series, err := c.seriesSource.Series(ctx, symbol, chartRange, now)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		logSeriesSource(symbol, chartRange, err)
		return AssetChartSeries{}, false
	case IsSymbolHistoryError(err):
		logSeriesSource(symbol, chartRange, err)
		series = AssetChartSeries{}
	default:
		c.seriesBreaker.trip(now)
		logSeriesSource(symbol, chartRange, err)
		return AssetChartSeries{}, false
	}
	if err == nil {
		c.seriesBreaker.reset()
	}
	series.Range = chartRange
	series.Source = ChartSourceBenchmarks
	series.Basis = PriceBasisUnderlying
	series.BasisSymbol = UnderlyingTicker(symbol)
	if len(series.Points) == 0 {
		// The source answered and had nothing. Sampling Hermes for the same window
		// would only spend a request per point to learn the same thing.
		series.Points = nil
		series.EmptyReason = EmptyReasonNoHistory
	}
	return series, true
}

// seriesFromHermes is the fallback: one request per sample against the historical
// price endpoint. It is coarse, but it keeps charts alive when Benchmarks is down.
// The second result says whether Hermes gave an answer worth caching; a missing
// key, a denied entitlement, a run cut short by the caller's deadline or a run
// with any failed sample is not one.
func (c *HermesClient) seriesFromHermes(ctx context.Context, symbol string, chartRange ChartRange, now time.Time) (AssetChartSeries, bool) {
	empty := AssetChartSeries{EmptyReason: EmptyReasonNoHistory, Range: chartRange}
	// Every Hermes request is authenticated. Without a key there is nothing to ask,
	// and building a request per sample only to have each one refused locally
	// would make the no-key deployment look like an outage in the logs.
	if !c.HasAPIKey() || equityFeedsDenied() {
		return empty, false
	}
	feedID, err := c.resolveFeedID(ctx, symbol, EquityQuerySymbol(symbol))
	if err != nil {
		markEquityDenied(err)
		return empty, false
	}

	window := chartWindow(chartRange, now)
	samples := chartSampleTimes(chartRange, now)
	points := make([]ChartPoint, 0, len(samples))
	for _, ts := range samples {
		if ctx.Err() != nil {
			break
		}
		price, err := c.fetchHistoricalPrice(ctx, feedID, ts)
		if err != nil {
			if markEquityDenied(err) {
				break
			}
			continue
		}
		points = append(points, ChartPoint{Timestamp: ts.Unix(), PriceUsdcMicros: price})
	}
	if len(points) == 0 {
		// Nothing came back. That is not a price history, and it is not drawn as one
		// either: no flat line built from the latest price, which would be a chart of
		// a single number presented as a range.
		return empty, false
	}
	if ctx.Err() != nil {
		// The caller's deadline cut the run short. The samples go oldest first, so
		// what came back is the start of the window and none of its end: a "1Y"
		// chart that stops months ago. It is not served, and not cached as the
		// range's answer.
		return empty, false
	}
	// The sampler reads the same equity feed Benchmarks does. It ships no previous
	// close: its first sample is a point on the curve, not a close, and sending it
	// under that name would draw the baseline straight through the first point.
	//
	// A run where some samples failed is still drawn, with gaps, but it is only
	// cached when every sample came back: a partial run is this request's best
	// effort, not the range's answer for the next ten minutes.
	complete := len(points) == len(samples)
	return AssetChartSeries{
		Points:       points,
		Range:        chartRange,
		Source:       ChartSourceHermes,
		Basis:        PriceBasisUnderlying,
		BasisSymbol:  UnderlyingTicker(symbol),
		RegularOpen:  window.regularOpen,
		RegularClose: window.regularClose,
	}, complete
}

// chartSampleTimes is the sampler's grid. Every range is capped at roughly 30
// samples because each one is a separate Hermes request; the long ranges are
// deliberately coarse rather than expensive.
func chartSampleTimes(chartRange ChartRange, now time.Time) []time.Time {
	switch chartRange {
	case ChartRange1W:
		return strideSamples(now, 7, 1)
	case ChartRange1M:
		return strideSamples(now, 30, 1)
	case ChartRange3M:
		return strideSamples(now, 90, 3)
	case ChartRange1Y:
		return strideSamples(now, 365, 14)
	case ChartRangeAll:
		return strideSamples(now, 365*allRangeYears, 70)
	default:
		return hourlySamples(now, 12)
	}
}

func hourlySamples(now time.Time, hours int) []time.Time {
	out := make([]time.Time, 0, hours+1)
	step := time.Hour
	if hours <= 12 {
		step = 2 * time.Hour
	}
	span := time.Duration(hours) * step
	start := now.Add(-span).Truncate(step)
	for t := start; !t.After(now); t = t.Add(step) {
		out = append(out, t)
	}
	return out
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

// DayChange is the underlying's move today as a decimal ratio string ("0.012345"
// for +1.23%): the latest price in the 1D series against the previous regular
// session's close. It is nil unless the series is Pyth's underlying equity and
// carries a genuine previous close, because a ratio across two instruments —
// a token mark over an equity close — would fold the token's multiplier into a
// "move" that never happened.
func DayChange(day AssetChartSeries) *string {
	if day.Basis != PriceBasisUnderlying || day.PreviousCloseUsdcMicros == nil || len(day.Points) == 0 {
		return nil
	}
	previous := *day.PreviousCloseUsdcMicros
	latest := day.Points[len(day.Points)-1].PriceUsdcMicros
	if previous <= 0 || latest <= 0 {
		return nil
	}
	change := formatDecimalRatio(float64(latest-previous) / float64(previous))
	return &change
}

func formatDecimalRatio(ratio float64) string {
	return fmt.Sprintf("%.6f", ratio)
}

func (c *HermesClient) fetchHistoricalPrice(ctx context.Context, feedID string, at time.Time) (int64, error) {
	endpoint := c.baseURL + hermesPricePath("historical", feedID, at.Unix())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	if err := c.setHermesAuth(req); err != nil {
		return 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		histErr := hermesRequestError("pyth historical price", resp.StatusCode, body)
		markEquityDenied(histErr)
		return 0, histErr
	}

	var payload latestPriceResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, err
	}
	if len(payload.Parsed) == 0 {
		return 0, fmt.Errorf("pyth historical price: empty parsed payload")
	}
	return priceToUSDCMicros(payload.Parsed[0].Price.Price, payload.Parsed[0].Price.Expo)
}
