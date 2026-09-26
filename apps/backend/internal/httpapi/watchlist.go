package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// Body caps. A reorder names at most app.WatchlistMaxSymbols symbols of at most 32 bytes;
// an alert is three short fields.
const (
	maxWatchlistRequestBytes = 8 << 10
	maxAlertRequestBytes     = 2 << 10
)

// WatchlistHandlers serves the caller's watchlist and price alerts.
type WatchlistHandlers struct {
	Watchlist *app.WatchlistService
	// Assets prices the rows the way the Stocks tab does: one MarketRowSource, one market
	// session. Nil ships rows with the symbol alone.
	Assets *AssetsHandlers
}

// watchlistResponse is GET /v1/me/watchlist: market rows, in the member's order, in the same
// shape as GET /v1/assets/popular.
type watchlistResponse struct {
	Assets []marketAssetResponse `json:"assets"`
	Market *marketStatusResponse `json:"market,omitempty"`
}

type watchlistEntryResponse struct {
	Symbol    string `json:"symbol"`
	Position  int    `json:"position"`
	CreatedAt string `json:"createdAt"`
}

type reorderWatchlistRequest struct {
	Symbols []string `json:"symbols"`
}

type reorderWatchlistResponse struct {
	Symbols []string `json:"symbols"`
}

type createPriceAlertRequest struct {
	Symbol          string `json:"symbol"`
	Direction       string `json:"direction"`
	PriceUsdcMicros int64  `json:"priceUsdcMicros"`
}

type priceAlertResponse struct {
	ID        string `json:"id"`
	Symbol    string `json:"symbol"`
	Direction string `json:"direction"`
	// PriceUsdcMicros is the line the member set.
	PriceUsdcMicros int64  `json:"priceUsdcMicros"`
	Active          bool   `json:"active"`
	CreatedAt       string `json:"createdAt"`
	// TriggeredAt and TriggeredPriceUsdcMicros say when the alert fired and on what mark.
	TriggeredAt              *string `json:"triggeredAt,omitempty"`
	TriggeredPriceUsdcMicros *int64  `json:"triggeredPriceUsdcMicros,omitempty"`
}

// priceAlertsResponse is GET /v1/me/alerts: the alerts, plus one market row per stock they
// name so each group can show its stock's mark without a second read.
type priceAlertsResponse struct {
	Alerts []priceAlertResponse  `json:"alerts"`
	Assets []marketAssetResponse `json:"assets"`
	Market *marketStatusResponse `json:"market,omitempty"`
}

// GetWatchlistHandler handles GET /v1/me/watchlist.
func (h *WatchlistHandlers) GetWatchlistHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/me/watchlist")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	entries, err := h.Watchlist.Watchlist(ctx, token)
	if err != nil {
		writeWatchlistError(ctx, log, w, err)
		return
	}
	symbols := make([]string, 0, len(entries))
	for _, entry := range entries {
		symbols = append(symbols, entry.Symbol)
	}
	resp := watchlistResponse{Assets: h.rowsInOrder(ctx, symbols), Market: h.marketStatus()}
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok", "count", len(resp.Assets))
}

// AddToWatchlistHandler handles PUT /v1/me/watchlist/{symbol}: 201 when added, 200 when the
// stock was already there (it keeps its place).
func (h *WatchlistHandlers) AddToWatchlistHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "PUT /v1/me/watchlist/{symbol}")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	symbol := strings.TrimSpace(r.PathValue("symbol"))
	entry, created, err := h.Watchlist.AddToWatchlist(ctx, token, symbol)
	if err != nil {
		writeWatchlistError(ctx, log, w, err, "symbol", symbol)
		return
	}
	status, branch := http.StatusOK, "already_watching"
	if created {
		status, branch = http.StatusCreated, "added"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(watchlistEntryResponse{
		Symbol:    entry.Symbol,
		Position:  entry.Position,
		CreatedAt: entry.CreatedAt.UTC().Format(time.RFC3339),
	})
	log.done(ctx, branch, status, "symbol", entry.Symbol, "position", entry.Position)
}

// RemoveFromWatchlistHandler handles DELETE /v1/me/watchlist/{symbol}. It answers 204 whether
// or not the stock was on the list: either way it is not now.
func (h *WatchlistHandlers) RemoveFromWatchlistHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "DELETE /v1/me/watchlist/{symbol}")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	symbol := strings.TrimSpace(r.PathValue("symbol"))
	removed, err := h.Watchlist.RemoveFromWatchlist(ctx, token, symbol)
	if err != nil {
		writeWatchlistError(ctx, log, w, err, "symbol", symbol)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logNoContent(ctx, log, "removed", "symbol", symbol, "was_watching", removed)
}

