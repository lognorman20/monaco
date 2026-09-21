package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/b20"
)

func TestSymbolResolver_KnownSymbol_skipsSolanaMint(t *testing.T) {
	t.Parallel()
	r := NewSymbolResolver(b20.NewPinnedCatalog())
	if _, ok := r.KnownSymbol(context.Background(), "So11111111111111111111111111111111111111112"); ok {
		t.Fatal("expected unknown Solana mint")
	}
	if r.SymbolForMint(context.Background(), "So11111111111111111111111111111111111111112") != unknownStockSymbol {
		t.Fatal("display fallback")
	}
	sym, ok := r.KnownSymbol(context.Background(), "0xb200000000000000000000c2e324d24d7eecd1fb")
	if !ok || sym != "AAPLc" {
		t.Fatalf("got %q ok=%v", sym, ok)
	}
}
