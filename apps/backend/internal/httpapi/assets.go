package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/marketcal"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// AssetsHandlers serves group-agnostic market catalog routes.
//
// Where each number comes from:
//   - the hero price (priceUsdcMicros) is the token's Chainlink total-return mark,
//     the same per-token mark pot valuation uses (Pyth is the marks client's
//     chart source only);
//   - the chart, the stats grid, the day change and the equity reference line are
//     Pyth, and are about the underlying equity, per share;
//   - the token leg of the stock-vs-token card and the spread are Kyber probes.
type AssetsHandlers struct {
	Store   *postgres.Store
	Auth    auth.Verifier
	Wallets wallets.Client
	Catalog b20.Catalog
	// Pyth is the marks client (Chainlink) with Pyth charts in front of its own
	// rounds; it serves the hero price and the chart route.
	Pyth pyth.AssetPriceClient
	// Charts is Pyth history alone, with no Chainlink fallback. The stats grid and
	// the day change read it, because a figure folded from token rounds would be
	// a different instrument under the same label. Nil omits both.
	Charts pyth.MarketDataClient
	// Quotes is Pyth's latest equity price, the reference line on the
	// stock-vs-token card. Nil reports that line as not configured.
	Quotes pyth.EquityQuoteClient
	Dex    dex.Client
	// Home answers "what do my cabals own and what are they voting on" for
	// GET /v1/assets/held. Nil makes that one route unavailable and leaves the
	// catalog routes untouched.
	Home *app.HomeService
	// Now is the market clock; tests pin it so session assertions do not depend on
	// the wall clock of whoever runs them.
	Now func() time.Time

	liqMu    sync.Mutex
	liqCache map[string]cachedLiquidity
}

type cachedLiquidity struct {
	snippet assetLiquidityResponse
	token   pyth.KyberQuote
	at      time.Time
}

const liquidityProbeTTL = 60 * time.Second

