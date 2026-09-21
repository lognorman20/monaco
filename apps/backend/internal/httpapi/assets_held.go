package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
)

type heldAssetCabalResponse struct {
	GroupID   string `json:"groupId"`
	Name      string `json:"name"`
	Units     string `json:"units"`
	ValueUsd  string `json:"valueUsd"`
	DollarPnL string `json:"dollarPnl"`
	// MySliceUsd is the caller's own share of this cabal's position.
	MySliceUsd string `json:"mySliceUsd"`
}

type heldAssetResponse struct {
	// Asset is the same row shape the list routes serve, so the app renders it
	// with the same row view: mark, sparkline and day move included.
	Asset          marketAssetResponse      `json:"asset"`
	Cabals         []heldAssetCabalResponse `json:"cabals"`
	TotalValueUsd  string                   `json:"totalValueUsd"`
	TotalDollarPnL string                   `json:"totalDollarPnl"`
	MySliceUsd     string                   `json:"mySliceUsd"`
}

type votableAssetResponse struct {
	Asset         marketAssetResponse `json:"asset"`
	OpenProposals int                 `json:"openProposals"`
	CabalNames    []string            `json:"cabalNames"`
	// SoonestExpiresAt is RFC3339 in UTC; the app converts for display.
	SoonestExpiresAt string `json:"soonestExpiresAt,omitempty"`
}

type heldAssetsResponse struct {
	Held      []heldAssetResponse    `json:"held"`
	UpForVote []votableAssetResponse `json:"upForVote"`
	Market    *marketStatusResponse  `json:"market,omitempty"`
}

// HeldAssetsHandler handles GET /v1/assets/held: what the caller's cabals own,
// and what they have open votes on.
//
// Both sections come from one route because they come from one scan of the same
// cabals, and because the Stocks tab wants them together. The response reuses the
// list row shape rather than inventing a second one: it is the same stock, and it
// should be the same row.
func (h *AssetsHandlers) HeldAssetsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/assets/held")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if h.Home == nil {
		logJSONError(ctx, log, "held_unavailable", w, http.StatusServiceUnavailable, "held assets unavailable")
		return
	}

	result, err := h.Home.GetHeldAssets(ctx, token)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUnauthorized):
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
		case errors.Is(err, app.ErrUserNotFound):
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
		default:
			logJSONError(ctx, log, "held_assets_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		}
		return
	}

	resp := h.buildHeldAssetsResponse(ctx, result)
	writeMarketJSON(ctx, log, w, http.StatusOK, resp, "ok",
		"held_count", len(resp.Held),
		"vote_count", len(resp.UpForVote),
	)
}

func (h *AssetsHandlers) buildHeldAssetsResponse(ctx context.Context, result app.HeldAssetsResult) heldAssetsResponse {
	symbols := make([]string, 0, len(result.Held)+len(result.UpForVote))
	for _, held := range result.Held {
		symbols = append(symbols, held.Symbol)
	}
	for _, votable := range result.UpForVote {
		symbols = append(symbols, votable.Symbol)
	}
	rows := h.marketRows().RowsForSymbols(ctx, symbols)

	resp := heldAssetsResponse{
		Held:      make([]heldAssetResponse, 0, len(result.Held)),
		UpForVote: make([]votableAssetResponse, 0, len(result.UpForVote)),
		Market:    h.marketStatus(),
	}
	for _, held := range result.Held {
		cabals := make([]heldAssetCabalResponse, 0, len(held.Cabals))
		for _, cabal := range held.Cabals {
			cabals = append(cabals, heldAssetCabalResponse{
				GroupID:    cabal.GroupID,
				Name:       cabal.Name,
				Units:      cabal.Units,
				ValueUsd:   cabal.ValueUsd,
				DollarPnL:  cabal.DollarPnL,
				MySliceUsd: cabal.MySliceUsd,
			})
		}
		resp.Held = append(resp.Held, heldAssetResponse{
			Asset:          rowFor(rows, held.Symbol),
			Cabals:         cabals,
			TotalValueUsd:  held.TotalValueUsd,
			TotalDollarPnL: held.TotalDollarPnL,
			MySliceUsd:     held.MySliceUsd,
		})
	}
	for _, votable := range result.UpForVote {
		names := votable.CabalNames
		if names == nil {
			// An absent array and a JSON null read differently to a decoder; always
			// ship a list.
			names = []string{}
		}
		row := votableAssetResponse{
			Asset:         rowFor(rows, votable.Symbol),
			OpenProposals: votable.OpenProposals,
			CabalNames:    names,
		}
		if votable.SoonestExpiresAt != nil {
			row.SoonestExpiresAt = votable.SoonestExpiresAt.UTC().Format(time.RFC3339)
		}
		resp.UpForVote = append(resp.UpForVote, row)
	}
	return resp
}

// rowFor is the decorated row for a symbol, or a bare one naming the symbol when
// the market side has nothing for it: a held position never ships an empty row.
func rowFor(rows map[string]marketAssetResponse, symbol string) marketAssetResponse {
	if row, ok := rows[strings.ToUpper(strings.TrimSpace(symbol))]; ok {
		return row
	}
	return marketAssetResponse{Symbol: symbol, Name: symbol}
}
