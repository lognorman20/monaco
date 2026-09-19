package xstocks

import (
	"context"
	"testing"
)

func TestPopular_returnsPinnedInOrder(t *testing.T) {
	t.Parallel()
	catalog := NewFakeCatalogSearcher()
	RegisterCatalogAsset(catalog, CatalogAsset{Symbol: "TSLAx", Name: "Tesla", Routable: true})
	RegisterCatalogAsset(catalog, CatalogAsset{Symbol: "AAPLx", Name: "Apple", Routable: true})

	got, err := Popular(context.Background(), catalog, 2)
	if err != nil {
		t.Fatalf("Popular: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Symbol != "AAPLx" {
		t.Fatalf("first = %q, want AAPLx", got[0].Symbol)
	}
	if got[1].Symbol != "TSLAx" {
		t.Fatalf("second = %q, want TSLAx", got[1].Symbol)
	}
}
