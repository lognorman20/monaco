package httpapi

import (
	"context"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func (h *GroupHandlers) enrichPotRow(ctx context.Context, row app.GroupViewPotRow) groupViewPotRowResponse {
	out := groupViewPotRowResponse{
		Symbol:      row.Symbol,
		Units:       row.Units,
		MarkUsd:     row.MarkUsd,
		ValueUsd:    row.ValueUsd,
		DollarPnL:   row.DollarPnL,
		AfterHours:  row.AfterHours,
		TokenAmount:        row.TokenAmount,
		UiAmountMultiplier: row.UiAmountMultiplier,
		Issuer:             row.Issuer,
		IssuerName:         row.IssuerName,
	}
	if row.Symbol == "USDC" {
		return out
	}
	asset, ok := lookupCatalogAssetBySymbol(ctx, h.Catalog, row.Symbol)
	if !ok {
		out.AssetKind = string(xstocks.AssetKindStock)
		return out
	}
	out.TokenDecimals = asset.Decimals
	out.AssetKind = string(asset.Kind)
	if asset.Kind == xstocks.AssetKindPreIPO {
		falseVal := false
		out.AfterHours = &falseVal
	}
	if h.Price != nil {
		if prices, err := h.Price.Prices(ctx, []string{asset.SolanaMint}); err == nil {
			if price, ok := prices[asset.SolanaMint]; ok {
				fields := catalogJSONFields(asset, &price, 0)
				out.PremiumBps = fields.PremiumBps
			}
		}
	}
	return out
}
