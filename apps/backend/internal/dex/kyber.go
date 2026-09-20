package dex

import (
	"context"
	"errors"
	"math/big"
)

// ErrNotConfigured means the Kyber client is not wired yet.
var ErrNotConfigured = errors.New("kyber dex client not configured")

type kyberClient struct{}

// NewKyberClient is a stub until M6-T5.
func NewKyberClient(httpClient interface{}, clientID string) Client {
	_ = httpClient
	_ = clientID
	return &kyberClient{}
}

func (k *kyberClient) QuoteBuy(ctx context.Context, tokenOut string, usdcIn *big.Int) (Quote, error) {
	_ = ctx
	_ = tokenOut
	_ = usdcIn
	return Quote{}, ErrNotConfigured
}

func (k *kyberClient) QuoteSell(ctx context.Context, tokenIn string, amountIn *big.Int) (Quote, error) {
	_ = ctx
	_ = tokenIn
	_ = amountIn
	return Quote{}, ErrNotConfigured
}

func (k *kyberClient) BuildSwap(ctx context.Context, q Quote, sender, recipient string) (SwapCall, error) {
	_ = ctx
	_ = q
	_ = sender
	_ = recipient
	return SwapCall{}, ErrNotConfigured
}