// ReorderWatchlistHandler handles PUT /v1/me/watchlist with { "symbols": [...] }.
func (h *WatchlistHandlers) ReorderWatchlistHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "PUT /v1/me/watchlist")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWatchlistRequestBytes)
	var req reorderWatchlistRequest
	if !decodeJSONBody(ctx, log, w, r, &req) {
		return
	}
	if req.Symbols == nil {
		logJSONError(ctx, log, "missing_symbols", w, http.StatusBadRequest, "symbols is required")
		return
	}
	entries, err := h.Watchlist.ReorderWatchlist(ctx, token, req.Symbols)
	if err != nil {
		writeWatchlistError(ctx, log, w, err, "count", len(req.Symbols))
		return
	}
	resp := reorderWatchlistResponse{Symbols: make([]string, 0, len(entries))}
	for _, entry := range entries {
		resp.Symbols = append(resp.Symbols, entry.Symbol)
	}
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "reordered", "count", len(resp.Symbols))
}

// ListPriceAlertsHandler handles GET /v1/me/alerts, optionally ?symbol= for one stock.
func (h *WatchlistHandlers) ListPriceAlertsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/me/alerts")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	alerts, err := h.Watchlist.PriceAlerts(ctx, token, symbol)
	if err != nil {
		writeWatchlistError(ctx, log, w, err, "symbol", symbol)
		return
	}
	resp := priceAlertsResponse{
		Alerts: make([]priceAlertResponse, 0, len(alerts)),
		Market: h.marketStatus(),
	}
	symbols := []string{}
	seen := map[string]bool{}
	for _, alert := range alerts {
		resp.Alerts = append(resp.Alerts, priceAlertToResponse(alert))
		if key := strings.ToUpper(alert.Symbol); !seen[key] {
			seen[key] = true
			symbols = append(symbols, alert.Symbol)
		}
	}
	resp.Assets = h.rowsInOrder(ctx, symbols)
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok", "count", len(resp.Alerts), "symbol", symbol)
}

// CreatePriceAlertHandler handles POST /v1/me/alerts.
func (h *WatchlistHandlers) CreatePriceAlertHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/me/alerts")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAlertRequestBytes)
	var req createPriceAlertRequest
	if !decodeJSONBody(ctx, log, w, r, &req) {
		return
	}
	alert, err := h.Watchlist.CreatePriceAlert(ctx, token, app.CreatePriceAlertInput{
		Symbol:          req.Symbol,
		Direction:       req.Direction,
		PriceUsdcMicros: req.PriceUsdcMicros,
	})
	if err != nil {
		writeWatchlistError(ctx, log, w, err, "symbol", strings.TrimSpace(req.Symbol), "direction", req.Direction)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(priceAlertToResponse(alert))
	log.done(ctx, "created", http.StatusCreated,
		"alert_id", alert.ID, "symbol", alert.Symbol, "direction", string(alert.Direction), "price_usdc_micros", alert.PriceUsdcMicros)
}

