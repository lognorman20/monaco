package dex

import (
	"context"
	"math/big"
)

// Client quotes and builds swaps on Base.
type Client interface {
	QuoteBuy(ctx context.Context, tokenOut string, usdcIn *big.Int) (Quote, error)
	QuoteSell(ctx context.Context, tokenIn string, amountIn *big.Int) (Quote, error)
	BuildSwap(ctx context.Context, q Quote, sender, recipient string) (SwapCall, error)
}
