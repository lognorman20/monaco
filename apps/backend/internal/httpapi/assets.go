package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/marketcal"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// AssetsHandlers serves group-agnostic market catalog routes.
type AssetsHandlers struct {
	Store   *postgres.Store
	Privy   privy.Client
	Catalog xstocks.CatalogSearcher
	// Pyth backs historical chart series only — display prices come from Price.
	Pyth    pyth.AssetPriceClient
	Jupiter jupiter.Client
	// Price batches current display prices via Jupiter's Price API (not Swap API v2).
	Price jupiter.PriceClient
	// Quotes serves the stock-vs-token comparison straight from the Pyth feeds.
	// Deliberately not the price chain: the comparison exists to show what the two
	// feeds actually said, and a valuation-policy-filtered mark would hide exactly
	// the divergence the card is about. Nil simply omits the card.
	Quotes pyth.ReferenceQuoteClient
	// Now is the market clock; tests pin it so session assertions do not depend on
	// the wall clock of whoever runs them.
	Now func() time.Time
}

func (h *AssetsHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

// marketStatusResponse is the US equities session, shared by every asset in a
// response. It is an envelope field rather than a per-row one because it is one
// global fact about the exchange, not a property of an individual stock.
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
	Symbol          string  `json:"symbol"`
	Name            string  `json:"name"`
	SolanaMint      string  `json:"solanaMint"`
	Routable        bool    `json:"routable"`
	PriceUsdcMicros *int64  `json:"priceUsdcMicros,omitempty"`
	Change24h       *string `json:"change24h,omitempty"`
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
	SpreadBps          *int   `json:"spreadBps,omitempty"`
}

// assetStatsResponse is the stats grid. Every cell is a pointer because "we could
// not source this" and "this is zero" are different answers, and the grid omits a
// cell rather than showing a made-up one. Market cap, P/E and dividend yield have
// no source behind xStocks and are deliberately absent.
type assetStatsResponse struct {
	OpenUsdcMicros          *int64 `json:"openUsdcMicros,omitempty"`
	HighUsdcMicros          *int64 `json:"highUsdcMicros,omitempty"`
	LowUsdcMicros           *int64 `json:"lowUsdcMicros,omitempty"`
	PreviousCloseUsdcMicros *int64 `json:"previousCloseUsdcMicros,omitempty"`
	Week52HighUsdcMicros    *int64 `json:"week52HighUsdcMicros,omitempty"`
	Week52LowUsdcMicros     *int64 `json:"week52LowUsdcMicros,omitempty"`
	// SpreadBps is the round-trip trading cost implied by the Jupiter probes.
	SpreadBps *int `json:"spreadBps,omitempty"`
	// ConfUsdcMicros is Pyth's own confidence interval on the latest equity mark.
	ConfUsdcMicros *int64 `json:"confUsdcMicros,omitempty"`
}

// referenceQuoteResponse is one leg of the stock-vs-token comparison. Status is
// "live", "stale" or "unavailable"; an unavailable leg carries a reason and no
// price, and is never filled in from the other leg.
type referenceQuoteResponse struct {
	Source          string  `json:"source"`
	Status          string  `json:"status"`
	PriceUsdcMicros *int64  `json:"priceUsdcMicros,omitempty"`
	ConfUsdcMicros  *int64  `json:"confUsdcMicros,omitempty"`
	PublishedAt     *string `json:"publishedAt,omitempty"`
	Reason          string  `json:"reason,omitempty"`
}

type stockVsTokenResponse struct {
	Equity referenceQuoteResponse `json:"equity"`
	Token  referenceQuoteResponse `json:"token"`
	// PremiumBps is how far the token trades above or below the underlying. Absent
	// unless both legs carry a real price.
	PremiumBps *int   `json:"premiumBps,omitempty"`
	AsOf       string `json:"asOf"`
}

