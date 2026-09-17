package app

import (
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

func TestDomainNavInputFromPyth_scalesTokenAtomicsToDecimalUnits(t *testing.T) {
	// Arrange
	input := pyth.NavInput{
		TreasuryUsdc: 0,
		Holdings: []pyth.MarkedHolding{{
			Symbol:    "AAPLx",
			Units:     500_000,
			MarkUsdc:  600_000_000,
			CostBasis: 50_000_000,
		}},
	}

	// Act
	navInput, err := domainNavInputFromPyth(input, domain.ShareUnits("100"))

	// Assert
	if err != nil {
		t.Fatalf("domainNavInputFromPyth: %v", err)
	}
	if len(navInput.Holdings) != 1 {
		t.Fatalf("holdings len = %d, want 1", len(navInput.Holdings))
	}
	if navInput.Holdings[0].Units != "0.5" {
		t.Fatalf("units = %q, want %q", navInput.Holdings[0].Units, "0.5")
	}
}
