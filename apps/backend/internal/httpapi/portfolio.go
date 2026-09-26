package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// PortfolioHandlers serves the member's portfolio across cabals and their money history.
type PortfolioHandlers struct {
	Portfolio *app.PortfolioService
}

type portfolioCabalResponse struct {
	GroupID    string  `json:"groupId"`
	Name       string  `json:"name"`
	Tint       string  `json:"tint"`
	PictureURL *string `json:"pictureUrl"`
	ValueUsd   string  `json:"valueUsd"`
	Quantity   string  `json:"quantity"`
	DollarPnL  string  `json:"dollarPnl"`
}

type portfolioHoldingResponse struct {
	Symbol        string                   `json:"symbol"`
	Name          string                   `json:"name"`
	Kind          string                   `json:"kind"`
	LogoURL       *string                  `json:"logoUrl"`
	ValueUsd      string                   `json:"valueUsd"`
	ShareOfTotal  string                   `json:"shareOfTotal"`
	DollarPnL     string                   `json:"dollarPnl"`
	PercentReturn *string                  `json:"percentReturn"`
	Cabals        []portfolioCabalResponse `json:"cabals"`
}

type portfolioResponse struct {
	TotalUsd          string                     `json:"totalUsd"`
	CashUsd           string                     `json:"cashUsd"`
	AccountBalanceUsd *string                    `json:"accountBalanceUsd"`
	DollarPnL         string                     `json:"dollarPnl"`
	PercentReturn     *string                    `json:"percentReturn"`
	Holdings          []portfolioHoldingResponse `json:"holdings"`
	UnvaluedCabals    int                        `json:"unvaluedCabals"`
}

type historyItemResponse struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Status        string  `json:"status"`
	GroupID       *string `json:"groupId"`
	GroupName     *string `json:"groupName"`
	Symbol        *string `json:"symbol"`
	Name          *string `json:"name"`
	AssetKind     *string `json:"assetKind"`
	AmountUsd     *string `json:"amountUsd"`
	Quantity      *string `json:"quantity"`
	At            string  `json:"at"`
	TransactionID *string `json:"transactionId"`
}

type historyPageResponse struct {
	Items      []historyItemResponse `json:"items"`
	NextCursor *string               `json:"nextCursor"`
}

// GetPortfolioHandler handles GET /v1/me/portfolio.
func (h *PortfolioHandlers) GetPortfolioHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/me/portfolio")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	result, err := h.Portfolio.GetPortfolio(ctx, token)
	if err != nil {
		writePortfolioError(ctx, log, w, err, "get_portfolio_failed")
		return
	}

	holdings := make([]portfolioHoldingResponse, 0, len(result.Holdings))
	for _, holding := range result.Holdings {
		cabals := make([]portfolioCabalResponse, 0, len(holding.Cabals))
		for _, cabal := range holding.Cabals {
			cabals = append(cabals, portfolioCabalResponse{
				GroupID:    cabal.GroupID,
				Name:       cabal.Name,
				Tint:       cabal.Tint,
				PictureURL: optionalString(cabal.PictureURL),
				ValueUsd:   app.FormatUsdDecimal(cabal.ValueMicros),
				Quantity:   cabal.Quantity,
				DollarPnL:  app.FormatSignedUsdDecimal(cabal.DollarPnLMicros),
			})
		}
		holdings = append(holdings, portfolioHoldingResponse{
			Symbol:        holding.Symbol,
			Name:          holding.Name,
			Kind:          holding.Kind,
			LogoURL:       optionalString(holding.LogoURL),
			ValueUsd:      app.FormatUsdDecimal(holding.ValueMicros),
			ShareOfTotal:  holding.ShareOfTotal,
			DollarPnL:     app.FormatSignedUsdDecimal(holding.DollarPnLMicros),
			PercentReturn: holding.PercentReturn,
			Cabals:        cabals,
		})
	}
	var accountBalance *string
	if result.AccountBalanceMicros != nil {
		formatted := app.FormatUsdDecimal(*result.AccountBalanceMicros)
		accountBalance = &formatted
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(portfolioResponse{
		TotalUsd:          app.FormatUsdDecimal(result.TotalMicros),
		CashUsd:           app.FormatUsdDecimal(result.CashMicros),
		AccountBalanceUsd: accountBalance,
		DollarPnL:         app.FormatSignedUsdDecimal(result.DollarPnLMicros),
		PercentReturn:     result.PercentReturn,
		Holdings:          holdings,
		UnvaluedCabals:    result.UnvaluedCabals,
	})
	logJSONOK(ctx, log, "ok", "holdings", len(holdings), "unvalued_cabals", result.UnvaluedCabals)
}