func (h *AssetsHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

// marketStatusResponse is the US equities session, shared by every asset in a
// response. It is an envelope field rather than a per-row one because it is one
// fact about the exchange, not a property of an individual stock.
type marketStatusResponse struct {
	Session    string `json:"session"`
	IsOpen     bool   `json:"isOpen"`
	AfterHours bool   `json:"afterHours"`
	// NextSession begins at NextTransition.
	NextSession string `json:"nextSession,omitempty"`
	// NextTransition and AsOf are RFC3339 in UTC; the app converts for display.
	NextTransition string `json:"nextTransition,omitempty"`
	AsOf           string `json:"asOf"`
	Holiday        string `json:"holiday,omitempty"`
	EarlyClose     bool   `json:"earlyClose,omitempty"`
}

type marketAssetResponse struct {
	Symbol          string `json:"symbol"`
	Name            string `json:"name"`
	TokenAddress    string `json:"tokenAddress"`
	Routable        bool   `json:"routable"`
	PriceUsdcMicros *int64 `json:"priceUsdcMicros,omitempty"`
	// Change24h is the underlying equity's move against its previous regular-session
	// close (Pyth), as a decimal ratio. It is the stock's day move, not the token's,
	// and it sits beside a per-token price, so it always travels with its basis.
	Change24h *string `json:"change24h,omitempty"`
	// Change24hBasis is "underlying" and Change24hBasisSymbol names the equity
	// ("AAPL"). Both are set exactly when Change24h is.
	Change24hBasis       string `json:"change24hBasis,omitempty"`
	Change24hBasisSymbol string `json:"change24hBasisSymbol,omitempty"`
	// Spark is the day's closes, downsampled to what a row's sparkline draws, from
	// the same Pyth 1D series change24h is measured on. It is batched onto the page
	// so the app never asks per row: twenty visible rows would otherwise be twenty
	// chart requests, all arriving after the user has scrolled past. Omitted when
	// no series could be sourced in budget; the row then draws no line rather than
	// a flat one, which would read as "this stock did not move".
	Spark []int64 `json:"spark,omitempty"`
	// SparkBasis and SparkBasisSymbol name the instrument Spark is about
	// ("underlying", "AAPL"). They are set exactly when Spark is. The app tints a
	// line by change24h only when the two name the same instrument, and by the
	// line's own first and last close otherwise, so it has to be told.
	SparkBasis       string `json:"sparkBasis,omitempty"`
	SparkBasisSymbol string `json:"sparkBasisSymbol,omitempty"`
	// LogoURL is kept on the wire and always empty: a B20 token publishes no logo,
	// and the app draws the bundled mark for the underlying (or its ticker tile).
	LogoURL string `json:"logoUrl,omitempty"`
}

// dayChangeFields is change24h with the basis section 4.3 of the port requires on
// every Pyth-derived figure. Nil in, empty out: no basis without a figure.
func dayChangeFields(symbol string, change *string) (*string, string, string) {
	if change == nil {
		return nil, "", ""
	}
	return change, pyth.PriceBasisUnderlying, pyth.UnderlyingTicker(symbol)
}

type listAssetsResponse struct {
	Assets  []marketAssetResponse `json:"assets"`
	HasMore bool                  `json:"hasMore"`
	Market  *marketStatusResponse `json:"market,omitempty"`
}

type popularAssetsResponse struct {
	Assets []marketAssetResponse `json:"assets"`
	Market *marketStatusResponse `json:"market,omitempty"`
}

type assetLiquidityResponse struct {
	Label              string `json:"label"`
	Routable           bool   `json:"routable"`
	BuyProbeUsdcMicros int64  `json:"buyProbeUsdcMicros"`
	BuyProbeOutAmount  string `json:"buyProbeOutAmount,omitempty"`
	SellProbeInAmount  string `json:"sellProbeInAmount,omitempty"`
	SellProbeOutAmount string `json:"sellProbeOutAmount,omitempty"`
	// SpreadBps is (ask - bid) / mid of the two probes, in basis points. Absent
	// unless both probes found a route.
	SpreadBps *int `json:"spreadBps,omitempty"`
}

// assetStatsResponse is the stats grid. Every cell is a pointer because "we could
// not source this" and "this is zero" are different answers, and the grid omits a
// cell rather than showing a made-up one. Market cap, P/E and dividend yield have
// no source behind a B20 token and are deliberately absent.
//
// Every cell is a Pyth figure for the underlying equity, per share: Open/High/Low/
// PreviousClose from Benchmarks candles, the 52-week range from Benchmarks daily
// bars, and the confidence interval on the latest equity price. The hero price is
// the token's total-return mark, per token. The two differ by the token's
// multiplier and by market hours, so Basis/BasisSymbol label the grid; without
// that label a hero price above "the day's high" reads as a bug. The Kyber spread
// is about the token, not the share, so it is not in this grid; it is on
// liquidity and stockVsToken.
type assetStatsResponse struct {
	OpenUsdcMicros          *int64 `json:"openUsdcMicros,omitempty"`
	HighUsdcMicros          *int64 `json:"highUsdcMicros,omitempty"`
	LowUsdcMicros           *int64 `json:"lowUsdcMicros,omitempty"`
	PreviousCloseUsdcMicros *int64 `json:"previousCloseUsdcMicros,omitempty"`
	Week52HighUsdcMicros    *int64 `json:"week52HighUsdcMicros,omitempty"`
	Week52LowUsdcMicros     *int64 `json:"week52LowUsdcMicros,omitempty"`
	// ConfUsdcMicros is Pyth's own confidence interval on the latest equity price,
	// and only while that price is live. Omitted once the feed goes stale, because
	// the grid carries no freshness of its own.
	ConfUsdcMicros *int64 `json:"confUsdcMicros,omitempty"`
	// Basis is "underlying"; BasisSymbol names it ("AAPL").
	Basis       string `json:"basis,omitempty"`
	BasisSymbol string `json:"basisSymbol,omitempty"`
}

// referenceQuoteResponse is one line of the stock-vs-token card. Status is "live",
// "stale" or "unavailable"; an unavailable line carries a reason and no price, and
// is never filled in from another line.
type referenceQuoteResponse struct {
	Source          string  `json:"source"`
	Status          string  `json:"status"`
	PriceUsdcMicros *int64  `json:"priceUsdcMicros,omitempty"`
	ConfUsdcMicros  *int64  `json:"confUsdcMicros,omitempty"`
	PublishedAt     *string `json:"publishedAt,omitempty"`
	Reason          string  `json:"reason,omitempty"`
	// BidUsdcMicros and AskUsdcMicros are the Kyber probes behind a dex_kyber mid.
	BidUsdcMicros *int64 `json:"bidUsdcMicros,omitempty"`
	AskUsdcMicros *int64 `json:"askUsdcMicros,omitempty"`
	// ProbedAt is when the Kyber probes behind a dex_kyber mid were taken, RFC3339
	// in UTC. Probes are shared for up to a minute, so it can be that much older
	// than asOf. It is our clock, not a publish time: Kyber does not say when a
	// quote's price was struck, so a dex_kyber line never has publishedAt.
	ProbedAt *string `json:"probedAt,omitempty"`
}

// stockVsTokenResponse compares the token in its pools with its own mark, and
// shows the equity it tracks as a separate reference line.
type stockVsTokenResponse struct {
	// Token is the Kyber quote-implied mid, per token.
	Token referenceQuoteResponse `json:"token"`
	// Mark is the Chainlink total-return mark the premium is measured against, per
	// token. It equals the hero price. PublishedAt is the round's updatedAt, and
	// Status is "stale" when the feed is holding a closed session's close (weekends,
	// holidays) or the round is past the feed's heartbeat.
	Mark referenceQuoteResponse `json:"mark"`
	// Equity is Pyth's price for the underlying, per share. No premium is computed
	// against it: the mark is this price times the token's multiplier, so the two
	// are not the same unit.
	Equity       referenceQuoteResponse `json:"equity"`
	EquitySymbol string                 `json:"equitySymbol"`
	// PremiumBps is Token against Mark. Absent unless both carry a real price and
	// both are live: against a stale mark it would be the market's move since the
	// mark froze, not a premium.
	PremiumBps *int `json:"premiumBps,omitempty"`
	// SpreadBps is the Kyber ask against bid, the same figure as liquidity.spreadBps.
	SpreadBps *int   `json:"spreadBps,omitempty"`
	AsOf      string `json:"asOf"`
}

type assetDetailResponse struct {
	Symbol          string  `json:"symbol"`
	Name            string  `json:"name"`
	TokenAddress    string  `json:"tokenAddress"`
	Routable        bool    `json:"routable"`
	PriceUsdcMicros *int64  `json:"priceUsdcMicros,omitempty"`
	Change24h       *string `json:"change24h,omitempty"`
	// Change24hBasis / Change24hBasisSymbol: see marketAssetResponse.
	Change24hBasis       string                 `json:"change24hBasis,omitempty"`
	Change24hBasisSymbol string                 `json:"change24hBasisSymbol,omitempty"`
	Liquidity            assetLiquidityResponse `json:"liquidity"`
	// MarketSession and AfterHours repeat Market.Session and Market.AfterHours on
	// the detail response, because the hero header reads them directly.
	MarketSession string                `json:"marketSession,omitempty"`
	AfterHours    bool                  `json:"afterHours"`
	Market        *marketStatusResponse `json:"market,omitempty"`
	Stats         *assetStatsResponse   `json:"stats,omitempty"`
	StockVsToken  *stockVsTokenResponse `json:"stockVsToken,omitempty"`
}

type assetChartResponse struct {
	Points      []pyth.ChartPoint `json:"points"`
	EmptyReason string            `json:"emptyReason,omitempty"`
	// PreviousCloseUsdcMicros is the previous regular session's close — the dashed
	// baseline the day chart measures its change against.
	PreviousCloseUsdcMicros *int64 `json:"previousCloseUsdcMicros,omitempty"`
	// Range echoes the window served, so a response arriving after the user has
	// already tapped another chip can be discarded instead of drawn.
	Range string `json:"range"`
	// Source is "benchmarks", "hermes" or "chainlink".
	Source string `json:"source,omitempty"`
	// Basis is which instrument the curve is: "underlying" for Pyth (the equity,
	// per share), "token" for the Chainlink fallback. BasisSymbol names it.
	Basis       string                `json:"basis,omitempty"`
	BasisSymbol string                `json:"basisSymbol,omitempty"`
	Market      *marketStatusResponse `json:"market,omitempty"`
}

// ListAssetsHandler handles GET /v1/assets.
func (h *AssetsHandlers) ListAssetsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/assets")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if _, err := h.authorizeUser(ctx, token); err != nil {
		writeAssetsError(ctx, log, w, err)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	limit := parseCatalogLimit(r.URL.Query().Get("limit"))
	offset := parseCatalogOffset(r.URL.Query().Get("offset"))

	page, err := h.Catalog.Search(ctx, query, limit, offset)
	if err != nil {
		logJSONError(ctx, log, "catalog_search_failed", w, http.StatusInternalServerError, "internal server error", "query", query, "err", err.Error())
		return
	}

	resp := listAssetsResponse{
		Assets:  h.enrichAssets(ctx, page.Assets),
		HasMore: page.HasMore,
		Market:  h.marketStatus(),
	}
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok", "query", query, "limit", limit, "offset", offset, "result_count", len(resp.Assets))
}

