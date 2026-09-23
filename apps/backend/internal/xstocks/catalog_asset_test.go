package xstocks

import (
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
)

func TestCatalogAssetNormalize_zeroValues_meanStock8(t *testing.T) {
	asset := CatalogAsset{}.Normalize()
	if asset.Kind != AssetKindStock {
		t.Fatalf("Kind = %q, want stock", asset.Kind)
	}
	if asset.Decimals != jupiter.XStockDecimals {
		t.Fatalf("Decimals = %d, want %d", asset.Decimals, jupiter.XStockDecimals)
	}
	if asset.Source != AssetSourceXStocks {
		t.Fatalf("Source = %q, want xstocks", asset.Source)
	}
	if asset.AtomicScale() != jupiter.XStockAtomicScale {
		t.Fatalf("AtomicScale = %d, want %d", asset.AtomicScale(), jupiter.XStockAtomicScale)
	}
}
