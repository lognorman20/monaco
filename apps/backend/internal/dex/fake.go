package dex

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
)

type fakeClient struct {
	mu sync.Mutex

	quotes    map[string]Quote
	quoteErrs map[string]error
	noRoute   map[string]struct{}
	swapCalls map[string]SwapCall
	quoteBuys int
}

// NewFakeClient returns an in-memory DEX client for tests.
func NewFakeClient() Client {
	return &fakeClient{
		quotes:    make(map[string]Quote),
		quoteErrs: make(map[string]error),
		noRoute:   make(map[string]struct{}),
		swapCalls: make(map[string]SwapCall),
	}
}

func buyKey(tokenOut string, usdcIn *big.Int) string {
	return fmt.Sprintf("buy:%s:%s", strings.ToLower(tokenOut), usdcIn.String())
}

func sellKey(tokenIn string, amountIn *big.Int) string {
	return fmt.Sprintf("sell:%s:%s", strings.ToLower(tokenIn), amountIn.String())
}

func swapKey(q Quote) string {
	return fmt.Sprintf("swap:%s:%s:%s", strings.ToLower(q.TokenIn), strings.ToLower(q.TokenOut), q.AmountIn.String())
}

// RegisterQuote registers a buy or sell quote keyed by token pair and amount.
func RegisterQuote(client Client, q Quote) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("dex: RegisterQuote requires NewFakeClient")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if q.TokenIn == USDCAddress() {
		f.quotes[buyKey(q.TokenOut, q.AmountIn)] = q
	} else {
		f.quotes[sellKey(q.TokenIn, q.AmountIn)] = q
	}
}

// RegisterNoRoute marks a buy quote key as unroutable.
func RegisterNoRoute(client Client, tokenOut string, usdcIn *big.Int) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("dex: RegisterNoRoute requires NewFakeClient")
	}
	f.mu.Lock()
	f.noRoute[buyKey(tokenOut, usdcIn)] = struct{}{}
	f.mu.Unlock()
}

// RegisterSwapCall configures BuildSwap output for a quote shape.
func RegisterSwapCall(client Client, q Quote, call SwapCall) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("dex: RegisterSwapCall requires NewFakeClient")
	}
	f.mu.Lock()
	f.swapCalls[swapKey(q)] = call
	f.mu.Unlock()
}

func USDCAddress() string {
	return "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
}

func (f *fakeClient) QuoteBuy(ctx context.Context, tokenOut string, usdcIn *big.Int) (Quote, error) {
	_ = ctx
	key := buyKey(tokenOut, usdcIn)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quoteBuys++
	if _, ok := f.noRoute[key]; ok {
		return Quote{Routable: false, TokenIn: USDCAddress(), TokenOut: tokenOut, AmountIn: usdcIn}, nil
	}
	if err, ok := f.quoteErrs[key]; ok {
		return Quote{}, err
	}
	if q, ok := f.quotes[key]; ok {
		return q, nil
	}
	return Quote{Routable: false, TokenIn: USDCAddress(), TokenOut: tokenOut, AmountIn: usdcIn}, nil
}

func (f *fakeClient) QuoteSell(ctx context.Context, tokenIn string, amountIn *big.Int) (Quote, error) {
	_ = ctx
	key := sellKey(tokenIn, amountIn)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.quoteErrs[key]; ok {
		return Quote{}, err
	}
	if q, ok := f.quotes[key]; ok {
		return q, nil
	}
	return Quote{Routable: false, TokenIn: tokenIn, TokenOut: USDCAddress(), AmountIn: amountIn}, nil
}

func (f *fakeClient) BuildSwap(ctx context.Context, q Quote, sender, recipient string) (SwapCall, error) {
	_ = ctx
	_ = sender
	_ = recipient
	f.mu.Lock()
	defer f.mu.Unlock()
	if call, ok := f.swapCalls[swapKey(q)]; ok {
		return call, nil
	}
	return SwapCall{Router: "0xrouter", Data: []byte("swap"), AmountOutMin: big.NewInt(1)}, nil
}

func QuoteBuyCallCount(client Client) int {
	f, ok := client.(*fakeClient)
	if !ok {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.quoteBuys
}
