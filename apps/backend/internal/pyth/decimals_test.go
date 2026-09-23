package pyth

import (
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
)

func TestCostBasisMarkPerUnitMicros_eightDecimals(t *testing.T) {
	t.Parallel()

	totalUSDC := int64(200_000_000_000)
	tokenAtomics := int64(1_000) * jupiter.XStockAtomicScale

	mark, err := CostBasisMarkPerUnitMicros(totalUSDC, tokenAtomics, jupiter.XStockDecimals)
	if err != nil {
		t.Fatalf("CostBasisMarkPerUnitMicros: %v", err)
	}
	if mark != 200_000_000 {
		t.Fatalf("mark = %d, want 200_000_000 ($200/share)", mark)
	}
}

func TestCostBasisMark_nineDecimals(t *testing.T) {
	t.Parallel()

	// $500 spent for 1 whole token (1e9 atomics at 9 dp).
	totalUSDC := int64(500_000_000)
	tokenAtomics := int64(1_000_000_000)

	mark, err := CostBasisMarkPerUnitMicros(totalUSDC, tokenAtomics, 9)
	if err != nil {
		t.Fatalf("CostBasisMarkPerUnitMicros: %v", err)
	}
	if mark != 500_000_000 {
		t.Fatalf("mark = %d, want 500_000_000 ($500/token)", mark)
	}
}

func TestTokenAtomicsToDecimalUnits_nineDecimals(t *testing.T) {
	t.Parallel()

	got, err := TokenAtomicsToDecimalUnits(1_500_000_000, 9)
	if err != nil {
		t.Fatalf("TokenAtomicsToDecimalUnits: %v", err)
	}
	if got != "1.5" {
		t.Fatalf("units = %q, want 1.5", got)
	}
}
