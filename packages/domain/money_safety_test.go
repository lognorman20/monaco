package domain

import (
	"errors"
	"math"
	"math/rand"
	"strconv"
	"testing"
)

// A holding big enough to overflow int64 micros must be reported, never wrapped:
// silently returning a negative pot NAV would price every share off nonsense.
func TestComputePotNAV_holdingValueOverflowsInt64_returnsError(t *testing.T) {
	_, err := ComputePotNAV(NavInput{
		Mode:         NavMarked,
		TreasuryUsdc: 1_000_000,
		TotalShares:  ShareUnits("1000000"),
		Holdings: []MarkedHolding{{
			Symbol:   "AAPLx",
			Units:    "1000000000000",
			MarkUsdc: USDCMicros(math.MaxInt64 / 1_000),
		}},
	})
	if !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("ComputePotNAV error = %v, want ErrAmountOverflow", err)
	}
}

func TestComputePotNAV_sumOfHoldingsOverflowsInt64_returnsError(t *testing.T) {
	big := MarkedHolding{Symbol: "AAPLx", Units: "4000000000", MarkUsdc: 1_000_000_000}
	_, err := ComputePotNAV(NavInput{
		Mode:         NavMarked,
		TreasuryUsdc: 0,
		TotalShares:  ShareUnits("1000000"),
		Holdings:     []MarkedHolding{big, big, big},
	})
	if !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("ComputePotNAV error = %v, want ErrAmountOverflow", err)
	}
}

func TestComputePotNAV_marksHoldingsDownToWholeMicros(t *testing.T) {
	nav, err := ComputePotNAV(NavInput{
		Mode:         NavMarked,
		TreasuryUsdc: 0,
		TotalShares:  ShareUnits("1"),
		Holdings: []MarkedHolding{{
			Symbol:   "AAPLx",
			Units:    "0.0000005",
			MarkUsdc: 3,
		}},
	})
	if err != nil {
		t.Fatalf("ComputePotNAV: %v", err)
	}
	// 0.0000005 * 3 = 0.0000015 micros: the pot keeps the fraction, it does not invent a micro.
	if nav.TotalUsdc != 0 {
		t.Fatalf("TotalUsdc = %d, want 0", nav.TotalUsdc)
	}
}

// Rounding direction: the redeem payout is floored so a member can never be paid
// more micros than their share fraction of the pot is worth.
func TestComputeRedeemSlice_flooredNeverOverpays(t *testing.T) {
	slice, err := ComputeRedeemSlice(RedeemSliceInput{
		SharesRedeemedMicros: 1,
		TotalSharesMicros:    3,
		PotNav:               1_000_000,
	})
	if err != nil {
		t.Fatalf("ComputeRedeemSlice: %v", err)
	}
	if slice.UsdcOwed != 333_333 {
		t.Fatalf("UsdcOwed = %d, want 333333 (floor of 333333.33)", slice.UsdcOwed)
	}
}

// Three members redeeming their whole position one after another can never take
// out more than the pot holds, at any share split.
func TestComputeRedeemSlice_sumOfAllSlicesNeverExceedsPot(t *testing.T) {
	rng := rand.New(rand.NewSource(20260919))
	for i := 0; i < 500; i++ {
		potNav := USDCMicros(rng.Int63n(500_000_000) + 1)
		shares := []int64{
			rng.Int63n(1_000_000_000) + 1,
			rng.Int63n(1_000_000_000) + 1,
			rng.Int63n(1_000_000_000) + 1,
		}
		total := shares[0] + shares[1] + shares[2]

		var paid USDCMicros
		for _, redeemed := range shares {
			slice, err := ComputeRedeemSlice(RedeemSliceInput{
				SharesRedeemedMicros: redeemed,
				TotalSharesMicros:    total,
				PotNav:               potNav,
			})
			if err != nil {
				// Slices below one micro are rejected; they simply pay nothing.
				continue
			}
			paid += slice.UsdcOwed
		}
		if paid > potNav {
			t.Fatalf("paid %d micros out of a %d pot (shares %v)", paid, potNav, shares)
		}
	}
}

