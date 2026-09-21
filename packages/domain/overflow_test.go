package domain

import (
	"math"
	"math/big"
	"testing"
)

// Overflow safety: every numeric entry point either returns the mathematically
// correct value or an error. A wrapped or negative amount is never acceptable.

func TestShareUnitsMicrosForDeposit_resultBeyondInt64_returnsError(t *testing.T) {
	// Arrange — MaxInt64 micros at a 1-micro share price needs ~9.2e24 share micros.
	nav := PotNAV{PerShareUsdc: 1}

	// Act
	micros, err := ShareUnitsMicrosForDeposit(USDCMicros(math.MaxInt64), nav)

	// Assert
	if err == nil {
		t.Fatalf("expected overflow error, got share micros = %d", micros)
	}
}

func TestSharesForDeposit_resultBeyondInt64_returnsError(t *testing.T) {
	// Arrange
	nav := PotNAV{PerShareUsdc: 999_999}

	// Act
	shares, err := SharesForDeposit(USDCMicros(math.MaxInt64), nav)

	// Assert
	if err == nil {
		t.Fatalf("expected overflow error, got shares = %q", shares)
	}
}

func TestShareUnitsMicrosForDeposit_largestRepresentableDeposit_isExact(t *testing.T) {
	// Arrange — at $1/share the mint is 1:1, so MaxInt64 micros still fits.
	nav := PotNAV{PerShareUsdc: BootstrapSharePriceMicros}

	// Act
	micros, err := ShareUnitsMicrosForDeposit(USDCMicros(math.MaxInt64), nav)

	// Assert
	if err != nil {
		t.Fatalf("ShareUnitsMicrosForDeposit: %v", err)
	}
	if micros != math.MaxInt64 {
		t.Fatalf("share micros = %d, want %d", micros, int64(math.MaxInt64))
	}
}

func TestShareUnitsMicrosForDollarTarget_resultBeyondInt64_returnsError(t *testing.T) {
	// Arrange
	// Act
	micros, err := ShareUnitsMicrosForDollarTarget(USDCMicros(math.MaxInt64), 1)

	// Assert
	if err == nil {
		t.Fatalf("expected overflow error, got share micros = %d", micros)
	}
}

