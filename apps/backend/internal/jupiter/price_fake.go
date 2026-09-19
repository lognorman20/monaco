package jupiter

import (
	"context"
	"sync"
)

// fakePriceClient is the locked test double for TestGET_assets_* price tests.
type fakePriceClient struct {
	mu     sync.Mutex
	prices map[string]TokenPrice
	err    error
	calls  int
}

// NewFakePriceClient returns an in-memory Jupiter price client for tests.
func NewFakePriceClient() PriceClient {
	return &fakePriceClient{prices: make(map[string]TokenPrice)}
}

// RegisterPrice configures a fake mark for mint.
func RegisterPrice(client PriceClient, mint string, price TokenPrice) {
	fake, ok := client.(*fakePriceClient)
	if !ok {
		panic("jupiter: RegisterPrice requires NewFakePriceClient")
	}
	fake.mu.Lock()
	fake.prices[mint] = price
	fake.mu.Unlock()
}

// RegisterPriceError forces Prices to return err for every call.
func RegisterPriceError(client PriceClient, err error) {
	fake, ok := client.(*fakePriceClient)
	if !ok {
		panic("jupiter: RegisterPriceError requires NewFakePriceClient")
	}
	fake.mu.Lock()
	fake.err = err
	fake.mu.Unlock()
}

// PriceCallCount returns how many Prices calls hit this fake client. Test hook.
func PriceCallCount(client PriceClient) int {
	fake, ok := client.(*fakePriceClient)
	if !ok {
		return 0
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.calls
}

func (f *fakePriceClient) Prices(_ context.Context, mints []string) (map[string]TokenPrice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]TokenPrice, len(mints))
	for _, mint := range mints {
		if price, ok := f.prices[mint]; ok {
			out[mint] = price
		}
	}
	return out, nil
}