// Minting is floored too: shares minted for a deposit can never claim back more
// than was paid in, across any NAV per share.
func TestShareUnitsMicrosForDeposit_mintedSharesNeverClaimMoreThanDeposited(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 500; i++ {
		perShare := USDCMicros(rng.Int63n(50_000_000) + 1)
		deposited := USDCMicros(rng.Int63n(100_000_000) + 1)
		existingShares := rng.Int63n(1_000_000_000) + 1
		potBefore := USDCMicros(0)
		slice, err := ComputeRedeemSlice(RedeemSliceInput{
			SharesRedeemedMicros: existingShares,
			TotalSharesMicros:    existingShares,
			PotNav:               USDCMicros(existingShares),
		})
		if err == nil {
			potBefore = slice.UsdcOwed
		}

		minted, err := ShareUnitsMicrosForDeposit(deposited, PotNAV{PerShareUsdc: perShare})
		if err != nil {
			t.Fatalf("ShareUnitsMicrosForDeposit: %v", err)
		}
		if minted < 0 {
			t.Fatalf("minted %d share units for %d micros", minted, deposited)
		}
		if minted == 0 {
			continue
		}

		// The depositor's freshly minted stake, valued at the same NAV per share it
		// was minted at, must be worth at most the USDC that actually arrived.
		stakeValue, err := MulDivFloor(minted, int64(perShare), 1_000_000)
		if err != nil {
			t.Fatalf("MulDivFloor: %v", err)
		}
		if USDCMicros(stakeValue) > deposited {
			t.Fatalf("minting %d units for a %d micro deposit is worth %d (pot before %d)",
				minted, deposited, stakeValue, potBefore)
		}
	}
}

func TestShareUnitsMicrosForDollarTarget_flooredToTarget(t *testing.T) {
	micros, err := ShareUnitsMicrosForDollarTarget(1_000_000, 3_000_000)
	if err != nil {
		t.Fatalf("ShareUnitsMicrosForDollarTarget: %v", err)
	}
	if micros != 333_333 {
		t.Fatalf("share units = %d, want 333333 (floor of 333333.33)", micros)
	}
}

// Every member's equity summed must stay within pot NAV: floor, never round.
func TestMemberEquity_sumOverMembersNeverExceedsPotNav(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	for i := 0; i < 500; i++ {
		potNav := USDCMicros(rng.Int63n(1_000_000_000) + 1)
		members := []int64{
			rng.Int63n(1_000_000) + 1,
			rng.Int63n(1_000_000) + 1,
			rng.Int63n(1_000_000) + 1,
			rng.Int63n(1_000_000) + 1,
		}
		var total int64
		for _, m := range members {
			total += m
		}
		totalShares := ShareUnits(strconv.FormatInt(total, 10))

		var sum USDCMicros
		for _, m := range members {
			equity, err := MemberEquity(ShareUnits(strconv.FormatInt(m, 10)), totalShares, potNav)
			if err != nil {
				t.Fatalf("MemberEquity: %v", err)
			}
			sum += equity
		}
		if sum > potNav {
			t.Fatalf("member equity sums to %d against a %d pot (%v)", sum, potNav, members)
		}
	}
}

func TestMulDivFloor_rejectsOverflowAndNegatives(t *testing.T) {
	if _, err := MulDivFloor(math.MaxInt64, math.MaxInt64, 1); err == nil {
		t.Fatal("MulDivFloor overflow: want error")
	}
	if _, err := MulDivFloor(-1, 2, 3); err == nil {
		t.Fatal("MulDivFloor negative operand: want error")
	}
	if _, err := MulDivFloor(1, 2, 0); err == nil {
		t.Fatal("MulDivFloor zero divisor: want error")
	}
}

func TestAddMicros_detectsOverflow(t *testing.T) {
	if _, err := AddMicros(math.MaxInt64, 1); !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("AddMicros overflow error = %v, want ErrAmountOverflow", err)
	}
	sum, err := AddMicros(2_000_000, 3_000_000)
	if err != nil || sum != 5_000_000 {
		t.Fatalf("AddMicros = %d, %v; want 5000000, nil", sum, err)
	}
}