func TestComputePotNAV_extremeInputs_errorInsteadOfWrapping(t *testing.T) {
	cases := []struct {
		name string
		in   NavInput
	}{
		{
			name: "holding value beyond int64",
			in: NavInput{
				Mode:        NavMarked,
				TotalShares: "1",
				Holdings:    []MarkedHolding{{Symbol: "AAPLx", Units: "9223372036854775807", MarkUsdc: math.MaxInt64}},
			},
		},
		{
			name: "holding units with huge exponent",
			in: NavInput{
				Mode:        NavMarked,
				TotalShares: "1",
				Holdings:    []MarkedHolding{{Symbol: "AAPLx", Units: "1e400", MarkUsdc: 1}},
			},
		},
		{
			name: "treasury plus holding sum beyond int64",
			in: NavInput{
				Mode:         NavMarked,
				TreasuryUsdc: math.MaxInt64,
				TotalShares:  "1",
				Holdings:     []MarkedHolding{{Symbol: "AAPLx", Units: "1", MarkUsdc: 1}},
			},
		},
		{
			name: "two holdings sum beyond int64",
			in: NavInput{
				Mode:        NavMarked,
				TotalShares: "1",
				Holdings: []MarkedHolding{
					{Symbol: "AAPLx", Units: "1", MarkUsdc: math.MaxInt64},
					{Symbol: "TSLAx", Units: "1", MarkUsdc: math.MaxInt64},
				},
			},
		},
		{
			name: "per-share price beyond int64 from tiny share count",
			in: NavInput{
				Mode:         NavUSDCOnly,
				TreasuryUsdc: math.MaxInt64,
				TotalShares:  "0.000001",
			},
		},
		{
			name: "negative mark",
			in: NavInput{
				Mode:         NavMarked,
				TreasuryUsdc: 10_000_000,
				TotalShares:  "10",
				Holdings:     []MarkedHolding{{Symbol: "AAPLx", Units: "5", MarkUsdc: -1_000_000}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			// Act
			nav, err := ComputePotNAV(tc.in)

			// Assert
			if err == nil {
				t.Fatalf("expected error, got nav = %+v", nav)
			}
		})
	}
}

func TestComputePotNAV_largestRepresentablePot_isExact(t *testing.T) {
	// Arrange — treasury and holding sum to exactly MaxInt64.
	in := NavInput{
		Mode:         NavMarked,
		TreasuryUsdc: math.MaxInt64 - 1_000_000,
		TotalShares:  "1",
		Holdings:     []MarkedHolding{{Symbol: "AAPLx", Units: "0.5", MarkUsdc: 2_000_000}},
	}

	// Act
	nav, err := ComputePotNAV(in)

	// Assert
	if err != nil {
		t.Fatalf("ComputePotNAV: %v", err)
	}
	if nav.TotalUsdc != math.MaxInt64 {
		t.Fatalf("TotalUsdc = %d, want %d", nav.TotalUsdc, int64(math.MaxInt64))
	}
	if nav.PerShareUsdc != math.MaxInt64 {
		t.Fatalf("PerShareUsdc = %d, want %d", nav.PerShareUsdc, int64(math.MaxInt64))
	}
}

func TestComputePotNAV_tinyMarkTimesHugeUnits_isExact(t *testing.T) {
	// Arrange — 1-micro mark × 9e18 units stays inside int64.
	in := NavInput{
		Mode:        NavMarked,
		TotalShares: "9000000000000000000",
		Holdings:    []MarkedHolding{{Symbol: "PENNYx", Units: "9000000000000000000", MarkUsdc: 1}},
	}

	// Act
	nav, err := ComputePotNAV(in)

	// Assert
	if err != nil {
		t.Fatalf("ComputePotNAV: %v", err)
	}
	if nav.TotalUsdc != 9_000_000_000_000_000_000 {
		t.Fatalf("TotalUsdc = %d, want 9e18", nav.TotalUsdc)
	}
	if nav.PerShareUsdc != 1 {
		t.Fatalf("PerShareUsdc = %d, want 1", nav.PerShareUsdc)
	}
}

func TestMemberEquity_resultBeyondInt64_returnsError(t *testing.T) {
	// Arrange — corrupt ledger: member holds far more than the recorded total.
	memberShares := ShareUnits("1000000000000")
	totalShares := ShareUnits("0.000001")

	// Act
	equity, err := MemberEquity(memberShares, totalShares, USDCMicros(math.MaxInt64))

	// Assert
	if err == nil {
		t.Fatalf("expected overflow error, got equity = %d", equity)
	}
}

func TestMemberEquity_soleMemberOfLargestPot_isExact(t *testing.T) {
	// Arrange
	shares := ShareUnits("9223372036854.775807")

	// Act
	equity, err := MemberEquity(shares, shares, USDCMicros(math.MaxInt64))

	// Assert
	if err != nil {
		t.Fatalf("MemberEquity: %v", err)
	}
	if equity != math.MaxInt64 {
		t.Fatalf("equity = %d, want %d", equity, int64(math.MaxInt64))
	}
}

func TestComputeRedeemSlice_extremeInputs_staysExact(t *testing.T) {
	cases := []struct {
		name string
		in   RedeemSliceInput
		want USDCMicros
	}{
		{
			name: "everything at int64 max",
			in:   RedeemSliceInput{SharesRedeemedMicros: math.MaxInt64, TotalSharesMicros: math.MaxInt64, PotNav: math.MaxInt64},
			want: math.MaxInt64,
		},
		{
			name: "half of the largest pot",
			in:   RedeemSliceInput{SharesRedeemedMicros: 1, TotalSharesMicros: 2, PotNav: math.MaxInt64 - 1},
			want: (math.MaxInt64 - 1) / 2,
		},
		{
			name: "one share micro of a huge ledger and huge pot",
			in:   RedeemSliceInput{SharesRedeemedMicros: 1, TotalSharesMicros: math.MaxInt64, PotNav: math.MaxInt64},
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			// Act
			slice, err := ComputeRedeemSlice(tc.in)

			// Assert
			if err != nil {
				t.Fatalf("ComputeRedeemSlice: %v", err)
			}
			if slice.UsdcOwed != tc.want {
				t.Fatalf("UsdcOwed = %d, want %d", slice.UsdcOwed, tc.want)
			}
		})
	}
}

