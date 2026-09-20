package app

import (
	"math/big"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/dex"
)

func registerTestB20Asset(catalog b20.Catalog, symbol, tokenAddress string) {
	b20.RegisterAsset(catalog, b20.Asset{
		Symbol:       symbol,
		Name:         symbol,
		TokenAddress: tokenAddress,
		Decimals:     8,
	})
}

func registerDexSellQuote(t *testing.T, client dex.Client, tokenIn string, amountIn, usdcOut int64) {
	t.Helper()
	dex.RegisterQuote(client, dex.Quote{
		TokenIn:   tokenIn,
		TokenOut:  dex.USDCAddress(),
		AmountIn:  big.NewInt(amountIn),
		AmountOut: big.NewInt(usdcOut),
		Routable:  true,
	})
}
