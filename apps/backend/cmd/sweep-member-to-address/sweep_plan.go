package main

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// sweepDustMinUSDCMicros skips a token sell whose Jupiter mark is under $0.50.
const sweepDustMinUSDCMicros int64 = 500_000

type sweepSellPlan struct {
	Decimals        int
	Kind            xstocks.AssetKind
	SlippageBps     int
	SkipDust        bool
	WholeTokens     string
	ValueUSDCMicros int64
}

func resolveSweepSell(ctx context.Context, lookup xstocks.MintCatalog, prices jupiter.PriceClient, mint string, amount int64) (sweepSellPlan, error) {
	var asset xstocks.CatalogAsset
	found := false
	if lookup != nil {
		row, ok, err := lookup.LookupByMint(ctx, mint)
		if err != nil {
			return sweepSellPlan{}, err
		}
		if ok {
			asset = row.Normalize()
			found = true
		}
	}

	var mark jupiter.TokenPrice
	priceOK := false
	if prices != nil {
		batch, err := prices.Prices(ctx, []string{mint})
		if err == nil {
			if row, ok := batch[mint]; ok && row.PriceUsdcMicros > 0 {
				mark = row
				priceOK = true
			}
		}
	}

	decimals := jupiter.XStockDecimals
	kind := xstocks.AssetKindStock
	var uiMult *big.Rat
	switch {
	case found:
		decimals = asset.Decimals
		kind = asset.Kind
		uiMult = asset.UiAmountMultiplier
	case priceOK && mark.Decimals > 0:
		decimals = mark.Decimals
	}

	if uiMult == nil {
		uiMult = big.NewRat(1, 1)
	}

	slippage := 0
	if kind == xstocks.AssetKindPreIPO {
		slippage = jupiter.PreIPOSlippageBps
	}

	value, err := tokenValueUSDCMicros(amount, mark.PriceUsdcMicros, decimals, uiMult, kind)
	if err != nil {
		return sweepSellPlan{}, err
	}
	whole, err := formatScaledWholeTokens(amount, decimals, uiMult, kind)
	if err != nil {
		return sweepSellPlan{}, err
	}
	return sweepSellPlan{
		Decimals:        decimals,
		Kind:            kind,
		SlippageBps:     slippage,
		SkipDust:        priceOK && value < sweepDustMinUSDCMicros,
		WholeTokens:     whole,
		ValueUSDCMicros: value,
	}, nil
}

func tokenValueUSDCMicros(amount, priceMicros int64, decimals int, uiMultiplier *big.Rat, kind xstocks.AssetKind) (int64, error) {
	return pyth.HoldingValueUSDCMicros(amount, priceMicros, decimals, uiMultiplier, kind)
}

func formatScaledWholeTokens(amount int64, decimals int, uiMultiplier *big.Rat, kind xstocks.AssetKind) (string, error) {
	units, err := pyth.TokenAtomicsToScaledDecimalUnits(amount, decimals, uiMultiplier, kind)
	if err != nil {
		return "", err
	}
	return string(units), nil
}

func formatWholeTokens(amount int64, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}
	scale := jupiter.AtomicScale(decimals)
	if scale <= 0 {
		return fmt.Sprintf("%d", amount)
	}
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	whole := amount / scale
	frac := amount % scale
	if frac == 0 {
		return fmt.Sprintf("%s%d", sign, whole)
	}
	fracText := fmt.Sprintf("%0*d", decimals, frac)
	fracText = strings.TrimRight(fracText, "0")
	return fmt.Sprintf("%s%d.%s", sign, whole, fracText)
}
