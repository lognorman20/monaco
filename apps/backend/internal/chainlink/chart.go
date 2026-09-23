package chainlink

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/marketcal"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

// The chart source of last resort, and on Base today the only one that works:
// the aggregator's own round history.
//
// Pyth serves the underlying equity and is preferred wherever it answers, but the
// Benchmarks TradingView shim the chart path called has been withdrawn (404 on
// /v1/shims/tradingview/*), and a crypto-only PYTH_API_KEY is refused for every
// equity feed (403 on Hermes updates). The Chainlink feed behind NAV marks needs
// no key and holds every round it has ever published, so it can draw a real curve.
//
// What it is NOT is the equity: a round prices the tokenised instrument, per
// token, on a total-return feed that reinvests dividends. Every series from here
// says so (Source chainlink, Basis token, BasisSymbol "AAPLc") and no caller may
// relabel it.

// chartMaxRounds caps one chart read. A B20 feed publishes on deviation and on a
// daily heartbeat — a few hundred rounds a quarter — so this is far above a real
// ALL window and exists only so a misbehaving feed cannot walk forever.
const chartMaxRounds = 3000

// chartSeriesTTL values mirror the Pyth chart cache: a day series feeds the day
// change on every list row and must stay current, a long range cannot visibly
// move inside ten minutes.
const (
	chartSeriesDayTTL   = time.Minute
	chartSeriesLongTTL  = 10 * time.Minute
	chartSeriesEmptyTTL = 2 * time.Minute
)

// chartWindow is one range's bounds, all UTC.
type chartWindow struct {
	// from and to bound what the chart draws.
	from, to time.Time
	// fetchFrom reaches further back than from so the round before the window can
	// supply a previous close. Zero means "the whole feed", which is what ALL
	// wants.
	fetchFrom time.Time
	// regularOpen and regularClose bound the regular cash session, for the range
	// that has one (1D). Zero otherwise.
	regularOpen, regularClose time.Time
	// previousCloseFrom and previousCloseAt bracket the rounds that may carry the
	// previous close. A close is only a previous close if it is from the session
	// before this window; anything older is a price from days ago, and change24h
	// measured against it is not a day move.
	previousCloseFrom, previousCloseAt time.Time
	// wholeHistory marks the range that is defined as "everything this feed has"
	// and so can never start before the feed did.
	wholeHistory bool
	// requireCoverage marks a range that promises a length — "the last month" —
	// rather than a session. Such a range is only honest if the feed existed for
	// all of it; a session range draws whatever the session holds, exactly as a
	// stock listed this morning would.
	requireCoverage bool
}

// chartWindowFor is how far back each range reaches.
//
// 1D follows the exchange calendar rather than a rolling 24 hours, for the same
// reason the Pyth path does: a total-return feed keeps publishing a heartbeat
// round after the bell and through the weekend, so a rolling day on a Sunday is a
// flat line of Friday's price presented as today.
func chartWindowFor(chartRange pyth.ChartRange, now time.Time) chartWindow {
	now = now.UTC()
	switch chartRange {
	case pyth.ChartRange1W:
		return backWindow(now, now.AddDate(0, 0, -7), 4)
	case pyth.ChartRange1M:
		return backWindow(now, now.AddDate(0, -1, 0), 4)
	case pyth.ChartRange3M:
		return backWindow(now, now.AddDate(0, -3, 0), 7)
	case pyth.ChartRange1Y:
		return backWindow(now, now.AddDate(-1, 0, 0), 7)
	case pyth.ChartRangeAll:
		return chartWindow{from: time.Time{}, to: now, wholeHistory: true}
	default:
		return dayWindow(now)
	}
}

// backWindow is a plain look-back range: everything from `from` to now, fetched
// with a few days of run-up so the last round before the window can be the
// baseline.
func backWindow(now, from time.Time, runUpDays int) chartWindow {
	return chartWindow{
		from:              from,
		to:                now,
		fetchFrom:         from.AddDate(0, 0, -runUpDays),
		previousCloseFrom: from.AddDate(0, 0, -runUpDays),
		previousCloseAt:   from,
		requireCoverage:   true,
	}
}

