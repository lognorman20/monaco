package pyth

import (
	"testing"
)

func TestPotAfterHours_ignoresPreIpo(t *testing.T) {
	t.Parallel()

	mixed := []MarkedHolding{
		{AfterHours: true, Kind: "pre_ipo"},
		{AfterHours: false, Kind: "stock"},
	}
	if PotAfterHours(mixed) {
		t.Fatal("pre-IPO after-hours mark must not flag the pot")
	}

	stockFrozen := []MarkedHolding{
		{AfterHours: true, Kind: "stock"},
	}
	if !PotAfterHours(stockFrozen) {
		t.Fatal("stock after-hours mark must flag the pot")
	}
}