// PopularAssetsHandler handles GET /v1/assets/popular.
func (h *AssetsHandlers) PopularAssetsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/assets/popular")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if _, err := h.authorizeUser(ctx, token); err != nil {
		writeAssetsError(ctx, log, w, err)
		return
	}

	limit := parsePopularLimit(r.URL.Query().Get("limit"))
	assets, err := h.Catalog.Popular(ctx)
	if err == nil && len(assets) > limit {
		assets = assets[:limit]
	}
	if err != nil {
		logJSONError(ctx, log, "popular_assets_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	resp := popularAssetsResponse{Assets: h.enrichAssets(ctx, assets), Market: h.marketStatus()}
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok", "limit", limit, "result_count", len(resp.Assets))
}

// GetAssetHandler handles GET /v1/assets/{symbol}.
func (h *AssetsHandlers) GetAssetHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/assets/{symbol}")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if _, err := h.authorizeUser(ctx, token); err != nil {
		writeAssetsError(ctx, log, w, err)
		return
	}

	symbol := strings.TrimSpace(r.PathValue("symbol"))
	if symbol == "" {
		logJSONError(ctx, log, "missing_symbol", w, http.StatusNotFound, "asset not found")
		return
	}

	asset, found, err := h.lookupAsset(ctx, symbol)
	if err != nil {
		logJSONError(ctx, log, "asset_lookup_failed", w, http.StatusInternalServerError, "internal server error", "symbol", symbol, "err", err.Error())
		return
	}
	if !found {
		logJSONError(ctx, log, "asset_not_found", w, http.StatusNotFound, "asset not found", "symbol", symbol)
		return
	}

	detail := h.buildAssetDetail(ctx, asset)
	writeMarketJSON(ctx, log, w, http.StatusOK, detail, "ok",
		"symbol", asset.Symbol,
		"has_stats", detail.Stats != nil,
		"has_stock_vs_token", detail.StockVsToken != nil,
	)
}

