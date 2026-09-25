package app

import (
	"math"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const proposalReferenceFreshness = 48 * time.Hour

func proposalPremiumBpsForStore(kind xstocks.AssetKind, price jupiter.TokenPrice) *int {
	if price.StockData == nil || !price.StockData.Fresh(proposalReferenceFreshness) {
		return nil
	}
	refMicros := usdFloatToUsdcMicros(price.StockData.Price)
	if refMicros <= 0 || price.PriceUsdcMicros <= 0 {
		return nil
	}
	_ = kind
	ratio := float64(price.PriceUsdcMicros) / float64(refMicros)
	bps := int(math.Round((ratio - 1) * 10_000))
	return &bps
}

func usdFloatToUsdcMicros(usd float64) int64 {
	if usd <= 0 || usd >= 1e9 {
		return 0
	}
	return int64(math.Round(usd * 1_000_000))
}
