package app

import (
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

func TestPotRowsFromPythInput_perAssetDollarPnL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      marks.NavInput
		wantUSDC   string
		wantSymbol string
		wantPnL    string
	}{
		{
			name: "usdc only",
			input: marks.NavInput{
				TreasuryUsdc: 1_250_000,
			},
			wantUSDC: "+0.00",
		},
		{
			name: "xstock gain",
			input: marks.NavInput{
				TreasuryUsdc: 100_000,
				Holdings: []marks.MarkedHolding{{
					Symbol:    "AAPLx",
					Mint:      "0xb200000000000000000000c2e324d24d7eecd1fb",
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
			input: marks.NavInput{
				TreasuryUsdc: 100_000,
				Holdings: []marks.MarkedHolding{{
					Symbol:    "AAPLx",
					Mint:      "0xb200000000000000000000c2e324d24d7eecd1fb",
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
	got, err := tokenAtomicsToDecimalUnits(75_000_000)
	if err != nil {
		t.Fatalf("tokenAtomicsToDecimalUnits: %v", err)
	}
	if got != "0.75" {
		t.Fatalf("units = %q, want 0.75", got)
	}
}

func TestCostBasisMarkPerUnitMicros_largeBasisDoesNotWrap(t *testing.T) {
	t.Parallel()
	// Arrange: $200k paid for 1,000 shares. 2e11 micros x 1e8 scale overflows int64.
	totalUSDC := int64(200_000_000_000)
	tokenAtomics := int64(1_000) * b20.TokenAtomicScale

	// Act
	mark, err := costBasisMarkPerUnitMicros(totalUSDC, tokenAtomics)

	// Assert
	if err != nil {
		t.Fatalf("costBasisMarkPerUnitMicros: %v", err)
	}
	if mark != 200_000_000 {
		t.Fatalf("mark = %d, want 200_000_000 ($200/share)", mark)
	}
}

func TestPotRowsFromPythInput_largeHoldingValueDoesNotWrap(t *testing.T) {
	t.Parallel()
	// Arrange: 1,000 shares marked at $250. Units x mark = 2.5e19 > int64.
	input := marks.NavInput{
		Holdings: []marks.MarkedHolding{{
			Symbol:    "AAPLx",
			Mint:      "0xb200000000000000000000c2e324d24d7eecd1fb",
			Units:     1_000 * b20.TokenAtomicScale,
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
