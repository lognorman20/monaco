package app

import (
	"context"
	"strings"
	"unicode"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const unknownStockSymbol = "Unknown stock"

// SymbolResolver maps Solana mint addresses to user-facing catalog symbols.
type SymbolResolver struct {
	catalog xstocks.CatalogSearcher
}

// NewSymbolResolver returns a resolver backed by the xStocks mint catalog.
func NewSymbolResolver(catalog xstocks.CatalogSearcher) *SymbolResolver {
	return &SymbolResolver{catalog: catalog}
}

// SymbolForMint returns a catalog ticker for a mint, never a raw pubkey.
func (r *SymbolResolver) SymbolForMint(ctx context.Context, mint string) string {
	mint = strings.TrimSpace(mint)
	if mint == "" {
		return ""
	}
	if symbol, ok := knownMintSymbol(mint); ok {
		return symbol
	}
	if r != nil && r.catalog != nil {
		asset, found, err := r.catalog.LookupByMint(ctx, mint)
		if err == nil && found && strings.TrimSpace(asset.Symbol) != "" {
			return strings.TrimSpace(asset.Symbol)
		}
	}
	if looksLikeSolanaMint(mint) {
		return unknownStockSymbol
	}
	return mint
}

func knownMintSymbol(mint string) (string, bool) {
	switch mint {
	case jupiter.USDCMint:
		return "USDC", true
	case jupiter.AAPLxMint:
		return "AAPLx", true
	case jupiter.TSLAxMint:
		return "TSLAx", true
	default:
		return "", false
	}
}

func looksLikeSolanaMint(value string) bool {
	if len(value) < 32 || len(value) > 44 {
		return false
	}
	for _, r := range value {
		if !isBase58Char(r) {
			return false
		}
	}
	return true
}

func isBase58Char(r rune) bool {
	if unicode.IsDigit(r) {
		return true
	}
	if r >= 'A' && r <= 'H' {
		return true
	}
	if r >= 'J' && r <= 'N' {
		return true
	}
	if r >= 'P' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'k' {
		return true
	}
	if r >= 'm' && r <= 'z' {
		return true
	}
	return false
}
