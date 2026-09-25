package app

import (
	"context"

	"github.com/monaco/monaco/apps/backend/internal/catalog"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
)

// redeemSellSlippageBufferBps is the slippage buffer added when sizing a redeem sell.
// While Jupiter nets transfer fees in quotes, the buffer stays at slippage-only.
func redeemSellSlippageBufferBps(ctx context.Context, symbols *SymbolResolver, reader mintinfo.Reader, mint string) int64 {
	buffer := int64(jupiter.RedeemSellSlippageBufferBps)
	if catalog.JupiterNetsTransferFee {
		return buffer
	}
	feeBps := int64(0)
	if symbols != nil && symbols.catalog != nil {
		if asset, found, err := symbols.catalog.LookupByMint(ctx, mint); err == nil && found {
			feeBps = int64(transferFeeBpsAtExecute(ctx, reader, asset.Normalize()))
		}
	}
	return buffer + feeBps
}
