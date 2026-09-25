package pyth

import (
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func TestPotAfterHours_ignoresPreIpo(t *testing.T) {
	t.Parallel()

	mixed := []MarkedHolding{
		{AfterHours: true, Kind: xstocks.AssetKindPreIPO},
		{AfterHours: false, Kind: xstocks.AssetKindStock},
	}
	if PotAfterHours(mixed) {
		t.Fatal("pre-IPO after-hours mark must not flag the pot")
	}

	stockFrozen := []MarkedHolding{
		{AfterHours: true, Kind: xstocks.AssetKindStock},
	}
	if !PotAfterHours(stockFrozen) {
		t.Fatal("stock after-hours mark must flag the pot")
	}
}
