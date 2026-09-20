package b20

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type fakeCatalog struct {
	mu     sync.Mutex
	assets map[string]Asset
	byAddr map[string]Asset
}

// NewFakeCatalog returns an in-memory B20 catalog for tests.
func NewFakeCatalog() Catalog {
	return &fakeCatalog{
		assets: make(map[string]Asset),
		byAddr: make(map[string]Asset),
	}
}

// RegisterTokenAddress registers a symbol → token mapping on a fake catalog.
func RegisterTokenAddress(c Catalog, symbol, addr string) {
	RegisterAsset(c, Asset{Symbol: symbol, Name: symbol, TokenAddress: addr, Decimals: 8})
}

// RegisterCatalogAsset is an alias for RegisterAsset.
func RegisterCatalogAsset(c Catalog, asset Asset) {
	RegisterAsset(c, asset)
}

// RegisterAsset adds an asset to the fake catalog.
func RegisterAsset(c Catalog, asset Asset) {
	f, ok := c.(*fakeCatalog)
	if !ok {
		panic("b20: RegisterAsset requires NewFakeCatalog")
	}
	f.mu.Lock()
	f.assets[strings.ToUpper(asset.Symbol)] = asset
	f.byAddr[strings.ToLower(asset.TokenAddress)] = asset
	f.mu.Unlock()
}

func (f *fakeCatalog) ResolveTokenAddress(ctx context.Context, symbol string) (string, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.assets[strings.ToUpper(strings.TrimSpace(symbol))]
	if !ok {
		return "", fmt.Errorf("%w: unknown symbol %q", ErrNotFound, symbol)
	}
	return a.TokenAddress, nil
}

func (f *fakeCatalog) Search(ctx context.Context, query string, limit, offset int) (SearchPage, error) {
	_ = ctx
	q := strings.ToLower(strings.TrimSpace(query))
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []Asset
	for _, a := range f.assets {
		if q == "" || strings.Contains(strings.ToLower(a.Symbol), q) || strings.Contains(strings.ToLower(a.Name), q) {
			all = append(all, a)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Symbol < all[j].Symbol })
	if offset >= len(all) {
		return SearchPage{}, nil
	}
	end := len(all)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return SearchPage{Assets: all[offset:end], HasMore: end < len(all)}, nil
}

func (f *fakeCatalog) LookupByAddress(ctx context.Context, addr string) (Asset, bool, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byAddr[strings.ToLower(strings.TrimSpace(addr))]
	return a, ok, nil
}

func (f *fakeCatalog) Popular(ctx context.Context) ([]Asset, error) {
	page, err := f.Search(ctx, "", 8, 0)
	return page.Assets, err
}

func (f *fakeCatalog) Feed(ctx context.Context, symbol string) (string, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.assets[strings.ToUpper(strings.TrimSpace(symbol))]
	if !ok {
		return "", fmt.Errorf("unknown symbol %q", symbol)
	}
	if a.FeedAddress == "" {
		sum := sha256.Sum256([]byte("feed:" + a.Symbol))
		return "0x" + hex.EncodeToString(sum[:20]), nil
	}
	return a.FeedAddress, nil
}