// GetAssetChartHandler handles GET /v1/assets/{symbol}/chart.
func (h *AssetsHandlers) GetAssetChartHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/assets/{symbol}/chart")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if _, err := h.authorizeUser(ctx, token); err != nil {
		writeAssetsError(ctx, log, w, err)
		return
	}

	symbol := strings.TrimSpace(r.PathValue("symbol"))
	if symbol == "" {
		logJSONError(ctx, log, "missing_symbol", w, http.StatusNotFound, "asset not found")
		return
	}

	chartRange, err := pyth.ParseChartRange(r.URL.Query().Get("range"))
	if err != nil {
		logJSONError(ctx, log, "invalid_chart_range", w, http.StatusBadRequest, "invalid chart range", "symbol", symbol)
		return
	}

	asset, found, err := h.lookupAsset(ctx, symbol)
	if err != nil {
		logJSONError(ctx, log, "asset_lookup_failed", w, http.StatusInternalServerError, "internal server error", "symbol", symbol, "err", err.Error())
		return
	}
	if !found {
		logJSONError(ctx, log, "asset_not_found", w, http.StatusNotFound, "asset not found", "symbol", symbol)
		return
	}

	// The catalog's own symbol, not the path's spelling: the Pyth feed is derived
	// from the token suffix, and "AAPLC" typed in upper case is a different ticker.
	series := pyth.AssetChartSeries{EmptyReason: pyth.EmptyReasonNoHistory}
	if h.Pyth != nil {
		series, err = h.Pyth.ChartSeries(ctx, asset.Symbol, chartRange)
		if err != nil {
			logJSONError(ctx, log, "chart_failed", w, http.StatusInternalServerError, "internal server error", "symbol", asset.Symbol, "err", err.Error())
			return
		}
	}

	resp := assetChartResponse{
		Points:                  series.Points,
		EmptyReason:             series.EmptyReason,
		PreviousCloseUsdcMicros: series.PreviousCloseUsdcMicros,
		Range:                   string(chartRange),
		Source:                  series.Source,
		Basis:                   series.Basis,
		BasisSymbol:             series.BasisSymbol,
		Market:                  h.marketStatus(),
	}
	if resp.Points == nil {
		// A Swift decoder reads an absent array and an empty one alike, but not a
		// JSON null. Always ship a list.
		resp.Points = []pyth.ChartPoint{}
	}
	if len(resp.Points) == 0 && resp.EmptyReason == "" {
		resp.EmptyReason = pyth.EmptyReasonNoHistory
	}
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok",
		"symbol", asset.Symbol,
		"range", chartRange,
		"point_count", len(resp.Points),
		"source", series.Source,
	)
}