func dayWindow(now time.Time) chartWindow {
	session, found := marketcal.LastTradingSession(now)
	if !found {
		// No session inside the calendar's horizon. Degrade to a rolling day rather
		// than to no chart, with a baseline bounded to the day before it.
		return chartWindow{
			from:              now.AddDate(0, 0, -1),
			to:                now,
			fetchFrom:         now.AddDate(0, 0, -3),
			previousCloseFrom: now.AddDate(0, 0, -2),
			previousCloseAt:   now.AddDate(0, 0, -1),
		}
	}
	to := session.PostCloseEnd
	if now.Before(to) {
		to = now
	}
	window := chartWindow{
		from:         session.PreMarketOpen,
		to:           to,
		fetchFrom:    session.PreMarketOpen.AddDate(0, 0, -7),
		regularOpen:  session.RegularOpen,
		regularClose: session.RegularClose,
	}
	if previous, ok := marketcal.PreviousTradingSession(session.Day); ok {
		window.previousCloseFrom = previous.PreMarketOpen
		window.previousCloseAt = previous.RegularClose
	}
	return window
}

// ChartSeries draws one range from the feed's own rounds.
//
// It returns an error only when the chain could not be read — an RPC outage, a
// rate limit, a feed with no priced round — so the app can offer Retry instead of
// showing "no history" for something that is merely down. Every other empty
// answer carries the reason it is empty.
func (a *assetPrices) ChartSeries(ctx context.Context, symbol string, chartRange pyth.ChartRange) (pyth.AssetChartSeries, error) {
	empty := pyth.AssetChartSeries{EmptyReason: pyth.EmptyReasonNoHistory, Range: chartRange}
	if a.live == nil || a.live.catalog == nil || a.live.chain == nil {
		return empty, nil
	}
	if cached, ok := a.charts.get(symbol, chartRange); ok {
		return cached, nil
	}
	feed, err := a.live.catalog.Feed(ctx, symbol)
	if err != nil || strings.TrimSpace(feed) == "" {
		// Not a chain failure: this symbol has no Chainlink feed at all, and no
		// number of retries will give it one.
		return empty, nil
	}

	now := a.live.now().UTC()
	window := chartWindowFor(chartRange, now)
	history, err := a.live.chain.ChainlinkRoundsSince(ctx, feed, window.fetchFrom, chartMaxRounds)
	if err != nil {
		return pyth.AssetChartSeries{}, fmt.Errorf("chainlink chart %s %s: %w", symbol, chartRange, err)
	}

	series, err := seriesFromRounds(symbol, chartRange, window, history, now)
	if err != nil {
		return pyth.AssetChartSeries{}, fmt.Errorf("chainlink chart %s %s: %w", symbol, chartRange, err)
	}
	a.charts.set(symbol, chartRange, series)
	return series, nil
}

// ErrHistoryShort is the read that stopped before it covered the window asked
// for: the feed is old enough, the chain just did not answer far enough back —
// a rate limit, usually, on an endpoint that should have been a keyed one. It is
// an error and not an empty chart, because the range exists and a retry can get
// it.
var ErrHistoryShort = errors.New("round history stopped short of the window")

// seriesFromRounds folds a feed's rounds into one range's series. It never
// invents a point: a window with nothing in it comes back empty, saying whether
// the feed had nothing to publish or simply did not exist yet.
func seriesFromRounds(symbol string, chartRange pyth.ChartRange, window chartWindow, history evm.RoundHistory, now time.Time) (pyth.AssetChartSeries, error) {
	empty := pyth.AssetChartSeries{EmptyReason: pyth.EmptyReasonNoHistory, Range: chartRange}

	// A look-back window that opens before the feed's first round is not a thin
	// chart, it is a chart of a different length than its label claims. Say which
	// day the history starts instead of drawing seven weeks across a year axis.
	//
	// Coverage is proven two ways, both from the chain: the feed's first round is
	// known and is old enough, or the fetch actually reached a round at or before
	// the window opens. When neither holds the range is refused, so an unreadable
	// round 1 costs a chart rather than buying a misleading one.
	if window.requireCoverage {
		first := history.FirstRoundAt
		if first.IsZero() && history.Complete {
			first = history.Oldest()
		}
		reachesBack := !history.Oldest().IsZero() && !history.Oldest().After(window.from)
		if !reachesBack {
			if !first.IsZero() && first.After(window.from) {
				// The feed is younger than the window. No retry will change that,
				// and the date says so plainly.
				empty.EmptyReason = pyth.EmptyReasonBefore(first)
				return empty, nil
			}
			// The feed is old enough but the read did not get there. Three weeks of
			// rounds drawn under a "1M" chip is the same lie as seven weeks under
			// "1Y", so this is a failure, not a chart.
			return pyth.AssetChartSeries{}, ErrHistoryShort
		}
	}

	from := window.from
	if window.wholeHistory {
		from = history.FirstRoundAt
	}

	points := make([]pyth.ChartPoint, 0, len(history.Rounds))
	var previousClose *int64
	for _, round := range history.Rounds {
		at := round.UpdatedAt.UTC()
		price, ok := roundPriceMicros(round)
		if !ok {
			continue
		}
		if !from.IsZero() && at.Before(from) {
			// Outside the drawn window, but a candidate for the baseline.
			if isPreviousClose(window, at) {
				value := price
				previousClose = &value
			}
			continue
		}
		if at.After(now) || (!window.to.IsZero() && at.After(window.to)) {
			continue
		}
		points = append(points, pyth.ChartPoint{Timestamp: at.Unix(), PriceUsdcMicros: price})
	}

	sort.Slice(points, func(i, j int) bool { return points[i].Timestamp < points[j].Timestamp })
	points = dedupeChartPoints(points)
	if len(points) < 2 {
		// One round is a dot, not a curve, and the app cannot draw a line through
		// it. Report the window as empty rather than shipping half a chart.
		return empty, nil
	}

	return pyth.AssetChartSeries{
		Points:                  points,
		Range:                   chartRange,
		Source:                  pyth.ChartSourceChainlink,
		Basis:                   pyth.PriceBasisToken,
		BasisSymbol:             symbol,
		PreviousCloseUsdcMicros: previousClose,
		RegularOpen:             window.regularOpen,
		RegularClose:            window.regularClose,
	}, nil
}

