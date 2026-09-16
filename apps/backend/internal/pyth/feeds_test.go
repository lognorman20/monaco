package pyth

import "testing"

func TestEquityQuerySymbol_mapsXStockToHermesEquityFeed(t *testing.T) {
	// Arrange
	symbol := "AAPLx"

	// Act
	query := EquityQuerySymbol(symbol)

	// Assert
	if query != "Equity.US.AAPL/USD" {
		t.Fatalf("query = %q, want Equity.US.AAPL/USD", query)
	}
}
