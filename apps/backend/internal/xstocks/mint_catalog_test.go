package xstocks

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
)

func TestFakeCatalogSearcher_LookupByMint_returnsSymbol(t *testing.T) {
	t.Parallel()

	catalog := NewFakeCatalogSearcher()
	RegisterCatalogAsset(catalog, CatalogAsset{
		Symbol:     "TSLAx",
		Name:       "Tesla",
		SolanaMint: jupiter.TSLAxMint,
	})

	asset, found, err := catalog.LookupByMint(context.Background(), jupiter.TSLAxMint)
	if err != nil {
		t.Fatalf("LookupByMint: %v", err)
	}
	if !found {
		t.Fatal("LookupByMint: want found")
	}
	if asset.Symbol != "TSLAx" {
		t.Fatalf("symbol = %q, want TSLAx", asset.Symbol)
	}
}
