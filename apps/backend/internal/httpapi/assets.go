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

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// AssetsHandlers serves group-agnostic market catalog routes.
type AssetsHandlers struct {
	Store   *postgres.Store
	Auth    auth.Verifier
	Wallets wallets.Client
	Catalog b20.Catalog
	Pyth    pyth.AssetPriceClient
	Dex     dex.Client
}

type marketAssetResponse struct {
	Symbol          string  `json:"symbol"`
	Name            string  `json:"name"`
	TokenAddress    string  `json:"tokenAddress"`
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
	TokenAddress    string                 `json:"tokenAddress"`
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
	assets, err := h.Catalog.Popular(ctx)
	if err == nil && len(assets) > limit {
		assets = assets[:limit]
	}
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

	chartRange, err := parsePythChartRange(r.URL.Query().Get("range"))
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

func (h *AssetsHandlers) lookupAsset(ctx context.Context, symbol string) (b20.Asset, bool, error) {
	page, err := h.Catalog.Search(ctx, symbol, 5, 0)
	if err != nil {
		return b20.Asset{}, false, err
	}
	needle := strings.ToUpper(strings.TrimSpace(symbol))
	for _, asset := range page.Assets {
		if strings.EqualFold(strings.TrimSpace(asset.Symbol), needle) {
			return asset, true, nil
		}
	}
	return b20.Asset{}, false, nil
}

// enrichAssets marks every asset with one batched Jupiter Price API call instead
// of a per-asset round trip (Pyth or Jupiter QuoteBuy). Shared by the list and
// popular routes — both just display current price, so both get the same source.
func (h *AssetsHandlers) enrichAssets(ctx context.Context, assets []b20.Asset) []marketAssetResponse {
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
type assetPriceSnapshot struct {
	PriceUsdcMicros int64
	Change24h       *string
}

func (h *AssetsHandlers) fetchPrices(ctx context.Context, assets []b20.Asset) map[string]assetPriceSnapshot {
	if h.Pyth == nil {
		return nil
	}
	out := make(map[string]assetPriceSnapshot, len(assets))
	for _, asset := range assets {
		mark, err := h.Pyth.AssetMark(ctx, asset.Symbol)
		if err != nil || mark.PriceUsdcMicros <= 0 {
			continue
		}
		out[asset.TokenAddress] = assetPriceSnapshot{
			PriceUsdcMicros: mark.PriceUsdcMicros,
			Change24h:       mark.Change24h,
		}
	}
	return out
}

func marketAssetResponseFor(asset b20.Asset, prices map[string]assetPriceSnapshot) marketAssetResponse {
	resp := marketAssetResponse{
		Symbol:       asset.Symbol,
		Name:         asset.Name,
		TokenAddress: asset.TokenAddress,
		Routable:     asset.Routable,
	}
	if price, ok := prices[asset.TokenAddress]; ok && price.PriceUsdcMicros > 0 {
		resp.PriceUsdcMicros = &price.PriceUsdcMicros
		resp.Change24h = price.Change24h
	}
	return resp
}

func (h *AssetsHandlers) buildAssetDetail(ctx context.Context, asset b20.Asset) assetDetailResponse {
	detail := assetDetailResponse{
		Symbol:       asset.Symbol,
		Name:         asset.Name,
		TokenAddress: asset.TokenAddress,
		Liquidity:    h.liquiditySnippet(ctx, asset, nil),
	}
	prices := h.fetchPrices(ctx, []b20.Asset{asset})
	if price, ok := prices[asset.TokenAddress]; ok && price.PriceUsdcMicros > 0 {
		detail.PriceUsdcMicros = &price.PriceUsdcMicros
		detail.Change24h = price.Change24h
		detail.Liquidity = h.liquiditySnippet(ctx, asset, &price.PriceUsdcMicros)
	}
	// Live Jupiter probe wins over a stale catalog rank (429s cache as not routable).
	detail.Routable = detail.Liquidity.Routable
	return detail
}

func (h *AssetsHandlers) liquiditySnippet(ctx context.Context, asset b20.Asset, markMicros *int64) assetLiquidityResponse {
	snippet := assetLiquidityResponse{
		Label:              "Via DEX",
		Routable:           false,
		BuyProbeUsdcMicros: app.CatalogRoutabilityProbeMicros,
	}
	if h.Dex == nil || strings.TrimSpace(asset.TokenAddress) == "" {
		return snippet
	}

	buyQuote, err := h.Dex.QuoteBuy(ctx, asset.TokenAddress, big.NewInt(app.CatalogRoutabilityProbeMicros))
	if err == nil && buyQuote.Routable && buyQuote.AmountOut != nil {
		snippet.Routable = true
		snippet.BuyProbeOutAmount = buyQuote.AmountOut.String()
	} else {
		snippet.Routable = false
	}

	sellQuote, err := h.Dex.QuoteSell(ctx, asset.TokenAddress, big.NewInt(b20.TokenAtomicScale))
	if err == nil && sellQuote.Routable && sellQuote.AmountOut != nil {
		snippet.SellProbeInAmount = strconv.FormatInt(b20.TokenAtomicScale, 10)
		snippet.SellProbeOutAmount = sellQuote.AmountOut.String()
	}
	_ = markMicros

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
	case errors.Is(err, auth.ErrUnauthorized):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
	default:
		logJSONError(ctx, log, "internal_error", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
	}
}

func parsePythChartRange(raw string) (pyth.ChartRange, error) {
	return pyth.ParseChartRange(raw)
}

func writeMarketJSON(ctx context.Context, log *requestLog, w http.ResponseWriter, status int, payload any, outcome string, attrs ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
	logJSONOK(ctx, log, outcome, attrs...)
}
