package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func TestSymbolResolver_TSLAxMint_returnsTickerNotPubkey(t *testing.T) {
	t.Parallel()

	catalog := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{
		Symbol:     "TSLAx",
		Name:       "Tesla",
		SolanaMint: jupiter.TSLAxMint,
	})
	resolver := NewSymbolResolver(catalog)

	got := resolver.SymbolForMint(context.Background(), jupiter.TSLAxMint)
	if got != "TSLAx" {
		t.Fatalf("SymbolForMint = %q, want TSLAx", got)
	}
	if got == jupiter.TSLAxMint {
		t.Fatalf("SymbolForMint returned raw mint")
	}
}

func TestSymbolResolver_unknownMint_returnsUnknownStock(t *testing.T) {
	t.Parallel()

	resolver := NewSymbolResolver(nil)
	got := resolver.SymbolForMint(context.Background(), "7GCihgDB8fe6KNjn2MYtkzZcRjQy3V9kP8qK9mN3vLxW")
	if got != unknownStockSymbol {
		t.Fatalf("SymbolForMint = %q, want %q", got, unknownStockSymbol)
	}
}