func (h *AssetsHandlers) authorizeUser(ctx context.Context, accessToken string) (string, error) {
	identity, err := h.Auth.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return "", auth.ErrUnauthorized
		}
		return "", fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.Store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", app.ErrUserNotFound
	}
	return user.ID, nil
}

// marketRows is the one place a stock row's market figures come from, shared with
// the held route and the cabal screen so the same instrument is read the same way
// everywhere.
func (h *AssetsHandlers) marketRows() *MarketRowSource {
	return &MarketRowSource{Catalog: h.Catalog, Marks: h.Pyth, Charts: h.Charts}
}

func (h *AssetsHandlers) lookupAsset(ctx context.Context, symbol string) (b20.Asset, bool, error) {
	return h.marketRows().LookupAsset(ctx, symbol)
}

// enrichAssets attaches the Chainlink mark, the underlying's day change and the
// day's sparkline.
func (h *AssetsHandlers) enrichAssets(ctx context.Context, assets []b20.Asset) []marketAssetResponse {
	return h.marketRows().Enrich(ctx, assets)
}

// fetchPrices loads current Chainlink marks, keyed by token address.
func (h *AssetsHandlers) fetchPrices(ctx context.Context, assets []b20.Asset) map[string]assetPriceSnapshot {
	return h.marketRows().Prices(ctx, assets)
}

// marketSectionTimeout bounds the reads the detail screen fans out — the mark, the
// Kyber probes, the equity quote and the two Pyth ranges the stats come from. They
// are all cached and all optional, so a slow upstream costs a missing card, never
// a hung request.
const marketSectionTimeout = 4 * time.Second

func (h *AssetsHandlers) marketStatus() *marketStatusResponse {
	status := marketcal.StatusAt(h.now())
	resp := &marketStatusResponse{
		Session:     string(status.Session),
		IsOpen:      status.IsOpen,
		AfterHours:  status.AfterHours,
		NextSession: string(status.NextSession),
		AsOf:        status.AsOf.UTC().Format(time.RFC3339),
		Holiday:     status.Holiday,
		EarlyClose:  status.EarlyClose,
	}
	if !status.NextTransition.IsZero() {
		resp.NextTransition = status.NextTransition.UTC().Format(time.RFC3339)
	}
	return resp
}