type assetDetailResponse struct {
	Symbol          string                 `json:"symbol"`
	Name            string                 `json:"name"`
	SolanaMint      string                 `json:"solanaMint"`
	Routable        bool                   `json:"routable"`
	PriceUsdcMicros *int64                 `json:"priceUsdcMicros,omitempty"`
	Change24h       *string                `json:"change24h,omitempty"`
	Liquidity       assetLiquidityResponse `json:"liquidity"`
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
	// PreviousCloseUsdcMicros is the last close before the window opened — the
	// dashed baseline the day chart measures its change against.
	PreviousCloseUsdcMicros *int64 `json:"previousCloseUsdcMicros,omitempty"`
	// Range echoes the window served, so a response arriving after the user has
	// already tapped another chip can be discarded instead of drawn.
	Range string `json:"range"`
	// Source is "benchmarks" or "hermes" — a dense candle series or the sparse
	// sampled fallback.
	Source string                `json:"source,omitempty"`
	Market *marketStatusResponse `json:"market,omitempty"`
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
		if errors.Is(err, xstocks.ErrInvalidResponse) {
			logJSONError(ctx, log, "invalid_catalog_query", w, http.StatusBadRequest, "invalid catalog query", "query", query)
			return
		}
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
	assets, err := xstocks.Popular(ctx, h.Catalog, limit)
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
	writeMarketJSON(ctx, log, w, http.StatusOK, detail, "ok", "symbol", symbol)
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

	if _, found, err := h.lookupAsset(ctx, symbol); err != nil {
		logJSONError(ctx, log, "asset_lookup_failed", w, http.StatusInternalServerError, "internal server error", "symbol", symbol, "err", err.Error())
		return
	} else if !found {
		logJSONError(ctx, log, "asset_not_found", w, http.StatusNotFound, "asset not found", "symbol", symbol)
		return
	}

	var series pyth.AssetChartSeries
	if h.Pyth != nil {
		series, err = h.Pyth.ChartSeries(ctx, symbol, chartRange)
		if err != nil {
			logJSONError(ctx, log, "chart_failed", w, http.StatusInternalServerError, "internal server error", "symbol", symbol, "err", err.Error())
			return
		}
	} else {
		series = pyth.AssetChartSeries{EmptyReason: "price history unavailable"}
	}

	resp := assetChartResponse{
		Points:                  series.Points,
		EmptyReason:             series.EmptyReason,
		PreviousCloseUsdcMicros: series.PreviousCloseUsdcMicros,
		Range:                   string(chartRange),
		Source:                  series.Source,
		Market:                  h.marketStatus(),
	}
	if resp.Points == nil {
		// An absent array and an empty one read the same to a Swift decoder, but a
		// JSON null does not. Always ship a list.
		resp.Points = []pyth.ChartPoint{}
	}
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok",
		"symbol", symbol,
		"range", chartRange,
		"point_count", len(resp.Points),
		"source", series.Source,
		"requested_samples", series.RequestedSamples,
		"failed_samples", series.FailedSamples,
	)
}

func (h *AssetsHandlers) authorizeUser(ctx context.Context, accessToken string) (string, error) {
	identity, err := h.Privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return "", privy.ErrInvalidToken
		}
		return "", fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.Store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", app.ErrUserNotFound
	}
	return user.ID, nil
}

func (h *AssetsHandlers) lookupAsset(ctx context.Context, symbol string) (xstocks.CatalogAsset, bool, error) {
	page, err := h.Catalog.Search(ctx, symbol, 5, 0)
	if err != nil {
		return xstocks.CatalogAsset{}, false, err
	}
	needle := strings.ToUpper(strings.TrimSpace(symbol))
	for _, asset := range page.Assets {
		if strings.EqualFold(strings.TrimSpace(asset.Symbol), needle) {
			return asset, true, nil
		}
	}
	return xstocks.CatalogAsset{}, false, nil
}

// enrichAssets marks every asset with one batched Jupiter Price API call instead
// of a per-asset round trip (Pyth or Jupiter QuoteBuy). Shared by the list and
// popular routes — both just display current price, so both get the same source.
func (h *AssetsHandlers) enrichAssets(ctx context.Context, assets []xstocks.CatalogAsset) []marketAssetResponse {
	prices := h.fetchPrices(ctx, assets)
	out := make([]marketAssetResponse, 0, len(assets))
	for _, asset := range assets {
		out = append(out, marketAssetResponseFor(asset, prices))
	}
	return out
}

// fetchPrices batches current USD marks for assets in one Jupiter Price API call.
// A nil Price client or a failed fetch degrades to "no price" rather than erroring
// the whole catalog response.
func (h *AssetsHandlers) fetchPrices(ctx context.Context, assets []xstocks.CatalogAsset) map[string]jupiter.TokenPrice {
	if h.Price == nil {
		return nil
	}
	mints := make([]string, 0, len(assets))
	for _, asset := range assets {
		if mint := strings.TrimSpace(asset.SolanaMint); mint != "" {
			mints = append(mints, mint)
		}
	}
	prices, err := h.Price.Prices(ctx, mints)
	if err != nil {
		return nil
	}
	return prices
}

func marketAssetResponseFor(asset xstocks.CatalogAsset, prices map[string]jupiter.TokenPrice) marketAssetResponse {
	resp := marketAssetResponse{
		Symbol:     asset.Symbol,
		Name:       asset.Name,
		SolanaMint: asset.SolanaMint,
		Routable:   asset.Routable,
	}
	if price, ok := prices[asset.SolanaMint]; ok && price.PriceUsdcMicros > 0 {
		resp.PriceUsdcMicros = &price.PriceUsdcMicros
		resp.Change24h = price.Change24h
	}
	return resp
}

