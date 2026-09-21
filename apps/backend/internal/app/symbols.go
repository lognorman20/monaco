package app

import (
	"context"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/evm"
)

const unknownStockSymbol = "Unknown stock"

// SymbolResolver maps token addresses to user-facing catalog symbols.
type SymbolResolver struct {
	catalog b20.Catalog
}

// NewSymbolResolver returns a resolver backed by the B20 catalog.
func NewSymbolResolver(catalog b20.Catalog) *SymbolResolver {
	return &SymbolResolver{catalog: catalog}
}

// SymbolForMint returns a catalog ticker for a token address.
func (r *SymbolResolver) SymbolForMint(ctx context.Context, token string) string {
	if symbol, ok := r.KnownSymbol(ctx, token); ok {
		return symbol
	}
	if strings.TrimSpace(token) == "" {
		return ""
	}
	return unknownStockSymbol
}

// KnownSymbol is true when token is USDC or a B20 catalog address.
func (r *SymbolResolver) KnownSymbol(ctx context.Context, token string) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	if symbol, ok := knownTokenSymbol(token); ok {
		return symbol, true
	}
	if r != nil && r.catalog != nil {
		asset, found, err := r.catalog.LookupByAddress(ctx, token)
		if err == nil && found && strings.TrimSpace(asset.Symbol) != "" {
			return strings.TrimSpace(asset.Symbol), true
		}
	}
	return "", false
}

func knownTokenSymbol(token string) (string, bool) {
	switch strings.ToLower(token) {
	case evm.USDCAddress:
		return "USDC", true
	default:
		return "", false
	}
}
