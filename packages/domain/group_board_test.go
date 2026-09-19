package domain

import (
	"math"
	"testing"
)

func TestBuildGroupBoard_excludesZeroAndNegativeNetIn(t *testing.T) {
	// Arrange
	groups := []GroupBoardInput{
		{GroupID: "funded", GroupName: "Weekend investors", PotNav: 110_000_000, NetUsdcIn: 100_000_000},
		{GroupID: "empty", GroupName: "Fresh cabal", PotNav: 0, NetUsdcIn: 0},
		{GroupID: "cashed-out", GroupName: "Took profits", PotNav: 5_000_000, NetUsdcIn: -20_000_000},
	}

	// Act
	board := BuildGroupBoard(groups)

	// Assert
	if len(board) != 1 || board[0].GroupID != "funded" {
		t.Fatalf("board = %+v, want only the funded cabal", board)
	}
}

func TestBuildGroupBoard_equalPercentBreaksOnDollarPnLThenPotThenID(t *testing.T) {
	// Arrange: all three return exactly +10%.
	groups := []GroupBoardInput{
		{GroupID: "c-small", GroupName: "Small", PotNav: 11_000_000, NetUsdcIn: 10_000_000},
		{GroupID: "b-big", GroupName: "Big", PotNav: 110_000_000, NetUsdcIn: 100_000_000},
		{GroupID: "a-small", GroupName: "Small twin", PotNav: 11_000_000, NetUsdcIn: 10_000_000},
	}

	// Act
	board := BuildGroupBoard(groups)

	// Assert
	got := []string{board[0].GroupID, board[1].GroupID, board[2].GroupID}
	want := []string{"b-big", "a-small", "c-small"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestBuildGroupBoard_orderIsIndependentOfInputOrder(t *testing.T) {
	// Arrange
	base := []GroupBoardInput{
		{GroupID: "g1", PotNav: 90_000_000, NetUsdcIn: 100_000_000},
		{GroupID: "g2", PotNav: 150_000_000, NetUsdcIn: 100_000_000},
		{GroupID: "g3", PotNav: 100_000_000, NetUsdcIn: 100_000_000},
		{GroupID: "g4", PotNav: 300_000_000, NetUsdcIn: 200_000_000},
	}
	reversed := make([]GroupBoardInput, len(base))
	for i := range base {
		reversed[len(base)-1-i] = base[i]
	}

	// Act
	a := BuildGroupBoard(base)
	b := BuildGroupBoard(reversed)

	// Assert
	for i := range a {
		if a[i].GroupID != b[i].GroupID {
			t.Fatalf("order differs at %d: %s vs %s", i, a[i].GroupID, b[i].GroupID)
		}
	}
	if a[0].GroupID != "g4" || a[1].GroupID != "g2" {
		t.Fatalf("top two = %s, %s; want g4 (+50%%, +$100) then g2 (+50%%, +$50)", a[0].GroupID, a[1].GroupID)
	}
}

func TestMulDivFloor_handlesProductsBeyondInt64(t *testing.T) {
	// Arrange: $100k of USDC micros times 8-decimal atomics would wrap int64.
	usdcMicros := int64(100_000 * 1_000_000)
	atomics := int64(100_000_000)

	// Act
	got, err := MulDivFloor(usdcMicros, atomics, 50*atomics)

	// Assert
	if err != nil {
		t.Fatalf("MulDivFloor: %v", err)
	}
	if got != 2_000_000_000 {
		t.Fatalf("got %d, want 2_000_000_000", got)
	}
}

func TestMulDivFloor_floorsTowardZero(t *testing.T) {
	got, err := MulDivFloor(10, 10, 3)
	if err != nil || got != 33 {
		t.Fatalf("got %d, %v; want 33", got, err)
	}
}

func TestMulDivFloor_rejectsInvalidInputsAndOverflow(t *testing.T) {
	cases := []struct {
		name    string
		a, b, c int64
	}{
		{"negative a", -1, 2, 3},
		{"negative b", 1, -2, 3},
		{"zero divisor", 1, 2, 0},
		{"result overflow", math.MaxInt64, 2, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := MulDivFloor(tc.a, tc.b, tc.c); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}
