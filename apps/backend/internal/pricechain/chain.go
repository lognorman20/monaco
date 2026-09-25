// Package pricechain values xStock holdings from an ordered chain of price sources:
// Pyth Hermes first, Jupiter's on-chain price for the mint second, fill-derived cost
// basis last. One vendor being down must not silently freeze pot valuation.
package pricechain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

// PythSource is the slice of the Hermes client the chain prices from.
type PythSource interface {
	EquityMark(ctx context.Context, symbol string) (pyth.EquityMark, error)
}

// Config bounds how long marks are reused, how stale or far off a mark may be, and
// how long a failing source is left alone.
type Config struct {
	// MarkTTL is how long a resolved market mark is shared across callers. Every screen
	// values every pot, so without it one page load fans out into dozens of vendor calls.
	MarkTTL time.Duration
	// PythOpenMaxAge rejects a Hermes mark older than this while the cash session is open.
	PythOpenMaxAge time.Duration
	// PythClosedMaxAge rejects a frozen after-hours mark older than a long weekend.
	PythClosedMaxAge time.Duration
	// EntitlementCooldown keeps Pyth closed for a feed after a 401/403: retrying cannot
	// fix a missing grant.
	EntitlementCooldown time.Duration
	// FailureCooldown keeps a source closed after an outage-style failure.
	FailureCooldown time.Duration
	// MinLiquidityUsd rejects a Jupiter price backed by a pool thin enough to push around.
	// Tessera pre-IPO pools measured $120k–$520k on 2026-09-22; the floor stays $10k.
	MinLiquidityUsd float64
	// MaxDeviationBps rejects a Jupiter price this far from the last accepted market mark.
	MaxDeviationBps int64
	// ReferenceMaxAge is how long an accepted mark stays a valid deviation reference.
	ReferenceMaxAge time.Duration
	// MaxCostBasisMultiple rejects a Jupiter price more than this multiple above, or this
	// fraction below, the holding's own cost basis.
	MaxCostBasisMultiple int64
	// ChartTTL is how long a chart series is reused.
	ChartTTL time.Duration
	// WarnWindow is the minimum gap between repeated warnings about the same key.
	WarnWindow time.Duration
	// FetchTimeout bounds one shared upstream resolution.
	FetchTimeout time.Duration
	// Now is the clock; tests inject a fake.
	Now func() time.Time
}

// DefaultConfig returns production bounds.
func DefaultConfig() Config {
	return Config{
		MarkTTL:              10 * time.Second,
		PythOpenMaxAge:       5 * time.Minute,
		PythClosedMaxAge:     96 * time.Hour,
		EntitlementCooldown:  10 * time.Minute,
		FailureCooldown:      30 * time.Second,
		MinLiquidityUsd:      10_000,
		MaxDeviationBps:      2_500,
		ReferenceMaxAge:      6 * time.Hour,
		MaxCostBasisMultiple: 5,
		ChartTTL:             time.Minute,
		WarnWindow:           5 * time.Minute,
		FetchTimeout:         20 * time.Second,
		Now:                  time.Now,
	}
}

const jupiterBreakerKey = "jupiter"

// marketMark is a mark from a live source. ok is false when no live source had a
// usable price; that outcome is cached too so a double outage is not hammered.
type marketMark struct {
	priceMicros int64
	afterHours  bool
	source      pyth.MarkSource
	ok          bool
	fetchedAt   time.Time
}

type reference struct {
	priceMicros int64
	at          time.Time
}

// breaker is an open circuit for one source. entitlement marks a denial that also
// rules out the feed's price history, not just its latest mark.
type breaker struct {
	openUntil   time.Time
	entitlement bool
}

type chartEntry struct {
	series    pyth.AssetChartSeries
	fetchedAt time.Time
}

// Chain is the price-source chain. It implements pyth.Client and
// pyth.AssetPriceClient so existing consumers pick it up unchanged.
type Chain struct {
	cfg     Config
	pyth    PythSource
	jupiter jupiter.PriceClient
	charts  pyth.AssetPriceClient

	mu         sync.Mutex
	marks      map[string]marketMark
	references map[string]reference
	chartCache map[string]chartEntry
	breakers   map[string]breaker
	entitled   map[string]time.Time
	warned     map[string]time.Time

	flights flightGroup
	// warmSlots caps the background fills a burst of cache misses can start, so a
	// page of cold rows leaves rate-limit headroom for the requests someone is
	// actually waiting on.
	warmSlots semaphore
}

