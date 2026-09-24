package postgres

import (
	"database/sql"
	"testing"
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
