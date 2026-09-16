package xstocks

import (
	"context"
	"fmt"
	"sync"
)

// fakeXStocksResolver is the locked test double for Jupiter and integration tests.
type fakeXStocksResolver struct {
	mu    sync.Mutex
	mints map[string]string
	errs  map[string]error
}

// NewFakeResolver returns a deterministic in-memory xStocks resolver for tests.
func NewFakeResolver() Resolver {
	return &fakeXStocksResolver{
		mints: make(map[string]string),
		errs:  make(map[string]error),
	}
}

// RegisterSolanaMint maps a symbol to a Solana mint on the fake resolver.
func RegisterSolanaMint(resolver Resolver, symbol, mint string) {
	fake, ok := resolver.(*fakeXStocksResolver)
	if !ok {
		panic("xstocks: RegisterSolanaMint requires NewFakeResolver")
	}
	fake.mu.Lock()
	fake.mints[symbol] = mint
	delete(fake.errs, symbol)
	fake.mu.Unlock()
}

// RegisterResolveError forces ResolveSolanaMint to return err for symbol.
func RegisterResolveError(resolver Resolver, symbol string, err error) {
	fake, ok := resolver.(*fakeXStocksResolver)
	if !ok {
		panic("xstocks: RegisterResolveError requires NewFakeResolver")
	}
	fake.mu.Lock()
	fake.errs[symbol] = err
	delete(fake.mints, symbol)
	fake.mu.Unlock()
}

func (f *fakeXStocksResolver) ResolveSolanaMint(ctx context.Context, symbol string) (string, error) {
	_ = ctx
	f.mu.Lock()
	if err, ok := f.errs[symbol]; ok {
		f.mu.Unlock()
		return "", err
	}
	mint, ok := f.mints[symbol]
	f.mu.Unlock()
	if !ok || mint == "" {
		return "", fmt.Errorf("%w: %s", ErrNotFound, symbol)
	}
	return mint, nil
}
