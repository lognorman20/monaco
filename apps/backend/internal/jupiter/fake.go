package jupiter

import (
	"context"
	"fmt"
	"sync"
)

// fakeJupiterClient is the locked test double for quote and integration tests.
type fakeJupiterClient struct {
	mu     sync.Mutex
	quotes map[string]BuyQuote
	errs   map[string]error
}

// NewFakeClient returns an in-memory Jupiter client for tests.
func NewFakeClient() Client {
	return &fakeJupiterClient{
		quotes: make(map[string]BuyQuote),
		errs:   make(map[string]error),
	}
}

func quoteKey(outputMint string, usdcAmount int64) string {
	return fmt.Sprintf("%s:%d", outputMint, usdcAmount)
}

// RegisterQuoteBuy configures a fake quote for outputMint and usdcAmount.
func RegisterQuoteBuy(client Client, outputMint string, usdcAmount int64, quote BuyQuote) {
	fake, ok := client.(*fakeJupiterClient)
	if !ok {
		panic("jupiter: RegisterQuoteBuy requires NewFakeClient")
	}
	fake.mu.Lock()
	key := quoteKey(outputMint, usdcAmount)
	fake.quotes[key] = quote
	delete(fake.errs, key)
	fake.mu.Unlock()
}

// RegisterQuoteBuyError forces QuoteBuy to return err for outputMint and usdcAmount.
func RegisterQuoteBuyError(client Client, outputMint string, usdcAmount int64, err error) {
	fake, ok := client.(*fakeJupiterClient)
	if !ok {
		panic("jupiter: RegisterQuoteBuyError requires NewFakeClient")
	}
	fake.mu.Lock()
	key := quoteKey(outputMint, usdcAmount)
	fake.errs[key] = err
	delete(fake.quotes, key)
	fake.mu.Unlock()
}

func (f *fakeJupiterClient) QuoteBuy(ctx context.Context, params QuoteBuyParams) (BuyQuote, error) {
	logQuoteAttempt(params.GroupID, params.UserID, params.Symbol, params.USDCAmount)

	key := quoteKey(params.OutputMint, params.USDCAmount)
	f.mu.Lock()
	if err, ok := f.errs[key]; ok {
		f.mu.Unlock()
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, err.Error())
		return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, err
	}
	quote, ok := f.quotes[key]
	f.mu.Unlock()
	if !ok {
		reason := "no route"
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, reason)
		return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, ErrNoRoute
	}
	if !quote.Routable {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, "no route")
		return quote, ErrNoRoute
	}
	if quote.InputMint == "" {
		quote.InputMint = USDCMint
	}
	if quote.OutputMint == "" {
		quote.OutputMint = params.OutputMint
	}
	return quote, nil
}
