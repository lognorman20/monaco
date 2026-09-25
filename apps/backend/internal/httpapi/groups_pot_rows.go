package httpapi

import (
	"context"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
)

// potRowResponses renders a cabal's holdings, with the market's own figures for
// each stock alongside the cabal's.
//
// A holdings row and a Stocks-tab row are the same instrument, so they now read
// the same way: same day change, same day series, from the same caches. The
// market read is decoration — a nil `Market` (or a slow one) simply leaves those
// fields out, and the pot, which is what this route is for, is untouched.
//
// Cash is skipped: it has no day change, and asking the catalogue for "USDC"
// would be a lookup that can only fail.
func (h *GroupHandlers) potRowResponses(ctx context.Context, rows []app.GroupViewPotRow) []groupViewPotRowResponse {
	symbols := make([]string, 0, len(rows))
	for _, row := range rows {
		symbol := strings.TrimSpace(row.Symbol)
		if symbol == "" || strings.EqualFold(symbol, "USDC") {
			continue
		}
		symbols = append(symbols, symbol)
	}

	var market map[string]marketAssetResponse
	if h.Market != nil && len(symbols) > 0 {
		market = h.Market.RowsForSymbols(ctx, symbols)
	}

	out := make([]groupViewPotRowResponse, 0, len(rows))
	for _, row := range rows {
		resp := h.enrichPotRow(ctx, row)
		if decorated, ok := market[strings.ToUpper(strings.TrimSpace(row.Symbol))]; ok {
			resp.Change24h = decorated.Change24h
			resp.Spark = decorated.Spark
			resp.SparkBasis = decorated.SparkBasis
			resp.SparkBasisSymbol = decorated.SparkBasisSymbol
			resp.ChangeBasis = decorated.ChangeBasis
			resp.LogoURL = decorated.LogoURL
		}
		out = append(out, resp)
	}
	return out
}