// defaultWarmConcurrency is how many cold symbols may be filled in the background
// at once across the whole process.
const defaultWarmConcurrency = 4

// semaphore is a non-blocking counting semaphore: a caller that cannot get a slot
// is told so rather than queued, because the work it guards is optional.
type semaphore struct {
	slots chan struct{}
}

func newSemaphore(n int) semaphore { return semaphore{slots: make(chan struct{}, n)} }

func (s semaphore) acquire() bool {
	if s.slots == nil {
		return false
	}
	select {
	case s.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s semaphore) release() {
	if s.slots == nil {
		return
	}
	select {
	case <-s.slots:
	default:
	}
}

// New builds a chain. pythSource, jupiterPrices and charts may each be nil; a nil
// source is skipped.
func New(pythSource PythSource, jupiterPrices jupiter.PriceClient, charts pyth.AssetPriceClient, cfg Config) *Chain {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Chain{
		cfg:        cfg,
		pyth:       pythSource,
		jupiter:    jupiterPrices,
		charts:     charts,
		marks:      make(map[string]marketMark),
		references: make(map[string]reference),
		chartCache: make(map[string]chartEntry),
		breakers:   make(map[string]breaker),
		entitled:   make(map[string]time.Time),
		warned:     make(map[string]time.Time),
		warmSlots:  newSemaphore(defaultWarmConcurrency),
	}
}

// USDCOnlyPot needs no marks.
func (c *Chain) USDCOnlyPot(_ context.Context, treasury pyth.TreasuryRef) (pyth.NavInput, error) {
	return pyth.NavInput{TreasuryUsdc: treasury.TreasuryUsdc}, nil
}

// MarkedPot marks each holding from the first source in the chain with a usable price.
// Holdings fall back independently: one denied feed does not drag the rest to cost basis.
func (c *Chain) MarkedPot(ctx context.Context, treasury pyth.TreasuryRef, holdings []pyth.CostBasis) (pyth.NavInput, error) {
	marked := make([]pyth.MarkedHolding, 0, len(holdings))
	for _, holding := range holdings {
		markedHolding, err := c.markHolding(ctx, treasury, holding)
		if err != nil {
			return pyth.NavInput{}, err
		}
		marked = append(marked, markedHolding)
	}
	return pyth.NavInput{
		TreasuryUsdc: treasury.TreasuryUsdc,
		Holdings:     marked,
		AfterHours:   pyth.PotAfterHours(marked),
	}, nil
}

func (c *Chain) markHolding(ctx context.Context, treasury pyth.TreasuryRef, holding pyth.CostBasis) (pyth.MarkedHolding, error) {
	out := pyth.MarkedHolding{
		Symbol:       holding.Symbol,
		Mint:         holding.Mint,
		Units:        holding.Units,
		CostBasis:    holding.Price,
		Decimals:     holding.Decimals,
		Kind:         holding.Kind,
		UiMultiplier: holding.UiMultiplier,
	}

	mark := c.marketMark(ctx, holding.Symbol, holding.Mint, xstocks.AssetKind(holding.Kind))
	if mark.ok && mark.source == pyth.MarkSourceJupiter {
		if err := c.checkAgainstCostBasis(mark.priceMicros, holding); err != nil {
			c.warn("cost-basis-bound:"+markKey(holding.Symbol, holding.Mint), "jupiter price rejected against cost basis",
				"symbol", holding.Symbol, "mint", holding.Mint, "price_usdc_micros", mark.priceMicros, "err", err)
			mark.ok = false
		}
	}
	if mark.ok {
		out.MarkUsdc = mark.priceMicros
		out.AfterHours = mark.afterHours
		if holding.Kind == string(xstocks.AssetKindPreIPO) {
			out.AfterHours = false
		}
		out.Source = mark.source
		return out, nil
	}

	costMark, err := pyth.CostBasisMarkPerUnitMicros(holding.Price, holding.Amount, holding.Decimals, holding.UiMultiplier, holding.Kind)
	if err != nil {
		return pyth.MarkedHolding{}, fmt.Errorf("no live price for %s and %w", holding.Symbol, err)
	}
	c.warn("cost-basis:"+markKey(holding.Symbol, holding.Mint), "no live price source; holding valued at cost basis",
		"group_id", treasury.GroupID, "symbol", holding.Symbol, "mint", holding.Mint)
	telemetry.PriceFallback("cost_basis")
	out.MarkUsdc = costMark
	out.Source = pyth.MarkSourceCostBasis
	return out, nil
}

// marketMark returns the shared live mark for a holding, resolving it at most once per
// MarkTTL no matter how many callers ask concurrently.
func (c *Chain) marketMark(ctx context.Context, symbol, mint string, kind xstocks.AssetKind) marketMark {
	key := markKey(symbol, mint)
	if mark, ok := c.cachedMark(key); ok {
		return mark
	}
	result := c.flights.do("mark:"+key, func() any {
		if mark, ok := c.cachedMark(key); ok {
			return mark
		}
		// Detached from the first caller's context: its cancellation must not fail the
		// callers sharing this resolution.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.FetchTimeout)
		defer cancel()
		mark := c.resolveMarketMark(fetchCtx, symbol, mint, kind)
		mark.fetchedAt = c.cfg.Now()
		c.mu.Lock()
		c.marks[key] = mark
		if mark.ok {
			c.references[key] = reference{priceMicros: mark.priceMicros, at: mark.fetchedAt}
		}
		c.mu.Unlock()
		return mark
	})
	return result.(marketMark)
}

func (c *Chain) cachedMark(key string) (marketMark, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	mark, ok := c.marks[key]
	if !ok || c.cfg.Now().Sub(mark.fetchedAt) >= c.cfg.MarkTTL {
		return marketMark{}, false
	}
	return mark, true
}

func (c *Chain) resolveMarketMark(ctx context.Context, symbol, mint string, kind xstocks.AssetKind) marketMark {
	if kind != xstocks.AssetKindPreIPO {
		if mark, ok := c.pythMark(ctx, symbol); ok {
			slog.Info("price chain mark", "symbol", symbol, "mint", mint, "source", mark.source, "price_usdc_micros", mark.priceMicros)
			return mark
		}
	}
	if mark, ok := c.jupiterMark(ctx, symbol, mint); ok {
		slog.Info("price chain mark", "symbol", symbol, "mint", mint, "source", mark.source, "price_usdc_micros", mark.priceMicros)
		return mark
	}
	return marketMark{}
}

func (c *Chain) pythMark(ctx context.Context, symbol string) (marketMark, bool) {
	if c.pyth == nil || strings.TrimSpace(symbol) == "" {
		return marketMark{}, false
	}
	breakerKey := "pyth:" + normalizeSymbol(symbol)
	if !c.breakerAllows(breakerKey) {
		return marketMark{}, false
	}

	mark, err := c.pyth.EquityMark(ctx, symbol)
	if err == nil {
		c.mu.Lock()
		c.entitled[breakerKey] = c.cfg.Now()
		c.mu.Unlock()
		err = c.checkPythMark(mark)
	}
	if err != nil {
		cooldown := c.cfg.FailureCooldown
		denied := pyth.IsEntitlementError(err)
		if denied {
			cooldown = c.cfg.EntitlementCooldown
		}
		c.openBreaker(breakerKey, cooldown, denied)
		c.warn(breakerKey, "pyth price unavailable; falling back to jupiter",
			"symbol", symbol, "retry_in", cooldown.String(), "err", err)
		return marketMark{}, false
	}
	c.closeBreaker(breakerKey)
	return marketMark{
		priceMicros: mark.PriceUsdcMicros,
		afterHours:  mark.AfterHours,
		source:      pyth.MarkSourcePyth,
		ok:          true,
	}, true
}

func (c *Chain) checkPythMark(mark pyth.EquityMark) error {
	if mark.PriceUsdcMicros <= 0 {
		return fmt.Errorf("pyth mark must be positive, got %d", mark.PriceUsdcMicros)
	}
	if mark.PublishedAt.IsZero() {
		return errors.New("pyth mark has no publish time")
	}
	maxAge := c.cfg.PythClosedMaxAge
	if mark.MarketOpen {
		maxAge = c.cfg.PythOpenMaxAge
	}
	if age := c.cfg.Now().Sub(mark.PublishedAt); age > maxAge {
		return fmt.Errorf("pyth mark is stale: published %s ago, max %s", age.Round(time.Second), maxAge)
	}
	return nil
}

func (c *Chain) jupiterMark(ctx context.Context, symbol, mint string) (marketMark, bool) {
	if c.jupiter == nil || strings.TrimSpace(mint) == "" {
		return marketMark{}, false
	}
	if !c.breakerAllows(jupiterBreakerKey) {
		return marketMark{}, false
	}

	prices, err := c.jupiter.Prices(ctx, []string{mint})
	if err != nil {
		c.openBreaker(jupiterBreakerKey, c.cfg.FailureCooldown, false)
		c.warn(jupiterBreakerKey, "jupiter price unavailable",
			"retry_in", c.cfg.FailureCooldown.String(), "err", err)
		return marketMark{}, false
	}
	c.closeBreaker(jupiterBreakerKey)

	price, found := prices[mint]
	if !found {
		c.warn("jupiter-missing:"+mint, "jupiter has no price for mint", "symbol", symbol, "mint", mint)
		return marketMark{}, false
	}
	if err := c.checkJupiterPrice(markKey(symbol, mint), price); err != nil {
		c.warn("jupiter-rejected:"+mint, "jupiter price rejected",
			"symbol", symbol, "mint", mint, "price_usdc_micros", price.PriceUsdcMicros, "err", err)
		return marketMark{}, false
	}
	return marketMark{
		priceMicros: price.PriceUsdcMicros,
		source:      pyth.MarkSourceJupiter,
		ok:          true,
	}, true
}

func (c *Chain) checkJupiterPrice(key string, price jupiter.TokenPrice) error {
	if price.PriceUsdcMicros <= 0 {
		return fmt.Errorf("price must be positive, got %d", price.PriceUsdcMicros)
	}
	if price.LiquidityUsd < c.cfg.MinLiquidityUsd {
		return fmt.Errorf("liquidity $%.0f is below the $%.0f floor", price.LiquidityUsd, c.cfg.MinLiquidityUsd)
	}

	c.mu.Lock()
	ref, ok := c.references[key]
	c.mu.Unlock()
	if !ok || c.cfg.Now().Sub(ref.at) > c.cfg.ReferenceMaxAge {
		return nil
	}
	delta := price.PriceUsdcMicros - ref.priceMicros
	if delta < 0 {
		delta = -delta
	}
	deviationBps, err := domain.MulDivFloor(delta, 10_000, ref.priceMicros)
	if err != nil {
		return fmt.Errorf("deviation vs last good price: %w", err)
	}
	if deviationBps > c.cfg.MaxDeviationBps {
		return fmt.Errorf("deviates %d bps from last good price %d, max %d bps", deviationBps, ref.priceMicros, c.cfg.MaxDeviationBps)
	}
	return nil
}

// checkAgainstCostBasis is the only sanity reference on a cold start, when no market
// mark has been accepted yet.
func (c *Chain) checkAgainstCostBasis(priceMicros int64, holding pyth.CostBasis) error {
	costMark, err := pyth.CostBasisMarkPerUnitMicros(holding.Price, holding.Amount, holding.Decimals, holding.UiMultiplier, holding.Kind)
	if err != nil {
		// No usable cost basis to compare against (e.g. a catalog probe); nothing to check.
		return nil
	}
	multiple := c.cfg.MaxCostBasisMultiple
	if multiple <= 0 {
		return nil
	}
	if priceMicros > costMark*multiple || priceMicros*multiple < costMark {
		return fmt.Errorf("price is outside %dx of cost basis mark %d", multiple, costMark)
	}
	return nil
}

// AssetMark passes through to Pyth; catalog display prices come from Jupiter directly.
func (c *Chain) AssetMark(ctx context.Context, symbol string) (pyth.AssetMark, error) {
	if c.charts == nil {
		return pyth.AssetMark{}, errors.New("pyth asset prices are not configured")
	}
	return c.charts.AssetMark(ctx, symbol)
}

// ChartSeries serves Pyth price history for stocks. Pre-IPO history uses ChartSeriesQuery.
func (c *Chain) ChartSeries(ctx context.Context, symbol string, chartRange pyth.ChartRange) (pyth.AssetChartSeries, error) {
	return c.ChartSeriesQuery(ctx, pyth.ChartQuery{Symbol: symbol, Range: chartRange})
}

// ChartSeriesQuery serves price history when the caller knows the asset kind.
// Stocks still go through the shared chart fetch. Pre-IPO tokens have no history,
// so they return empty without opening a Pyth entitlement check.
func (c *Chain) ChartSeriesQuery(ctx context.Context, q pyth.ChartQuery) (pyth.AssetChartSeries, error) {
	unavailable := pyth.AssetChartSeries{EmptyReason: pyth.EmptyReasonNoHistory}
	if q.Kind == string(xstocks.AssetKindPreIPO) {
		return unavailable, nil
	}
	if c.charts == nil {
		return unavailable, nil
	}
	symbol := q.Symbol
	chartRange := q.Range
	cacheKey := normalizeSymbol(symbol) + "|" + string(chartRange)
	if series, ok := c.cachedChart(cacheKey); ok {
		return series, nil
	}
	if err := ctx.Err(); err != nil {
		return unavailable, err
	}
	if !c.chartsAreKeyless() && !c.pythEntitled(ctx, symbol) {
		return unavailable, nil
	}

	raw, err := c.flights.doCtx(ctx, "chart:"+cacheKey, c.fetchChart(ctx, cacheKey, symbol, chartRange))
	if err != nil {
		return unavailable, err
	}
	result := raw.(chartResult)
	return result.series, result.err
}

type chartResult struct {
	series pyth.AssetChartSeries
	err    error
}

// fetchChart is the shared, detached body of one chart fetch: whoever wins the
// flight runs it, and every other caller — and the cache — gets its answer.
func (c *Chain) fetchChart(ctx context.Context, cacheKey, symbol string, chartRange pyth.ChartRange) func() any {
	return func() any {
		if series, ok := c.cachedChart(cacheKey); ok {
			return chartResult{series: series}
		}
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.FetchTimeout)
		defer cancel()
		series, err := c.charts.ChartSeries(fetchCtx, symbol, chartRange)
		if err != nil {
			return chartResult{err: err}
		}
		// An empty series is cached too. It is an answer the chart client actually
		// got, and leaving it uncached meant a symbol with no history went upstream
		// on every request — on the detail route, which asks for two ranges and which
		// the asset screen polls. ChartTTL is short enough that a symbol whose first
		// bar has just appeared starts drawing within the minute.
		c.mu.Lock()
		c.chartCache[cacheKey] = chartEntry{series: series, fetchedAt: c.cfg.Now()}
		c.mu.Unlock()
		return chartResult{series: series}
	}
}