func (h *AssetsHandlers) buildAssetDetail(ctx context.Context, asset b20.Asset) assetDetailResponse {
	detail := assetDetailResponse{
		Symbol:       asset.Symbol,
		Name:         asset.Name,
		TokenAddress: asset.TokenAddress,
	}
	status := h.marketStatus()
	detail.Market = status
	detail.MarketSession = status.Session
	detail.AfterHours = status.AfterHours

	// Every read here is independent, so they run together and the screen costs
	// one round trip's latency, bounded by the section timeout.
	sectionCtx, cancel := context.WithTimeout(ctx, marketSectionTimeout)
	defer cancel()

	var (
		prices    map[string]assetPriceSnapshot
		liquidity assetLiquidityResponse
		tokenLeg  pyth.KyberQuote
		equity    pyth.ReferenceQuote
		day       pyth.AssetChartSeries
		year      pyth.AssetChartSeries
		wg        sync.WaitGroup
	)
	wg.Add(5)
	go func() {
		defer wg.Done()
		prices = h.fetchPrices(sectionCtx, []b20.Asset{asset})
	}()
	go func() {
		defer wg.Done()
		liquidity, tokenLeg = h.liquiditySnippet(sectionCtx, asset)
	}()
	go func() {
		defer wg.Done()
		equity = h.equityQuote(sectionCtx, asset.Symbol)
	}()
	go func() {
		defer wg.Done()
		day = h.underlyingSeries(sectionCtx, asset.Symbol, pyth.ChartRange1D)
	}()
	go func() {
		defer wg.Done()
		year = h.underlyingSeries(sectionCtx, asset.Symbol, pyth.ChartRange1Y)
	}()
	wg.Wait()

	var mark *pyth.AssetMark
	if price, ok := prices[tokenKey(asset.TokenAddress)]; ok && price.PriceUsdcMicros > 0 {
		detail.PriceUsdcMicros = &price.PriceUsdcMicros
		mark = &price.Mark
	}
	detail.Change24h, detail.Change24hBasis, detail.Change24hBasisSymbol = dayChangeFields(asset.Symbol, pyth.DayChange(day))
	detail.Liquidity = liquidity
	detail.Routable = asset.Routable || strings.TrimSpace(asset.TokenAddress) != "" || liquidity.Routable
	now := h.now()
	detail.StockVsToken = stockVsTokenResponseFor(asset.Symbol, tokenLeg, pyth.MarkQuote(mark, now), equity, now)

	// Live only. Priced() is also true for a stale quote — the frozen last print
	// Pyth keeps republishing after the bell — and its confidence interval is the
	// confidence of that frozen print, not of anything current. The grid has no
	// freshness field to say so, and the cell would sit unlabelled beside
	// Benchmarks candles for the session in progress while the same price on the
	// card is explicitly marked stale. A number we cannot date is left out.
	var confMicros *int64
	if equity.Status == pyth.QuoteStatusLive && equity.ConfUsdcMicros > 0 {
		conf := equity.ConfUsdcMicros
		confMicros = &conf
	}
	detail.Stats = assetStatsResponseFor(asset.Symbol, day, year, confMicros)
	return detail
}

// underlyingSeries reads Pyth history for the stats grid and the day change, and
// keeps only Benchmarks candles of the underlying equity.
//
// Not merely any underlying-basis series: the Hermes sampler is the same equity
// feed but a price at an instant every two hours (1D) or every fourteen days (1Y).
// Folded as candles it would give an "Open" that is the first sample after the
// bell rather than the opening print, and a "52-week high" of 27 samples. Those
// are not what the labels mean, so on a Benchmarks outage the grid omits them.
func (h *AssetsHandlers) underlyingSeries(ctx context.Context, symbol string, chartRange pyth.ChartRange) pyth.AssetChartSeries {
	if h.Charts == nil {
		return pyth.AssetChartSeries{}
	}
	series, err := h.Charts.ChartSeries(ctx, symbol, chartRange)
	if err != nil || series.Basis != pyth.PriceBasisUnderlying || series.Source != pyth.ChartSourceBenchmarks {
		return pyth.AssetChartSeries{}
	}
	return series
}

