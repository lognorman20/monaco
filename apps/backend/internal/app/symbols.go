package app

import (
	"context"
	"log/slog"
	"math/big"
	"strings"
	"unicode"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const unknownStockSymbol = "Unknown stock"

// SymbolResolver maps Solana mint addresses to user-facing catalog symbols.
type SymbolResolver struct {
	catalog  xstocks.CatalogSearcher
	mintinfo mintinfo.Reader
}

// NewSymbolResolver returns a resolver backed by the xStocks mint catalog.
func NewSymbolResolver(catalog xstocks.CatalogSearcher) *SymbolResolver {
	return &SymbolResolver{catalog: catalog}
}

// SetMintInfo attaches a mintinfo reader for UI multiplier fallback (optional until wiring).
func (r *SymbolResolver) SetMintInfo(reader mintinfo.Reader) {
	if r == nil {
		return
	}
	r.mintinfo = reader
}

// ResolveUiMultiplier loads the scaled-ui multiplier from catalog, then mintinfo.
func (r *SymbolResolver) ResolveUiMultiplier(ctx context.Context, mint string, kind xstocks.AssetKind) (*big.Rat, bool) {
	mint = strings.TrimSpace(mint)
	catalogHit := false
	if r != nil && r.catalog != nil && mint != "" {
		asset, found, err := r.catalog.LookupByMint(ctx, mint)
		if err == nil && found {
			catalogHit = true
			n := asset.Normalize()
			kind = n.Kind
			if n.UiAmountMultiplier != nil {
				return new(big.Rat).Set(n.UiAmountMultiplier), true
			}
		}
	}
	if r != nil && r.mintinfo != nil && mint != "" {
		info, err := r.mintinfo.Info(ctx, mint)
		if err == nil && info.UiMultiplier != nil {
			return new(big.Rat).Set(info.UiMultiplier), true
		}
	}
	if catalogHit {
		// Catalog row without a scaled-ui extension (Tessera, multiplier 1).
		return big.NewRat(1, 1), true
	}
	if mint != "" && looksLikeSolanaMint(mint) {
		slog.Warn("mint_multiplier_unresolved", "mint", mint)
	}
	return pyth.EffectiveUiMultiplier(nil, kind)
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