// CachedChartSeries answers from the chart cache and never goes upstream, and
// schedules a background warm when there is nothing cached.
//
// This is what a list row's sparkline reads. A row's series is decoration: a page
// of twenty rows is worth showing without it, and is never worth waiting on
// Hermes for. So the read path is memory-only and the cold path belongs to a
// background fill, which the next request — or the spark warmer's next pass —
// collects. That is a budget that cannot be exceeded rather than a budget that is
// merely written down.
func (c *Chain) CachedChartSeries(ctx context.Context, symbol string, chartRange pyth.ChartRange) (pyth.AssetChartSeries, bool) {
	if c.charts == nil {
		return pyth.AssetChartSeries{}, false
	}
	cacheKey := normalizeSymbol(symbol) + "|" + string(chartRange)
	if series, ok := c.cachedChart(cacheKey); ok {
		return series, true
	}
	c.warmChart(ctx, cacheKey, symbol, chartRange)
	return pyth.AssetChartSeries{}, false
}

// warmChart starts the shared fetch for a symbol nobody has cached yet, without
// waiting for it. The flight group is what keeps a page of cold rows from
// becoming a page of duplicate fetches: a symbol already being fetched is left
// alone.
func (c *Chain) warmChart(ctx context.Context, cacheKey, symbol string, chartRange pyth.ChartRange) {
	key := "chart:" + cacheKey
	if c.flights.inFlight(key) {
		return
	}
	if !c.warmSlots.acquire() {
		// The warm budget for concurrent cold symbols is spent. The spark warmer's
		// next pass picks this up; a row without a series draws no line, which is
		// what it is supposed to do.
		return
	}
	// Detached on purpose: this outlives the request that noticed the miss.
	warmCtx := context.WithoutCancel(ctx)
	fetch := c.fetchChart(warmCtx, cacheKey, symbol, chartRange)
	go func() {
		defer c.warmSlots.release()
		c.flights.do(key, fetch)
	}()
}

