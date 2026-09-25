package httpapi

import (
	"context"
	"math/big"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func uiAmountMultiplierForMint(ctx context.Context, catalog xstocks.CatalogSearcher, mint string) string {
	if catalog == nil {
		return ""
	}
	mint = strings.TrimSpace(mint)
	if mint == "" {
		return ""
	}
	asset, ok, err := catalog.LookupByMint(ctx, mint)
	if err != nil || !ok {
		return ""
	}
	return uiAmountMultiplierForAsset(asset)
}

func uiAmountMultiplierForAsset(asset xstocks.CatalogAsset) string {
	n := asset.Normalize()
	if n.Kind != xstocks.AssetKindPreIPO && n.UiAmountMultiplier == nil {
		return ""
	}
	mult := n.UiAmountMultiplier
	if mult == nil {
		mult = big.NewRat(1, 1)
	}
	return pyth.UiMultiplierDecimalString(mult)
}
