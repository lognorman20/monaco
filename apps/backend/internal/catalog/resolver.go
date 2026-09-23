package catalog

import (
	"context"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// Resolver resolves symbols to Solana mints across Tessera and xStocks.
type Resolver struct {
	xstocks xstocks.Resolver
	tessera Source
}

// NewResolver returns a composite mint resolver.
func NewResolver(xstocksResolver xstocks.Resolver, tessera Source) *Resolver {
	return &Resolver{xstocks: xstocksResolver, tessera: tessera}
}

// ResolveSolanaMint implements xstocks.Resolver.
func (r *Resolver) ResolveSolanaMint(ctx context.Context, symbol string) (string, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return "", xstocks.ErrNotFound
	}
	if mint, ok := r.resolveTessera(ctx, symbol); ok {
		return mint, nil
	}
	if r.xstocks != nil {
		return r.xstocks.ResolveSolanaMint(ctx, symbol)
	}
	return "", xstocks.ErrNotFound
}

func (r *Resolver) resolveTessera(ctx context.Context, symbol string) (string, bool) {
	if r.tessera == nil {
		return "", false
	}
	rows, err := r.tessera.List(ctx)
	if err != nil {
		return "", false
	}
	needle := strings.ToLower(strings.TrimSpace(symbol))
	for _, asset := range rows {
		asset = asset.Normalize()
		sym := strings.ToLower(strings.TrimSpace(asset.Symbol))
		name := strings.ToLower(strings.TrimSpace(asset.Name))
		if needle == sym || needle == name {
			return strings.TrimSpace(asset.SolanaMint), true
		}
	}
	return "", false
}