// marketSectionTimeout bounds the extra reads the detail screen wants — the two
// Pyth feeds and the two chart ranges the stats come from. They are all cached and
// all optional, so a slow upstream costs a missing card, never a hung request.
const marketSectionTimeout = 4 * time.Second

func (h *AssetsHandlers) marketStatus() *marketStatusResponse {
	status := marketcal.StatusAt(h.now())
	resp := &marketStatusResponse{
		Session:     string(status.Session),
		IsOpen:      status.IsOpen,
		AfterHours:  status.AfterHours,
		NextSession: string(status.NextSession),
		AsOf:        status.AsOf.Format(time.RFC3339),
		Holiday:     status.Holiday,
		EarlyClose:  status.EarlyClose,
	}
	if !status.NextTransition.IsZero() {
		resp.NextTransition = status.NextTransition.Format(time.RFC3339)
	}
	return resp
}

func (h *AssetsHandlers) buildAssetDetail(ctx context.Context, asset xstocks.CatalogAsset) assetDetailResponse {
	// The mark is needed for the spread, so price first and probe once. Probing before the
	// price as well would double every Jupiter quote this screen costs.
	prices := h.fetchPrices(ctx, []xstocks.CatalogAsset{asset})
	var markMicros *int64
	detail := assetDetailResponse{
		Symbol:     asset.Symbol,
		Name:       asset.Name,
		SolanaMint: asset.SolanaMint,
		Routable:   asset.Routable,
	}
	if price, ok := prices[asset.SolanaMint]; ok && price.PriceUsdcMicros > 0 {
		detail.PriceUsdcMicros = &price.PriceUsdcMicros
		detail.Change24h = price.Change24h
		markMicros = &price.PriceUsdcMicros
	}

	status := h.marketStatus()
	detail.Market = status
	detail.MarketSession = status.Session
	detail.AfterHours = status.AfterHours

	// The liquidity probes, the two Pyth feeds and the two chart ranges are all
	// independent reads. Run them together so the screen costs one round trip's
	// latency instead of four.
	sectionCtx, cancel := context.WithTimeout(ctx, marketSectionTimeout)
	defer cancel()

	var (
		quotes    pyth.ReferenceQuotes
		hasQuotes bool
		day       pyth.AssetChartSeries
		year      pyth.AssetChartSeries
		wg        sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		quotes, hasQuotes = h.referenceQuotes(sectionCtx, asset, markMicros)
	}()
	go func() {
		defer wg.Done()
		day = h.chartSeries(sectionCtx, asset.Symbol, pyth.ChartRange1D)
	}()
	go func() {
		defer wg.Done()
		year = h.chartSeries(sectionCtx, asset.Symbol, pyth.ChartRange1Y)
	}()
	detail.Liquidity = h.liquiditySnippet(ctx, asset, markMicros)
	detail.Routable = detail.Liquidity.Routable
	wg.Wait()

	var confMicros *int64
	if hasQuotes {
		detail.StockVsToken = stockVsTokenResponseFor(quotes)
		if quotes.Equity.ConfUsdcMicros > 0 {
			conf := quotes.Equity.ConfUsdcMicros
			confMicros = &conf
		}
	}
	detail.Stats = assetStatsResponseFor(day, year, detail.Liquidity.SpreadBps, confMicros)
	return detail
}

// referenceQuotes fetches the stock-vs-token pair, substituting the on-chain price
// for the token leg when the symbol has no Pyth crypto feed. The substitution is
// labelled as Jupiter, so nothing ever reads as a Pyth price that is not one.
func (h *AssetsHandlers) referenceQuotes(ctx context.Context, asset xstocks.CatalogAsset, markMicros *int64) (pyth.ReferenceQuotes, bool) {
	if h.Quotes == nil {
		return pyth.ReferenceQuotes{}, false
	}
	quotes, err := h.Quotes.ReferenceQuotes(ctx, asset.Symbol)
	if err != nil {
		return pyth.ReferenceQuotes{}, false
	}
	if quotes.Token.Status == pyth.QuoteStatusUnavailable && markMicros != nil {
		quotes.Token = pyth.JupiterFallbackQuote(*markMicros, h.now())
	}
	// A card with nothing on either side is not a card.
	if !quotes.Equity.Priced() && !quotes.Token.Priced() {
		return pyth.ReferenceQuotes{}, false
	}
	return quotes, true
}

