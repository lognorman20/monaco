package app

import (
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

func TestPotRowsFromPythInput_perAssetDollarPnL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      pyth.NavInput
		wantUSDC   string
		wantSymbol string
		wantPnL    string
	}{
		{
			name: "usdc only",
			input: pyth.NavInput{
				TreasuryUsdc: 1_250_000,
			},
			wantUSDC: "+0.00",
		},
		{
			name: "xstock gain",
			input: pyth.NavInput{
				TreasuryUsdc: 100_000,
				Holdings: []pyth.MarkedHolding{{
					Symbol:    "AAPLx",
					Mint:      jupiter.AAPLxMint,
					Units:     50_000_000,
					MarkUsdc:  2_400_000,
					CostBasis: 1_000_000,
				}},
			},
			wantUSDC:   "+0.00",
			wantSymbol: "AAPLx",
			wantPnL:    "+0.20",
		},
		{
			name: "xstock loss",
			input: pyth.NavInput{
				TreasuryUsdc: 100_000,
				Holdings: []pyth.MarkedHolding{{
					Symbol:    "AAPLx",
					Mint:      jupiter.AAPLxMint,
					Units:     50_000_000,
					MarkUsdc:  1_600_000,
					CostBasis: 1_000_000,
				}},
			},
			wantUSDC:   "+0.00",
			wantSymbol: "AAPLx",
			wantPnL:    "-0.20",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rows, err := potRowsFromPythInput(tc.input)
			if err != nil {
				t.Fatalf("potRowsFromPythInput: %v", err)
			}
			if rows[0].Symbol != "USDC" {
				t.Fatalf("first row symbol = %q, want USDC", rows[0].Symbol)
			}
			if rows[0].DollarPnL != tc.wantUSDC {
				t.Fatalf("USDC dollarPnl = %q, want %q", rows[0].DollarPnL, tc.wantUSDC)
			}
			if tc.wantSymbol == "" {
				return
			}
			if len(rows) != 2 {
				t.Fatalf("rows = %d, want 2", len(rows))
			}
			if rows[1].Symbol != tc.wantSymbol {
				t.Fatalf("holding symbol = %q, want %q", rows[1].Symbol, tc.wantSymbol)
			}
			if rows[1].DollarPnL != tc.wantPnL {
				t.Fatalf("holding dollarPnl = %q, want %q", rows[1].DollarPnL, tc.wantPnL)
			}
			if rows[1].TokenAmount != "50000000" {
				t.Fatalf("tokenAmount = %q, want 50000000", rows[1].TokenAmount)
			}
		})
	}
}

func TestTokenAtomicsToDecimalUnits_usesEightDecimals(t *testing.T) {
	got, err := pyth.TokenAtomicsToDecimalUnits(75_000_000, jupiter.XStockDecimals)
	if err != nil {
		t.Fatalf("TokenAtomicsToDecimalUnits: %v", err)
	}
	if got != "0.75" {
		t.Fatalf("units = %q, want 0.75", got)
	}
}

func TestMarkedPot_nineDecimalHolding_valuesOnceNotTenTimes(t *testing.T) {
	t.Parallel()

	// 1 whole token at 9 dp with a $500 mark must value to $500, not $5,000.
	input := pyth.NavInput{
		Holdings: []pyth.MarkedHolding{{
			Symbol:   "tSpaceX",
			Units:    1_000_000_000,
			MarkUsdc: 500_000_000,
			Decimals: 9,
		}},
	}

	rows, err := potRowsFromPythInput(input)
	if err != nil {
		t.Fatalf("potRowsFromPythInput: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (USDC + holding)", len(rows))
	}
	if rows[1].ValueUsd != "500.00" {
		t.Fatalf("value = %q, want 500.00", rows[1].ValueUsd)
	}
	if rows[1].Units != "1" {
		t.Fatalf("units = %q, want 1", rows[1].Units)
	}
}

func TestPotRowsFromPythInput_largeHoldingValueDoesNotWrap(t *testing.T) {
	t.Parallel()
	// Arrange: 1,000 shares marked at $250. Units x mark = 2.5e19 > int64.
	input := pyth.NavInput{
		Holdings: []pyth.MarkedHolding{{
			Symbol:    "AAPLx",
			Mint:      jupiter.AAPLxMint,
			Units:     1_000 * jupiter.XStockAtomicScale,
			MarkUsdc:  250_000_000,
			CostBasis: 200_000_000_000,
		}},
	}

	// Act
	rows, err := potRowsFromPythInput(input)

	// Assert
	if err != nil {
		t.Fatalf("potRowsFromPythInput: %v", err)
	}
	if rows[1].ValueUsd != "250000.00" {
		t.Fatalf("value = %q, want 250000.00", rows[1].ValueUsd)
	}
	if rows[1].DollarPnL != "+50000.00" {
		t.Fatalf("pnl = %q, want +50000.00", rows[1].DollarPnL)
	}
}

func TestCostBasisMarkedPotInput_recordsCostBasisSource(t *testing.T) {
	// Arrange
	costBasis := []pyth.CostBasis{{
		Symbol: "AAPLx",
		Mint:   jupiter.AAPLxMint,
		Units:  50_000_000,
		Price:  100_000_000,
		Amount: 50_000_000,
	}}

	// Act
	input, err := costBasisMarkedPotInput(7_000_000, costBasis)

	// Assert
	if err != nil {
		t.Fatalf("costBasisMarkedPotInput: %v", err)
	}
	if len(input.Holdings) != 1 {
		t.Fatalf("expected one holding, got %d", len(input.Holdings))
	}
	if got := input.Holdings[0].Source; got != pyth.MarkSourceCostBasis {
		t.Fatalf("source = %q, want %q", got, pyth.MarkSourceCostBasis)
	}
	if got := input.Holdings[0].MarkUsdc; got != 200_000_000 {
		t.Fatalf("mark = %d, want 200000000", got)
	}
}
