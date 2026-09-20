package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/b20"
)

func TestSymbolResolver_TSLAxMint_returnsTickerNotPubkey(t *testing.T) {
	t.Parallel()

	catalog := b20.NewFakeCatalog()
	b20.RegisterAsset(catalog, b20.Asset{
		Symbol:     "TSLAx",
		Name:       "Tesla",
		TokenAddress: "0xb2000000000000000000000000000000000004",
	})
	resolver := NewSymbolResolver(catalog)

	got := resolver.SymbolForMint(context.Background(), "0xb2000000000000000000000000000000000004")
	if got != "TSLAx" {
		t.Fatalf("SymbolForMint = %q, want TSLAx", got)
	}
	if got == "0xb2000000000000000000000000000000000004" {
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