func TestRatRoundToInt64_outsideInt64_returnsError(t *testing.T) {
	cases := []struct {
		name  string
		value *big.Rat
	}{
		{name: "one above max", value: new(big.Rat).SetInt(new(big.Int).Add(big.NewInt(math.MaxInt64), big.NewInt(1)))},
		{name: "max plus half rounds up past max", value: new(big.Rat).Add(big.NewRat(math.MaxInt64, 1), big.NewRat(1, 2))},
		{name: "two to the 64", value: new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 64))},
		{name: "far below min", value: new(big.Rat).SetInt(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 70)))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			// Act
			got, err := ratRoundToInt64(tc.value)

			// Assert
			if err == nil {
				t.Fatalf("expected overflow error, got %d", got)
			}
		})
	}
}

func TestRatRoundToInt64_roundsHalfUpInsideInt64(t *testing.T) {
	cases := []struct {
		name  string
		value *big.Rat
		want  int64
	}{
		{name: "nil", value: nil, want: 0},
		{name: "below half", value: big.NewRat(49, 100), want: 0},
		{name: "exact half", value: big.NewRat(1, 2), want: 1},
		{name: "above half", value: big.NewRat(151, 100), want: 2},
		{name: "max minus half rounds to max", value: new(big.Rat).Sub(big.NewRat(math.MaxInt64, 1), big.NewRat(1, 2)), want: math.MaxInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			// Act
			got, err := ratRoundToInt64(tc.value)

			// Assert
			if err != nil {
				t.Fatalf("ratRoundToInt64: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestValidateIntent_spentPlusPendingBeyondInt64_rejectsBuy(t *testing.T) {
	// Arrange — 0 − MaxInt64 − MaxInt64 wraps to +2 in unchecked int64 math.
	agent := buildActiveAgent(func(a *GroupAgent) { a.AllocationUsdcMicros = 0 })
	snap := buildTreasurySnapshot(func(s *AgentTreasurySnapshot) {
		s.TreasuryUsdcMicros = math.MaxInt64
		s.AgentSpentUsdcMicros = math.MaxInt64
		s.PendingAgentUsdcMicros = math.MaxInt64
	})
	intent := AgentIntentRequest{Side: AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 2}

	// Act
	err := ValidateIntent(agent, intent, snap)

	// Assert
	if err == nil {
		t.Fatal("expected rejection: agent has no allocation left")
	}
}

func TestValidateIntent_negativeLedgerValues_rejectsBuy(t *testing.T) {
	cases := []struct {
		name  string
		agent func(*GroupAgent)
		snap  func(*AgentTreasurySnapshot)
	}{
		{
			// −3 − MaxInt64 wraps to a huge positive "available" in unchecked math.
			name:  "negative allocation with max spent",
			agent: func(a *GroupAgent) { a.AllocationUsdcMicros = -3 },
			snap:  func(s *AgentTreasurySnapshot) { s.AgentSpentUsdcMicros = math.MaxInt64 },
		},
		{
			name: "negative spent inflates headroom",
			snap: func(s *AgentTreasurySnapshot) { s.AgentSpentUsdcMicros = math.MinInt64 },
		},
		{
			name: "negative pending inflates headroom",
			snap: func(s *AgentTreasurySnapshot) { s.PendingAgentUsdcMicros = -1 },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			agent := buildActiveAgent(tc.agent)
			snap := buildTreasurySnapshot(tc.snap)
			intent := AgentIntentRequest{Side: AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 1}

			// Act
			err := ValidateIntent(agent, intent, snap)

			// Assert
			if err == nil {
				t.Fatal("expected rejection for corrupt ledger values")
			}
		})
	}
}