func (h *AssetsHandlers) equityQuote(ctx context.Context, symbol string) pyth.ReferenceQuote {
	if h.Quotes == nil {
		return pyth.ReferenceQuote{Source: pyth.QuoteSourcePythEquity, Status: pyth.QuoteStatusUnavailable, Reason: pyth.QuoteReasonNotConfigured}
	}
	return h.Quotes.EquityQuote(ctx, symbol)
}

func assetStatsResponseFor(symbol string, day, year pyth.AssetChartSeries, confMicros *int64) *assetStatsResponse {
	stats := pyth.SessionStats(day)
	stats.Week52HighUsdcMicros, stats.Week52LowUsdcMicros = pyth.Week52Range(year)
	stats.ConfUsdcMicros = confMicros
	if !stats.HasFigures() {
		// Nothing could be sourced. Omit the grid rather than ship an empty object.
		return nil
	}
	// Every candle-derived cell and the confidence interval are Pyth's equity feed,
	// so the grid is the underlying's whichever of its cells were filled.
	return &assetStatsResponse{
		OpenUsdcMicros:          stats.OpenUsdcMicros,
		HighUsdcMicros:          stats.HighUsdcMicros,
		LowUsdcMicros:           stats.LowUsdcMicros,
		PreviousCloseUsdcMicros: stats.PreviousCloseUsdcMicros,
		Week52HighUsdcMicros:    stats.Week52HighUsdcMicros,
		Week52LowUsdcMicros:     stats.Week52LowUsdcMicros,
		ConfUsdcMicros:          stats.ConfUsdcMicros,
		Basis:                   pyth.PriceBasisUnderlying,
		BasisSymbol:             pyth.UnderlyingTicker(symbol),
	}
}

// stockVsTokenResponseFor builds the card, or nil unless the token leg has a
// price, which needs both Kyber probes to have routed. The card is about the
// token in its pools; without that leg it is the hero price and a per-share
// reference line, which the rest of the screen already shows.
func stockVsTokenResponseFor(symbol string, token pyth.KyberQuote, mark, equity pyth.ReferenceQuote, asOf time.Time) *stockVsTokenResponse {
	if !token.Quote.Priced() {
		return nil
	}
	tokenResp := referenceQuoteResponseFor(token.Quote)
	ask, bid := token.AskUsdcMicros, token.BidUsdcMicros
	tokenResp.AskUsdcMicros, tokenResp.BidUsdcMicros = &ask, &bid
	if !token.ProbedAt.IsZero() {
		probedAt := token.ProbedAt.UTC().Format(time.RFC3339)
		tokenResp.ProbedAt = &probedAt
	}
	return &stockVsTokenResponse{
		Token:        tokenResp,
		Mark:         referenceQuoteResponseFor(mark),
		Equity:       referenceQuoteResponseFor(equity),
		EquitySymbol: pyth.UnderlyingTicker(symbol),
		PremiumBps:   pyth.PremiumBps(token.Quote, mark),
		SpreadBps:    token.SpreadBps,
		AsOf:         asOf.UTC().Format(time.RFC3339),
	}
}

func referenceQuoteResponseFor(quote pyth.ReferenceQuote) referenceQuoteResponse {
	resp := referenceQuoteResponse{
		Source: string(quote.Source),
		Status: string(quote.Status),
		Reason: quote.Reason,
	}
	if quote.Priced() {
		price := quote.PriceUsdcMicros
		resp.PriceUsdcMicros = &price
	}
	if quote.Priced() && quote.ConfUsdcMicros > 0 {
		conf := quote.ConfUsdcMicros
		resp.ConfUsdcMicros = &conf
	}
	if !quote.PublishedAt.IsZero() {
		publishedAt := quote.PublishedAt.UTC().Format(time.RFC3339)
		resp.PublishedAt = &publishedAt
	}
	return resp
}