// chartsAreKeyless reports whether the chart client can serve history without a
// Hermes entitlement.
func (c *Chain) chartsAreKeyless() bool {
	source, ok := c.charts.(pyth.KeylessHistorySource)
	return ok && source.HasKeylessHistory()
}

// pythEntitled reports whether Hermes currently serves this symbol's feed to our key.
// A recent successful mark answers it for free; otherwise one shared probe does.
func (c *Chain) pythEntitled(ctx context.Context, symbol string) bool {
	if c.pyth == nil {
		// No mark source to probe with; let the chart client answer for itself.
		return true
	}
	breakerKey := "pyth:" + normalizeSymbol(symbol)
	if denied, known := c.entitlementState(breakerKey); known {
		return !denied
	}
	c.flights.do("probe:"+breakerKey, func() any {
		if _, known := c.entitlementState(breakerKey); known {
			return nil
		}
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.FetchTimeout)
		defer cancel()
		c.pythMark(probeCtx, symbol)
		return nil
	})
	denied, _ := c.entitlementState(breakerKey)
	return !denied
}

// entitlementState returns (denied, known). known is false when neither an open
// entitlement breaker nor a recent successful mark says anything about the feed.
func (c *Chain) entitlementState(breakerKey string) (denied, known bool) {
	now := c.cfg.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if state, open := c.breakers[breakerKey]; open && state.entitlement && now.Before(state.openUntil) {
		return true, true
	}
	if at, ok := c.entitled[breakerKey]; ok && now.Sub(at) < c.cfg.EntitlementCooldown {
		return false, true
	}
	return false, false
}

