package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
)

func TestSwapUsdcMicros_sellUsesProceedsNotTokenAtomics(t *testing.T) {
	t.Parallel()

	// 12 xStock shares at 1e8 atomics per share: the input side of a sell.
	const soldAtomics = int64(1_200_000_000)
	const proceedsMicros = int64(2_784_600_000)

	cases := []struct {
		name      string
		action    string
		status    string
		amount    int64
		costBasis sql.NullInt64
		want      int64
		wantKnown bool
	}{
		{
			name:   "buy spends usdc, so amount is already micros",
			action: TransactionActionBuy, status: TransactionStatusConfirmed,
			amount: 5_000_000, want: 5_000_000, wantKnown: true,
		},
		{
			name:   "confirmed sell reports its recorded proceeds",
			action: TransactionActionSell, status: TransactionStatusConfirmed,
			amount:    soldAtomics,
			costBasis: sql.NullInt64{Int64: proceedsMicros, Valid: true},
			want:      proceedsMicros, wantKnown: true,
		},
		{
			name:   "in-flight sell has no dollar figure yet",
			action: TransactionActionSell, status: TransactionStatusPending,
			amount: soldAtomics, want: 0, wantKnown: false,
		},
		{
			name:   "confirmed sell with no recorded proceeds stays unknown",
			action: TransactionActionSell, status: TransactionStatusConfirmed,
			amount: soldAtomics, want: 0, wantKnown: false,
		},
		{
			name:   "action and status casing from older rows still matches",
			action: "SELL", status: "Confirmed",
			amount:    soldAtomics,
			costBasis: sql.NullInt64{Int64: proceedsMicros, Valid: true},
			want:      proceedsMicros, wantKnown: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, known := SwapUsdcMicros(tc.action, tc.status, tc.amount, tc.costBasis)
			if got != tc.want || known != tc.wantKnown {
				t.Fatalf("SwapUsdcMicros(%q, %q, %d, %+v) = (%d, %t), want (%d, %t)",
					tc.action, tc.status, tc.amount, tc.costBasis, got, known, tc.want, tc.wantKnown)
			}
		})
	}
}

func TestListNetTokenHoldingsByGroup_returnsDecimalsPerMint(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	_, groupID := seedProposalGroup(t, store, iso)

	const tesseraMint = "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v"
	const buyTokens int64 = 1_000_000_000
	const buyUSDC int64 = 10_000_000

	_, _, err := store.ConfirmBuyTransaction(ctx, ConfirmBuyTransactionParams{
		GroupID:          groupID,
		Amount:           buyUSDC,
		InputMint:        jupiter.USDCMint,
		OutputMint:       tesseraMint,
		TxSignature:      fmt.Sprintf("sig-%s-tessera-decimals", iso.Suffix()),
		ExecuteRequestID: fmt.Sprintf("req-%s-tessera-decimals", iso.Suffix()),
		CostBasisPrice:   buyUSDC,
		CostBasisAmount:  buyTokens,
		TokenDecimals:    9,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	holdings, err := store.ListNetTokenHoldingsByGroup(ctx, groupID)
	if err != nil {
		t.Fatalf("ListNetTokenHoldingsByGroup: %v", err)
	}

	var found *TokenHoldingRow
	for i := range holdings {
		if holdings[i].Mint == tesseraMint {
			found = &holdings[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected holding for %s, got %+v", tesseraMint, holdings)
	}
	if found.TokenDecimals != 9 {
		t.Fatalf("TokenDecimals = %d, want 9", found.TokenDecimals)
	}
}