// DeletePriceAlertHandler handles DELETE /v1/me/alerts/{id}.
func (h *WatchlistHandlers) DeletePriceAlertHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "DELETE /v1/me/alerts/{id}")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	alertID := strings.TrimSpace(r.PathValue("id"))
	if err := h.Watchlist.DeletePriceAlert(ctx, token, alertID); err != nil {
		writeWatchlistError(ctx, log, w, err, "alert_id", alertID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logNoContent(ctx, log, "deleted", "alert_id", alertID)
}

// rowsInOrder decorates symbols as market rows and returns them in the order given. A symbol
// the catalogue could not resolve still ships as a row carrying its symbol, so a stock never
// drops off the member's own list because the catalogue was slow.
func (h *WatchlistHandlers) rowsInOrder(ctx context.Context, symbols []string) []marketAssetResponse {
	out := make([]marketAssetResponse, 0, len(symbols))
	if len(symbols) == 0 {
		return out
	}
	var rows map[string]marketAssetResponse
	if h.Assets != nil {
		rows = h.Assets.marketRows().RowsForSymbols(ctx, symbols)
	}
	for _, symbol := range symbols {
		row, ok := rows[strings.ToUpper(strings.TrimSpace(symbol))]
		if !ok {
			row = marketAssetResponse{Symbol: symbol, Name: symbol}
		}
		out = append(out, row)
	}
	return out
}

func (h *WatchlistHandlers) marketStatus() *marketStatusResponse {
	if h.Assets == nil {
		return nil
	}
	return h.Assets.marketStatus()
}

// applyWatchState answers the stock screen's watching and alertCount on the detail response.
// A failed read leaves both off rather than failing a screen that is mostly market data.
func (h *AssetsHandlers) applyWatchState(ctx context.Context, detail *assetDetailResponse, userID string) {
	if h.Watchlist == nil || userID == "" || detail == nil {
		return
	}
	watching, alerts, err := h.Watchlist.AssetWatchState(ctx, userID, detail.Symbol)
	if err != nil {
		slog.WarnContext(ctx, "asset watch state unavailable", "symbol", detail.Symbol, "err", err)
		return
	}
	detail.Watching = &watching
	detail.AlertCount = &alerts
}

func priceAlertToResponse(alert app.PriceAlert) priceAlertResponse {
	resp := priceAlertResponse{
		ID:              alert.ID,
		Symbol:          alert.Symbol,
		Direction:       string(alert.Direction),
		PriceUsdcMicros: alert.PriceUsdcMicros,
		Active:          alert.Active,
		CreatedAt:       alert.CreatedAt.UTC().Format(time.RFC3339),
	}
	if alert.TriggeredAt != nil {
		at := alert.TriggeredAt.UTC().Format(time.RFC3339)
		resp.TriggeredAt = &at
	}
	if alert.TriggeredPriceUsdcMicros != nil {
		price := *alert.TriggeredPriceUsdcMicros
		resp.TriggeredPriceUsdcMicros = &price
	}
	return resp
}

// writeWatchlistError maps watchlist and alert errors. Messages are for the member; reasons
// are for the app to branch on.
func writeWatchlistError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	var alreadyMet *app.AlertAlreadyMetError
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", attrs...)
	case errors.Is(err, app.ErrWatchlistUnknownSymbol):
		logJSONError(ctx, log, "symbol_not_found", w, http.StatusNotFound, "stock not found", attrs...)
	case errors.Is(err, app.ErrWatchlistCatalogUnavailable):
		logJSONError(ctx, log, "catalog_unavailable", w, http.StatusServiceUnavailable, "could not check that stock right now, try again", append(attrs, "err", err.Error())...)
	case errors.Is(err, app.ErrWatchlistFull):
		logJSONErrorWithReason(ctx, log, "watchlist_full", w, http.StatusConflict, "your watchlist is full, remove a stock first", "watchlist_full", attrs...)
	case errors.Is(err, app.ErrWatchlistOrderInvalid):
		logJSONError(ctx, log, "invalid_order", w, http.StatusBadRequest, "symbols must name each stock once", attrs...)
	case errors.Is(err, app.ErrWatchlistOrderStale):
		logJSONErrorWithReason(ctx, log, "watchlist_changed", w, http.StatusConflict, "your watchlist changed, reload it and try again", "watchlist_changed", attrs...)
	case errors.Is(err, app.ErrAlertDirectionInvalid):
		logJSONError(ctx, log, "invalid_direction", w, http.StatusBadRequest, "direction must be above or below", attrs...)
	case errors.Is(err, app.ErrAlertPriceInvalid):
		logJSONError(ctx, log, "invalid_price", w, http.StatusBadRequest, "priceUsdcMicros must be above zero and at most $1,000,000", attrs...)
	case errors.Is(err, app.ErrAlertLimitReached):
		logJSONErrorWithReason(ctx, log, "alert_limit", w, http.StatusConflict, "you have 20 alerts waiting, remove one first", "alert_limit", attrs...)
	case errors.As(err, &alreadyMet):
		logJSONErrorWithReason(ctx, log, "alert_already_met", w, http.StatusUnprocessableEntity, alreadyMet.Error(), "alert_already_met",
			append(attrs, "mark_usdc_micros", alreadyMet.MarkUsdcMicros)...)
	case errors.Is(err, app.ErrAlertNotFound):
		logJSONError(ctx, log, "alert_not_found", w, http.StatusNotFound, "alert not found", attrs...)
	default:
		logJSONError(ctx, log, "watchlist_failed", w, http.StatusInternalServerError, "internal server error", append(attrs, "err", err.Error())...)
	}
}