func (c *Chain) cachedChart(key string) (pyth.AssetChartSeries, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.chartCache[key]
	if !ok || c.cfg.Now().Sub(entry.fetchedAt) >= c.cfg.ChartTTL {
		return pyth.AssetChartSeries{}, false
	}
	return entry.series, true
}

// breakerAllows reports whether a source may be tried: closed, or open long enough
// that the next call is the recovery probe.
func (c *Chain) breakerAllows(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, open := c.breakers[key]
	return !open || !c.cfg.Now().Before(state.openUntil)
}

func (c *Chain) openBreaker(key string, cooldown time.Duration, entitlement bool) {
	c.mu.Lock()
	_, alreadyOpen := c.breakers[key]
	c.breakers[key] = breaker{openUntil: c.cfg.Now().Add(cooldown), entitlement: entitlement}
	if entitlement {
		delete(c.entitled, key)
	}
	c.mu.Unlock()

	if alreadyOpen {
		// A failed recovery probe re-arms an open breaker; only the first open is news.
		return
	}
	source := breakerSource(key)
	telemetry.BreakerOpened(source)
	if source == jupiterBreakerKey {
		// Jupiter is the last live tier. With it open, pots are valued at cost basis and
		// P&L silently stops moving, so this one goes to a person.
		telemetry.Alert(context.Background(), telemetry.AlertEvent{
			Kind:     "price_source_down",
			Key:      "price_source_down:" + source,
			Severity: telemetry.SeverityWarning,
			Title:    "Jupiter price source breaker opened",
			Detail:   "Holdings fall back to cost basis until it recovers.",
			Fields:   map[string]string{"retry_in": cooldown.String()},
		})
	}
}

// breakerSource maps a breaker key to its metric label. Pyth breakers are per symbol
// ("pyth:AAPLx"); the label is the source only, so the series count stays fixed.
func breakerSource(key string) string {
	if source, _, found := strings.Cut(key, ":"); found {
		return source
	}
	return key
}

func (c *Chain) closeBreaker(key string) {
	c.mu.Lock()
	_, wasOpen := c.breakers[key]
	delete(c.breakers, key)
	delete(c.warned, key)
	c.mu.Unlock()
	if wasOpen {
		slog.Info("price source recovered", "source", key)
	}
}

// warn logs at WARN at most once per WarnWindow per key; repeats inside the window
// are dropped so a denied feed does not flood the log on every screen load.
func (c *Chain) warn(key, msg string, args ...any) {
	now := c.cfg.Now()
	c.mu.Lock()
	last, seen := c.warned[key]
	if seen && now.Sub(last) < c.cfg.WarnWindow {
		c.mu.Unlock()
		return
	}
	c.warned[key] = now
	c.mu.Unlock()
	slog.Warn(msg, args...)
}

func markKey(symbol, mint string) string {
	if mint = strings.TrimSpace(mint); mint != "" {
		return mint
	}
	return normalizeSymbol(symbol)
}

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}