// isPreviousClose reports whether a round outside the window may carry its
// baseline: the last round of the previous session, never a price from days
// earlier that happens to be the newest one the fetch reached.
func isPreviousClose(window chartWindow, at time.Time) bool {
	if window.previousCloseAt.IsZero() {
		return false
	}
	if at.After(window.previousCloseAt) {
		return false
	}
	if !window.previousCloseFrom.IsZero() && at.Before(window.previousCloseFrom) {
		return false
	}
	return true
}

// roundPriceMicros converts one round's 8-decimal answer to USDC micros.
func roundPriceMicros(round evm.RoundData) (int64, bool) {
	if round.UpdatedAt.IsZero() || round.UpdatedAt.Unix() <= 0 {
		return 0, false
	}
	price, _, err := roundToMark(round.UpdatedAt, round)
	if err != nil {
		return 0, false
	}
	return price, true
}

func dedupeChartPoints(points []pyth.ChartPoint) []pyth.ChartPoint {
	if len(points) == 0 {
		return points
	}
	out := points[:1]
	for _, p := range points[1:] {
		if p.Timestamp == out[len(out)-1].Timestamp {
			out[len(out)-1] = p
			continue
		}
		out = append(out, p)
	}
	return out
}

// chartCache holds resolved series per symbol and range. Keys come from the
// catalog, so the map is bounded by the catalog times the six ranges and needs no
// eviction policy.
type chartCache struct {
	now func() time.Time

	mu      sync.RWMutex
	entries map[string]chartCacheEntry
}

type chartCacheEntry struct {
	series    pyth.AssetChartSeries
	expiresAt time.Time
}

func newChartCache(now func() time.Time) *chartCache {
	if now == nil {
		now = time.Now
	}
	return &chartCache{now: now, entries: make(map[string]chartCacheEntry)}
}

func chartCacheTTL(chartRange pyth.ChartRange, empty bool) time.Duration {
	ttl := chartSeriesLongTTL
	if chartRange == pyth.ChartRange1D {
		ttl = chartSeriesDayTTL
	}
	if empty && chartSeriesEmptyTTL < ttl {
		ttl = chartSeriesEmptyTTL
	}
	return ttl
}

func (c *chartCache) get(symbol string, chartRange pyth.ChartRange) (pyth.AssetChartSeries, bool) {
	if c == nil {
		return pyth.AssetChartSeries{}, false
	}
	c.mu.RLock()
	entry, found := c.entries[chartCacheKey(symbol, chartRange)]
	c.mu.RUnlock()
	if !found || !c.now().UTC().Before(entry.expiresAt) {
		return pyth.AssetChartSeries{}, false
	}
	return entry.series, true
}

func (c *chartCache) set(symbol string, chartRange pyth.ChartRange, series pyth.AssetChartSeries) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries[chartCacheKey(symbol, chartRange)] = chartCacheEntry{
		series:    series,
		expiresAt: c.now().UTC().Add(chartCacheTTL(chartRange, len(series.Points) == 0)),
	}
	c.mu.Unlock()
}

// chartCacheKey is case-insensitive in the ticker but keeps the token suffix,
// because "AAPLc" and "AAPLC" are a token and a ticker, not two spellings.
func chartCacheKey(symbol string, chartRange pyth.ChartRange) string {
	return strings.TrimSpace(symbol) + ":" + string(chartRange)
}
