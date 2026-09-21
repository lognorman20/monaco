package domain

import (
	"math"
	"math/big"
	"testing"
)

func TestPercentReturn_zeroNetIn_isUndefinedForAnyEquity(t *testing.T) {
	for _, equity := range []USDCMicros{math.MinInt64, -1, 0, 1, 1_000_000, math.MaxInt64} {
		// Arrange
		// Act
		pct := PercentReturn(equity, 0)

		// Assert
		if pct != nil {
			t.Fatalf("PercentReturn(%d, 0) = %v, want nil", equity, *pct)
		}
	}
}

func TestPercentReturn_randomInputs_finiteAndSignMatchesEquityVersusNetIn(t *testing.T) {
	rng := newSeededRand(t, 53)
	for i := 0; i < 20_000; i++ {
		// Arrange — positive net in: the only case the boards rank.
		netIn := USDCMicros(1 + randomAmountAbs(rng.Int63()))
		equity := USDCMicros(randomAmountAbs(rng.Int63()))
		if rng.Intn(10) == 0 {
			equity = netIn
		}

		// Act
		pct := PercentReturn(equity, netIn)

		// Assert
		if pct == nil {
			t.Fatalf("PercentReturn(%d, %d) = nil for non-zero net in", equity, netIn)
		}
		if math.IsNaN(*pct) || math.IsInf(*pct, 0) {
			t.Fatalf("PercentReturn(%d, %d) = %v", equity, netIn, *pct)
		}
		switch {
		case equity > netIn && *pct <= 0:
			t.Fatalf("equity %d > net in %d but return %v is not positive", equity, netIn, *pct)
		case equity < netIn && *pct >= 0:
			t.Fatalf("equity %d < net in %d but return %v is not negative", equity, netIn, *pct)
		case equity == netIn && *pct != 0:
			t.Fatalf("equity == net in == %d but return is %v", equity, *pct)
		}
		if *pct < -1 {
			t.Fatalf("PercentReturn(%d, %d) = %v: cannot lose more than 100%% of net in", equity, netIn, *pct)
		}
	}
}

// randomAmountAbs spreads a raw 63-bit draw across magnitudes from micros to int64 max.
func randomAmountAbs(raw int64) int64 {
	return raw >> (uint(raw) % 60)
}

func TestComputeMemberPnL_zeroNetIn_percentReturnSkippedEquityStillReported(t *testing.T) {
	// Arrange — withdrew exactly what was deposited but still holds shares.
	pos := MemberPosition{UserID: "alex", ShareUnits: "10", AmountDeposited: 50_000_000, AmountWithdrawn: 50_000_000}

	// Act
	pnl, err := ComputeMemberPnL(pos, ShareUnits("100"), 1_000_000_000)

	// Assert
	if err != nil {
		t.Fatalf("ComputeMemberPnL: %v", err)
	}
	if pnl.NetUsdcIn != 0 {
		t.Fatalf("NetUsdcIn = %d, want 0", pnl.NetUsdcIn)
	}
	if pnl.PercentReturn != nil {
		t.Fatalf("PercentReturn = %v, want nil", *pnl.PercentReturn)
	}
	if pnl.Equity != 100_000_000 {
		t.Fatalf("Equity = %d, want 100000000", pnl.Equity)
	}
}

func TestBoards_zeroNetIn_neverRankedAndNeverDivides(t *testing.T) {
	// Arrange
	members := []MemberPosition{
		{UserID: "zero-net", ShareUnits: "10", AmountDeposited: 5_000_000, AmountWithdrawn: 5_000_000},
		{UserID: "funded", ShareUnits: "10", AmountDeposited: 5_000_000},
	}

	// Act
	inGroup, err := BuildInGroupBoard(members, ShareUnits("20"), 20_000_000)
	groupBoard := BuildGroupBoard([]GroupBoardInput{
		{GroupID: "zero", PotNav: 9_000_000, NetUsdcIn: 0},
		{GroupID: "negative", PotNav: 9_000_000, NetUsdcIn: -1},
		{GroupID: "funded", PotNav: 9_000_000, NetUsdcIn: 6_000_000},
	})
	people := BuildPeopleBoard([]PersonBoardInput{
		{UserID: "zero-net", TotalEquity: 9_000_000, TotalNetUsdcIn: 0},
		{UserID: "funded", TotalEquity: 9_000_000, TotalNetUsdcIn: 6_000_000},
	})

	// Assert
	if err != nil {
		t.Fatalf("BuildInGroupBoard: %v", err)
	}
	if len(inGroup) != 1 || inGroup[0].UserID != "funded" {
		t.Fatalf("in-group board = %+v, want only funded", inGroup)
	}
	if len(groupBoard) != 1 || groupBoard[0].GroupID != "funded" {
		t.Fatalf("group board = %+v, want only funded", groupBoard)
	}
	if len(people) != 2 || people[0].UserID != "funded" || people[1].PercentReturn != nil {
		t.Fatalf("people board = %+v, want funded first and zero-net unranked", people)
	}
}

func TestBuildGroupBoard_randomRows_dollarPnLSignMatchesPercentReturnSign(t *testing.T) {
	rng := newSeededRand(t, 59)
	for i := 0; i < 5_000; i++ {
		// Arrange
		in := GroupBoardInput{
			GroupID:   "g",
			PotNav:    USDCMicros(randomAmountAbs(rng.Int63())),
			NetUsdcIn: USDCMicros(1 + randomAmountAbs(rng.Int63())),
		}

		// Act
		rows := BuildGroupBoard([]GroupBoardInput{in})

		// Assert
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1 for net in %d", len(rows), in.NetUsdcIn)
		}
		row := rows[0]
		if (row.DollarPnL > 0) != (row.PercentReturn > 0) || (row.DollarPnL < 0) != (row.PercentReturn < 0) {
			t.Fatalf("pot %d net in %d: dollar P&L %d and percent return %v disagree in sign",
				in.PotNav, in.NetUsdcIn, row.DollarPnL, row.PercentReturn)
		}
	}
}

func TestMemberEquity_randomLedgers_neverExceedsPotAndSumsBackToIt(t *testing.T) {
	rng := newSeededRand(t, 61)
	for i := 0; i < 3_000; i++ {
		// Arrange
		n := 1 + rng.Intn(6)
		holdings := make([]int64, n)
		var total int64
		for m := range holdings {
			holdings[m] = rng.Int63n(1_000_000_000_000)
			total += holdings[m]
		}
		if total == 0 {
			continue
		}
		pot := USDCMicros(randomAmountAbs(rng.Int63()))
		totalShares, _ := ShareUnitsMicrosToDomain(total)

		// Act
		sum := new(big.Int)
		for _, held := range holdings {
			shares, _ := ShareUnitsMicrosToDomain(held)
			equity, err := MemberEquity(shares, totalShares, pot)
			if err != nil {
				t.Fatalf("MemberEquity: %v", err)
			}
			if equity < 0 || equity > pot {
				t.Fatalf("equity %d outside [0, pot %d]", equity, pot)
			}
			sum.Add(sum, big.NewInt(int64(equity)))
		}

		// Assert — each equity is within half a micro of exact, so the sum is within n/2.
		diff := sum.Sub(sum, big.NewInt(int64(pot)))
		if slack := big.NewInt(int64(n+1) / 2); diff.CmpAbs(slack) > 0 {
			t.Fatalf("%d member equities differ from pot %d by %s (slack %s)", n, pot, diff, slack)
		}
	}
}
