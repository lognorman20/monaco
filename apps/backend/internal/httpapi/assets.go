package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
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
	Pyth    pyth.AssetPriceClient
	Jupiter jupiter.Client
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
}

type popularAssetsResponse struct {
	Assets []marketAssetResponse `json:"assets"`
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

type assetDetailResponse struct {
	Symbol          string                 `json:"symbol"`
	Name            string                 `json:"name"`
	SolanaMint      string                 `json:"solanaMint"`
	Routable        bool                   `json:"routable"`
	PriceUsdcMicros *int64                 `json:"priceUsdcMicros,omitempty"`
	Change24h       *string                `json:"change24h,omitempty"`
	Liquidity       assetLiquidityResponse `json:"liquidity"`
}

type assetChartResponse struct {
	Points      []pyth.ChartPoint `json:"points"`
	EmptyReason string            `json:"emptyReason,omitempty"`
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

	resp := popularAssetsResponse{Assets: h.enrichAssets(ctx, assets)}
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
		Points:      series.Points,
		EmptyReason: series.EmptyReason,
	}
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok", "symbol", symbol, "range", chartRange, "point_count", len(resp.Points))
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

func (h *AssetsHandlers) enrichAssets(ctx context.Context, assets []xstocks.CatalogAsset) []marketAssetResponse {
	out := make([]marketAssetResponse, 0, len(assets))
	for _, asset := range assets {
		out = append(out, h.enrichAsset(ctx, asset))
	}
	return out
}

func (h *AssetsHandlers) enrichAsset(ctx context.Context, asset xstocks.CatalogAsset) marketAssetResponse {
	resp := marketAssetResponse{
		Symbol:     asset.Symbol,
		Name:       asset.Name,
		SolanaMint: asset.SolanaMint,
		Routable:   asset.Routable,
	}
	if price, change, ok := h.assetPrice(ctx, asset); ok {
		resp.PriceUsdcMicros = &price
		resp.Change24h = change
	}
	return resp
}

func (h *AssetsHandlers) buildAssetDetail(ctx context.Context, asset xstocks.CatalogAsset) assetDetailResponse {
	detail := assetDetailResponse{
		Symbol:     asset.Symbol,
		Name:       asset.Name,
		SolanaMint: asset.SolanaMint,
		Routable:   asset.Routable,
		Liquidity:  h.liquiditySnippet(ctx, asset, nil),
	}
	if price, change, ok := h.assetPrice(ctx, asset); ok {
		detail.PriceUsdcMicros = &price
		detail.Change24h = change
		detail.Liquidity = h.liquiditySnippet(ctx, asset, &price)
	}
	// Live Jupiter probe wins over a stale catalog rank (429s cache as not routable).
	detail.Routable = detail.Liquidity.Routable
	return detail
}

func (h *AssetsHandlers) assetPrice(ctx context.Context, asset xstocks.CatalogAsset) (int64, *string, bool) {
	if h.Pyth != nil {
		mark, err := h.Pyth.AssetMark(ctx, asset.Symbol)
		if err == nil && mark.PriceUsdcMicros > 0 {
			return mark.PriceUsdcMicros, mark.Change24h, true
		}
	}
	if h.Jupiter == nil || strings.TrimSpace(asset.SolanaMint) == "" {
		return 0, nil, false
	}
	quote, err := h.Jupiter.QuoteBuy(ctx, jupiter.QuoteBuyParams{
		Symbol:     asset.Symbol,
		OutputMint: asset.SolanaMint,
		USDCAmount: app.CatalogRoutabilityProbeMicros,
	})
	if err != nil || !quote.Routable {
		return 0, nil, false
	}
	outRaw, err := strconv.ParseInt(strings.TrimSpace(quote.OutAmount), 10, 64)
	if err != nil || outRaw <= 0 {
		return 0, nil, false
	}
	priceMicros := (app.CatalogRoutabilityProbeMicros * jupiter.XStockAtomicScale) / outRaw
	if priceMicros <= 0 {
		return 0, nil, false
	}
	return priceMicros, nil, true
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

	buyQuote, err := h.Jupiter.QuoteBuy(ctx, jupiter.QuoteBuyParams{
		Symbol:     asset.Symbol,
		OutputMint: asset.SolanaMint,
		USDCAmount: app.CatalogRoutabilityProbeMicros,
	})
	if err == nil && buyQuote.Routable {
		snippet.Routable = true
		snippet.BuyProbeOutAmount = buyQuote.OutAmount
		if markMicros != nil {
			snippet.SpreadBps = pyth.MidSpreadBps(*markMicros, buyQuote.OutAmount, app.CatalogRoutabilityProbeMicros, jupiter.XStockDecimals)
		}
	} else {
		snippet.Routable = false
	}

	sellQuote, err := h.Jupiter.QuoteSell(ctx, jupiter.QuoteSellParams{
		Symbol:    asset.Symbol,
		InputMint: asset.SolanaMint,
		Amount:    jupiter.XStockAtomicScale,
	})
	if err == nil && sellQuote.Routable {
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
