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
					Units:     500_000,
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
					Units:     500_000,
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
		})
	}
}