// ListHistoryHandler handles GET /v1/me/transactions.
func (h *PortfolioHandlers) ListHistoryHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/me/transactions")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	query := r.URL.Query()
	filter, err := app.ParseHistoryFilter(query.Get("type"))
	if err != nil {
		logJSONError(ctx, log, "invalid_type", w, http.StatusBadRequest, historyQueryMessage(err))
		return
	}
	limit, err := app.ParseHistoryLimit(query.Get("limit"))
	if err != nil {
		logJSONError(ctx, log, "invalid_limit", w, http.StatusBadRequest, historyQueryMessage(err))
		return
	}

	page, err := h.Portfolio.ListHistory(ctx, token, filter, query.Get("cursor"), limit)
	if err != nil {
		writePortfolioError(ctx, log, w, err, "list_history_failed")
		return
	}

	items := make([]historyItemResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, historyItemJSON(item))
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(historyPageResponse{
		Items:      items,
		NextCursor: optionalString(page.NextCursor),
	})
	logJSONOK(ctx, log, "ok", "filter", string(filter), "items", len(items), "has_more", page.NextCursor != "")
}

// historyCSVHeader is the export's column order. It is part of the contract: spreadsheets
// and scripts key on these names.
var historyCSVHeader = []string{"date", "kind", "status", "cabal", "stock", "amount_usd", "quantity", "id"}

// ExportHistoryCSVHandler handles GET /v1/me/transactions/export.csv. It takes the same
// `type` filter as the list and returns every row it keeps.
func (h *PortfolioHandlers) ExportHistoryCSVHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/me/transactions/export.csv")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	filter, err := app.ParseHistoryFilter(r.URL.Query().Get("type"))
	if err != nil {
		logJSONError(ctx, log, "invalid_type", w, http.StatusBadRequest, historyQueryMessage(err))
		return
	}

	items, truncated, err := h.Portfolio.ExportHistory(ctx, token, filter)
	if err != nil {
		writePortfolioError(ctx, log, w, err, "export_history_failed")
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="monaco-history.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	if truncated {
		// The newest rows are all there; the header says the oldest were cut.
		w.Header().Set("X-Monaco-History-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	writer := csv.NewWriter(w)
	_ = writer.Write(historyCSVHeader)
	for _, item := range items {
		_ = writer.Write(historyCSVRecord(item))
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		// Headers are gone; the log is the only place left to say so.
		log.done(ctx, "csv_write_failed", http.StatusOK, "err", err.Error(), "rows", len(items))
		return
	}
	logJSONOK(ctx, log, "ok", "filter", string(filter), "rows", len(items), "truncated", truncated)
}

func historyItemJSON(item app.HistoryItem) historyItemResponse {
	resp := historyItemResponse{
		ID:            item.ID,
		Kind:          item.Kind,
		Status:        item.Status,
		GroupID:       optionalString(item.GroupID),
		GroupName:     optionalString(item.GroupName),
		Symbol:        optionalString(item.Symbol),
		Name:          optionalString(item.Name),
		AssetKind:     optionalString(item.AssetKind),
		Quantity:      optionalString(item.Quantity),
		At:            item.At.UTC().Format(time.RFC3339),
		TransactionID: optionalString(item.TransactionID),
	}
	if item.AmountMicros != nil {
		amount := app.FormatUsdDecimal(*item.AmountMicros)
		resp.AmountUsd = &amount
	}
	return resp
}

func historyCSVRecord(item app.HistoryItem) []string {
	amount := ""
	if item.AmountMicros != nil {
		amount = app.FormatUsdDecimal(*item.AmountMicros)
	}
	return []string{
		item.At.UTC().Format(time.RFC3339),
		item.Kind,
		item.Status,
		csvText(item.GroupName),
		csvText(item.Symbol),
		amount,
		item.Quantity,
		item.ID,
	}
}

// csvText neutralises a member-typed field a spreadsheet would run as a formula. A cabal named
// "=HYPERLINK(...)" is text in Monaco and must stay text in Excel.
func csvText(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}

func historyQueryMessage(err error) string {
	message := err.Error()
	if idx := strings.Index(message, ": "); idx >= 0 && errors.Is(err, app.ErrInvalidHistoryQuery) {
		return message[idx+2:]
	}
	return message
}

func writePortfolioError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, branch string) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
	case errors.Is(err, app.ErrInvalidHistoryQuery):
		logJSONError(ctx, log, "invalid_query", w, http.StatusBadRequest, historyQueryMessage(err))
	case errors.Is(err, context.Canceled):
		logJSONError(ctx, log, "canceled", w, http.StatusRequestTimeout, "request canceled")
	default:
		logJSONError(ctx, log, branch, w, http.StatusInternalServerError, "internal server error", "err", err.Error())
	}
}
