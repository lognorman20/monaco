package postgres

import (
	"database/sql"
	"testing"
)

func TestSwapUsdcMicros(t *testing.T) {
	t.Parallel()

	// 12 shares of a stock token, sold for $2,784.60.
	const sellAtomics int64 = 12 * 100_000_000
	const proceeds int64 = 2_784_600_000
	noProceeds := sql.NullInt64{}
	withProceeds := sql.NullInt64{Int64: proceeds, Valid: true}

	cases := []struct {
		name      string
		action    string
		status    string
		amount    int64
		costBasis sql.NullInt64
		want      int64
		wantKnown bool
	}{
		{"confirmed buy spends its amount", TransactionActionBuy, TransactionStatusConfirmed, 3_000_000, sql.NullInt64{Int64: 1_500_000, Valid: true}, 3_000_000, true},
		{"pending buy spends its amount", TransactionActionBuy, TransactionStatusPending, 4_000_000, noProceeds, 4_000_000, true},
		{"failed buy spends its amount", TransactionActionBuy, TransactionStatusFailed, 5_000_000, noProceeds, 5_000_000, true},
		{"confirmed sell reports its proceeds", TransactionActionSell, TransactionStatusConfirmed, sellAtomics, withProceeds, proceeds, true},
		{"pending sell has no dollar figure", TransactionActionSell, TransactionStatusPending, sellAtomics, noProceeds, 0, false},
		{"failed sell has no dollar figure", TransactionActionSell, TransactionStatusFailed, sellAtomics, noProceeds, 0, false},
		{"in-flight sell ignores a stray cost basis", TransactionActionSell, TransactionStatusPending, sellAtomics, withProceeds, 0, false},
		{"confirmed sell without recorded proceeds", TransactionActionSell, TransactionStatusConfirmed, sellAtomics, noProceeds, 0, false},
		{"mixed-case sell", " Sell ", "CONFIRMED", sellAtomics, withProceeds, proceeds, true},
		{"mixed-case in-flight sell", "SELL", "Pending", sellAtomics, noProceeds, 0, false},
		{"mixed-case buy", "BUY", "Confirmed", 3_000_000, noProceeds, 3_000_000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, known := SwapUsdcMicros(tc.action, tc.status, tc.amount, tc.costBasis)
			if got != tc.want || known != tc.wantKnown {
				t.Fatalf("SwapUsdcMicros(%q, %q, %d, %+v) = (%d, %v), want (%d, %v)",
					tc.action, tc.status, tc.amount, tc.costBasis, got, known, tc.want, tc.wantKnown)
			}
		})
	}
}
