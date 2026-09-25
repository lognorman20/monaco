package catalog

import (
	"context"
	"errors"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// Resolver resolves symbols to Solana mints across supplemental sources and xStocks.
type Resolver struct {
	xstocks xstocks.Resolver
	tessera Source
	catalog *Composite
}

// NewResolver returns a composite mint resolver with an optional Tessera source.
func NewResolver(xstocksResolver xstocks.Resolver, tessera Source) *Resolver {
	return &Resolver{xstocks: xstocksResolver, tessera: tessera}
}

// NewResolverWithCatalog resolves symbols using a Composite for supplemental rows and defaults.
func NewResolverWithCatalog(xstocksResolver xstocks.Resolver, catalog *Composite) *Resolver {
	return &Resolver{xstocks: xstocksResolver, catalog: catalog}
}

// ResolveSolanaMint implements xstocks.Resolver.
func (r *Resolver) ResolveSolanaMint(ctx context.Context, symbol string) (string, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return "", xstocks.ErrNotFound
	}
	if mint, ok := r.resolveSupplementalExact(ctx, symbol); ok {
		return mint, nil
	}
	if r.xstocks != nil {
		mint, err := r.xstocks.ResolveSolanaMint(ctx, symbol)
		if err == nil {
			return mint, nil
		}
		if !errors.Is(err, xstocks.ErrNotFound) {
			return "", err
		}
	}
	if mint, ok := r.resolveUnderlyingDefault(ctx, symbol); ok {
		return mint, nil
	}
	return "", xstocks.ErrNotFound
}

func (r *Resolver) resolveSupplementalExact(ctx context.Context, symbol string) (string, bool) {
	if r.catalog != nil {
		for _, asset := range r.catalog.supplementalRows(ctx) {
			if resolverExactMatch(symbol, asset) {
				return strings.TrimSpace(asset.SolanaMint), true
			}
		}
		return "", false
	}
	return r.resolveTessera(ctx, symbol)
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

func (r *Resolver) resolveUnderlyingDefault(ctx context.Context, symbol string) (string, bool) {
	if r.catalog == nil {
		return "", false
	}
	underlying := strings.ToLower(strings.TrimSpace(symbol))
	if !isKnownPreIPOUnderlying(underlying) {
		return "", false
	}
	return r.catalog.DefaultMintForUnderlying(ctx, underlying)
}

func isKnownPreIPOUnderlying(id string) bool {
	switch id {
	case "spacex", "openai", "kalshi", "anthropic", "anduril", "neuralink", "figureai", "polymarket":
		return true
	default:
		return false
	}
}
