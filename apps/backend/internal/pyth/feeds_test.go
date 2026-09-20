package pyth

import "testing"

func TestEquityQuerySymbol_mapsXStockToHermesEquityFeed(t *testing.T) {
	query := EquityQuerySymbol("AAPLx")
	if query != "Equity.US.AAPL/USD" {
		t.Fatalf("query = %q, want Equity.US.AAPL/USD", query)
	}
}

func TestEquityQuerySymbol_stripsTrailingC(t *testing.T) {
	if got := EquityQuerySymbol("AAPLc"); got != "Equity.US.AAPL/USD" {
		t.Fatalf("got %q", got)
	}
}
