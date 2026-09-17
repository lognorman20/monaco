package domain

import "testing"

func buildMarkedHolding(overrides func(*MarkedHolding)) MarkedHolding {
	holding := MarkedHolding{
		Symbol:    "AAPLx",
		Units:     "0.1",
		MarkUsdc:  600_000_000,
		CostBasis: 50_000_000,
	}
	if overrides != nil {
		overrides(&holding)
	}
	return holding
}

func TestComputePotNAV_usdcOnly_returnsTreasuryUsdc(t *testing.T) {
	// Arrange
	input := NavInput{
		Mode:         NavUSDCOnly,
		TreasuryUsdc: 50_000_000,
		TotalShares:  ShareUnits("100"),
		Holdings: []MarkedHolding{
			buildMarkedHolding(nil),
		},
	}

	// Act
	nav, err := ComputePotNAV(input)

	// Assert
	if err != nil {
		t.Fatalf("ComputePotNAV: %v", err)
	}
	if nav.TotalUsdc != 50_000_000 {
		t.Fatalf("total usdc: got %d want %d", nav.TotalUsdc, 50_000_000)
	}
	if nav.PerShareUsdc != 500_000 {
		t.Fatalf("per-share usdc: got %d want %d", nav.PerShareUsdc, 500_000)
	}
}

func TestComputePotNAV_mixedPot_sumsUsdcAndMarkedHoldings(t *testing.T) {
	// Arrange
	input := NavInput{
		Mode:         NavMarked,
		TreasuryUsdc: 40_000_000,
		TotalShares:  ShareUnits("100"),
		Holdings: []MarkedHolding{
			buildMarkedHolding(func(h *MarkedHolding) {
				h.Units = "0.1"
				h.MarkUsdc = 600_000_000
				h.CostBasis = 50_000_000
			}),
		},
	}

	// Act
	nav, err := ComputePotNAV(input)

	// Assert
	if err != nil {
		t.Fatalf("ComputePotNAV: %v", err)
	}
	if nav.TotalUsdc != 100_000_000 {
		t.Fatalf("total usdc: got %d want %d", nav.TotalUsdc, 100_000_000)
	}
	if nav.PerShareUsdc != 1_000_000 {
		t.Fatalf("per-share usdc: got %d want %d", nav.PerShareUsdc, 1_000_000)
	}
}

func TestComputePotNAV_unparseableTotalShares_returnsError(t *testing.T) {
	// Arrange
	input := NavInput{
		Mode:         NavUSDCOnly,
		TreasuryUsdc: 50_000_000,
		TotalShares:  ShareUnits("not-a-number"),
	}

	// Act
	_, err := ComputePotNAV(input)

	// Assert
	if err == nil {
		t.Fatal("ComputePotNAV: expected error for unparseable total shares")
	}
}

func TestComputePotNAV_negativeTotalShares_returnsError(t *testing.T) {
	// Arrange
	input := NavInput{
		Mode:         NavUSDCOnly,
		TreasuryUsdc: 50_000_000,
		TotalShares:  ShareUnits("-10"),
	}

	// Act
	_, err := ComputePotNAV(input)

	// Assert
	if err == nil {
		t.Fatal("ComputePotNAV: expected error for negative total shares")
	}
}

func TestComputePotNAV_emptyShares_firstDepositUsesOneDollarPerShare(t *testing.T) {
	// Arrange
	input := NavInput{
		Mode:         NavUSDCOnly,
		TreasuryUsdc: 0,
		TotalShares:  ShareUnits("0"),
	}

	// Act
	nav, err := ComputePotNAV(input)

	// Assert
	if err != nil {
		t.Fatalf("ComputePotNAV: %v", err)
	}
	if nav.TotalUsdc != 0 {
		t.Fatalf("total usdc: got %d want %d", nav.TotalUsdc, 0)
	}
	if nav.PerShareUsdc != BootstrapSharePriceMicros {
		t.Fatalf("per-share usdc: got %d want %d", nav.PerShareUsdc, BootstrapSharePriceMicros)
	}
}