// liquiditySnippet probes Kyber once per token per minute: a 1 USDC buy and a
// 1-token sell. The token leg carries the probe time, so a leg served from this
// cache says it is up to a minute old. The pair gives the liquidity strip, the spread and the token leg
// of the stock-vs-token card. A probe that errored (a timeout, an outage) is not
// cached, because it says nothing about the pool; a real no-route is.
func (h *AssetsHandlers) liquiditySnippet(ctx context.Context, asset b20.Asset) (assetLiquidityResponse, pyth.KyberQuote) {
	key := strings.ToLower(strings.TrimSpace(asset.TokenAddress))
	if key != "" {
		h.liqMu.Lock()
		if h.liqCache != nil {
			if hit, ok := h.liqCache[key]; ok && h.now().Sub(hit.at) < liquidityProbeTTL {
				h.liqMu.Unlock()
				return hit.snippet, hit.token
			}
		}
		h.liqMu.Unlock()
	}

	snippet := assetLiquidityResponse{
		Label:              "Via DEX",
		Routable:           false,
		BuyProbeUsdcMicros: app.CatalogRoutabilityProbeMicros,
	}
	probe := pyth.KyberProbe{
		BuyInUsdcMicros:  app.CatalogRoutabilityProbeMicros,
		SellInAtomics:    b20.TokenAtomicScale,
		TokenAtomicScale: b20.TokenAtomicScale,
	}
	if h.Dex == nil || key == "" {
		return snippet, pyth.KyberTokenQuote(probe)
	}

	var (
		buyQuote, sellQuote dex.Quote
		buyErr, sellErr     error
		wg                  sync.WaitGroup
	)
	probedAt := h.now()
	wg.Add(2)
	go func() {
		defer wg.Done()
		buyQuote, buyErr = h.Dex.QuoteBuy(ctx, asset.TokenAddress, big.NewInt(app.CatalogRoutabilityProbeMicros))
	}()
	go func() {
		defer wg.Done()
		sellQuote, sellErr = h.Dex.QuoteSell(ctx, asset.TokenAddress, big.NewInt(b20.TokenAtomicScale))
	}()
	wg.Wait()

	if buyErr == nil && buyQuote.Routable && buyQuote.AmountOut != nil {
		snippet.Routable = true
		snippet.BuyProbeOutAmount = buyQuote.AmountOut.String()
		probe.BuyOutAtomics = snippet.BuyProbeOutAmount
	}
	if sellErr == nil && sellQuote.Routable && sellQuote.AmountOut != nil {
		snippet.SellProbeInAmount = strconv.FormatInt(b20.TokenAtomicScale, 10)
		snippet.SellProbeOutAmount = sellQuote.AmountOut.String()
		probe.SellOutUsdcMicros = snippet.SellProbeOutAmount
	}
	tokenLeg := pyth.KyberTokenQuote(probe)
	tokenLeg.ProbedAt = probedAt
	if buyErr != nil || sellErr != nil {
		if tokenLeg.Quote.Status == pyth.QuoteStatusUnavailable {
			tokenLeg.Quote.Reason = pyth.QuoteReasonUpstream
		}
		return snippet, tokenLeg
	}
	snippet.SpreadBps = tokenLeg.SpreadBps

	h.liqMu.Lock()
	if h.liqCache == nil {
		h.liqCache = make(map[string]cachedLiquidity)
	}
	h.liqCache[key] = cachedLiquidity{snippet: snippet, token: tokenLeg, at: probedAt}
	h.liqMu.Unlock()
	return snippet, tokenLeg
}

func parsePopularLimit(raw string) int {
	const defaultLimit = 10
	const maxLimit = 20
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func writeAssetsError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
	default:
		logJSONError(ctx, log, "internal_error", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
	}
}

func writeMarketJSON(ctx context.Context, log *requestLog, w http.ResponseWriter, status int, payload any, outcome string, attrs ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
	logJSONOK(ctx, log, outcome, attrs...)
}
