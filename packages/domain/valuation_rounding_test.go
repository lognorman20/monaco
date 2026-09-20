package domain

import "testing"

func TestShareUnitsMicrosForDeposit_pricesEveryPotAtItsNav(t *testing.T) {
	cases := []struct {
		name         string
		deposited    USDCMicros
		totalShares  int64
		preCreditNav USDCMicros
		want         int64
		wantErr      bool
	}{
		{name: "first deposit mints 1:1", deposited: 100_000_000, totalShares: 0, preCreditNav: 0, want: 100_000_000},
		{name: "first deposit ignores stray treasury cash", deposited: 100_000_000, totalShares: 0, preCreditNav: 7_000_000, want: 100_000_000},
		{name: "cash-only pot at par", deposited: 25_000_000, totalShares: 100_000_000, preCreditNav: 100_000_000, want: 25_000_000},
		{name: "cash-only pot after a losing round trip", deposited: 90_000_000, totalShares: 100_000_000, preCreditNav: 90_000_000, want: 100_000_000},
		{name: "cash-only pot after a winning round trip", deposited: 110_000_000, totalShares: 100_000_000, preCreditNav: 110_000_000, want: 100_000_000},
		{name: "marked pot after stock halves", deposited: 100_000_000, totalShares: 200_000_000, preCreditNav: 100_000_000, want: 200_000_000},
		{name: "mint floors in favour of the pool", deposited: 1_000_000, totalShares: 100_000_000, preCreditNav: 300_000_000, want: 333_333},
		{name: "worthless pot with shares outstanding cannot price a mint", deposited: 1_000_000, totalShares: 100_000_000, preCreditNav: 0, wantErr: true},
		{name: "deposit too small to mint a unit", deposited: 1, totalShares: 1, preCreditNav: 10, wantErr: true},
		{name: "non-positive deposit", deposited: 0, totalShares: 0, preCreditNav: 0, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ShareUnitsMicrosForDeposit(tc.deposited, tc.totalShares, tc.preCreditNav)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("minted %d, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ShareUnitsMicrosForDeposit: %v", err)
			}
			if got != tc.want {
				t.Fatalf("minted = %d, want %d", got, tc.want)
			}
			if tc.totalShares == 0 {
				return
			}
			// The new depositor's claim never exceeds what they put in.
			claim, err := MulDivFloor(got, int64(tc.preCreditNav+tc.deposited), tc.totalShares+got)
			if err != nil {
				t.Fatalf("MulDivFloor: %v", err)
			}
			if claim > int64(tc.deposited) {
				t.Fatalf("depositor claim %d exceeds deposit %d", claim, tc.deposited)
			}
		})
	}
}

func TestComputeRedeemSlice_floorsInFavourOfThePool(t *testing.T) {
	cases := []struct {
		name   string
		shares int64
		total  int64
		potNav USDCMicros
		want   USDCMicros
	}{
		{name: "stock halved: half the shares get half of what is left", shares: 100_000_000, total: 200_000_000, potNav: 100_000_000, want: 50_000_000},
		{name: "stock doubled: half the shares get half the gain", shares: 100_000_000, total: 200_000_000, potNav: 400_000_000, want: 200_000_000},
		{name: "two thirds rounds down", shares: 2, total: 3, potNav: 1_000_000, want: 666_666},
		{name: "last member out takes the whole pot", shares: 7, total: 7, potNav: 123_457, want: 123_457},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComputeRedeemSlice(RedeemSliceInput{SharesRedeemedMicros: tc.shares, TotalSharesMicros: tc.total, PotNav: tc.potNav})
			if err != nil {
				t.Fatalf("ComputeRedeemSlice: %v", err)
			}
			if got.UsdcOwed != tc.want {
				t.Fatalf("usdc owed = %d, want %d", got.UsdcOwed, tc.want)
			}
		})
	}
}