func (h *AssetsHandlers) chartSeries(ctx context.Context, symbol string, chartRange pyth.ChartRange) pyth.AssetChartSeries {
	if h.Pyth == nil {
		return pyth.AssetChartSeries{}
	}
	series, err := h.Pyth.ChartSeries(ctx, symbol, chartRange)
	if err != nil {
		return pyth.AssetChartSeries{}
	}
	return series
}

func assetStatsResponseFor(day, year pyth.AssetChartSeries, spreadBps *int, confMicros *int64) *assetStatsResponse {
	stats := pyth.SessionStats(day)
	week52High, week52Low := pyth.Week52Range(year)
	resp := &assetStatsResponse{
		OpenUsdcMicros:          stats.OpenUsdcMicros,
		HighUsdcMicros:          stats.HighUsdcMicros,
		LowUsdcMicros:           stats.LowUsdcMicros,
		PreviousCloseUsdcMicros: stats.PreviousCloseUsdcMicros,
		Week52HighUsdcMicros:    week52High,
		Week52LowUsdcMicros:     week52Low,
		SpreadBps:               spreadBps,
		ConfUsdcMicros:          confMicros,
	}
	if *resp == (assetStatsResponse{}) {
		// Nothing could be sourced. Omit the grid rather than ship an empty object
		// the app would have to special-case.
		return nil
	}
	return resp
}

func stockVsTokenResponseFor(quotes pyth.ReferenceQuotes) *stockVsTokenResponse {
	return &stockVsTokenResponse{
		Equity:     referenceQuoteResponseFor(quotes.Equity),
		Token:      referenceQuoteResponseFor(quotes.Token),
		PremiumBps: quotes.PremiumBps(),
		AsOf:       quotes.AsOf.Format(time.RFC3339),
	}
}

func referenceQuoteResponseFor(quote pyth.ReferenceQuote) referenceQuoteResponse {
	resp := referenceQuoteResponse{
		Source: string(quote.Source),
		Status: string(quote.Status),
		Reason: quote.Reason,
	}
	if quote.PriceUsdcMicros > 0 {
		price := quote.PriceUsdcMicros
		resp.PriceUsdcMicros = &price
	}
	if quote.ConfUsdcMicros > 0 {
		conf := quote.ConfUsdcMicros
		resp.ConfUsdcMicros = &conf
	}
	if !quote.PublishedAt.IsZero() {
		publishedAt := quote.PublishedAt.UTC().Format(time.RFC3339)
		resp.PublishedAt = &publishedAt
	}
	return resp
}

func (h *AssetsHandlers) liquiditySnippet(ctx context.Context, asset xstocks.CatalogAsset, markMicros *int64) assetLiquidityResponse {
	snippet := assetLiquidityResponse{
		Label:              "Via Jupiter",
		Routable:           asset.Routable,
		BuyProbeUsdcMicros: app.CatalogRoutabilityProbeMicros,
	}
	if h.Jupiter == nil || strings.TrimSpace(asset.SolanaMint) == "" {
		return snippet
	}

	// Both probes are independent reads; the buy one alone decides routability.
	var (
		buyQuote  jupiter.BuyQuote
		buyErr    error
		sellQuote jupiter.SellQuote
		sellErr   error
		wg        sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		buyQuote, buyErr = h.Jupiter.QuoteBuy(ctx, jupiter.QuoteBuyParams{
			Symbol:     asset.Symbol,
			OutputMint: asset.SolanaMint,
			USDCAmount: app.CatalogRoutabilityProbeMicros,
		})
	}()
	go func() {
		defer wg.Done()
		sellQuote, sellErr = h.Jupiter.QuoteSell(ctx, jupiter.QuoteSellParams{
			Symbol:    asset.Symbol,
			InputMint: asset.SolanaMint,
			Amount:    jupiter.XStockAtomicScale,
		})
	}()
	wg.Wait()

	// A probe that never got an answer is not an answer. Rate limits, timeouts and upstream
	// 5xx all surface as errors here, and telling a member a stock "can't be bought" on one
	// of those is a lie about their money — the catalog's own probe stands instead.
	if buyErr == nil {
		snippet.Routable = buyQuote.Routable
	}
	if buyErr == nil && buyQuote.Routable {
		snippet.BuyProbeOutAmount = buyQuote.OutAmount
		if markMicros != nil {
			snippet.SpreadBps = pyth.MidSpreadBps(*markMicros, buyQuote.OutAmount, app.CatalogRoutabilityProbeMicros, jupiter.XStockDecimals)
		}
	}
	if sellErr == nil && sellQuote.Routable {
		snippet.SellProbeInAmount = sellQuote.InAmount
		snippet.SellProbeOutAmount = sellQuote.OutAmount
	}

	return snippet
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
	case errors.Is(err, privy.ErrInvalidToken):
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
