package app

import (
	"testing"

	"github.com/monaco/monaco/packages/domain"
)

func TestInGroupBoard_ranksByPercentReturnNotDollars(t *testing.T) {
	// Arrange
	members := []domain.MemberPosition{
		{
			UserID:          "alice",
			ShareUnits:      domain.ShareUnits("10"),
			AmountDeposited: 100_000_000,
		},
		{
			UserID:          "bob",
			ShareUnits:      domain.ShareUnits("90"),
			AmountDeposited: 1_000_000_000,
		},
	}
	totalShares := domain.ShareUnits("100")
	potNav := domain.PotNAV{TotalUsdc: 1_200_000_000}

	// Act
	board, err := BuildInGroupMemberBoard(members, totalShares, potNav)

	// Assert
	if err != nil {
		t.Fatalf("BuildInGroupMemberBoard: %v", err)
	}
	if len(board) != 2 {
		t.Fatalf("board len = %d, want 2", len(board))
	}
	if board[0].UserID != "alice" {
		t.Fatalf("board[0].UserID = %q, want alice (higher %% return)", board[0].UserID)
	}
	if board[1].UserID != "bob" {
		t.Fatalf("board[1].UserID = %q, want bob", board[1].UserID)
	}
	if board[0].Equity >= board[1].Equity {
		t.Fatal("expected alice to have lower dollar equity than bob")
	}
	if *board[0].PercentReturn <= *board[1].PercentReturn {
		t.Fatal("expected alice percent return to beat bob despite lower dollar equity")
	}
}

func TestGroupBoard_ranksPotsByPercentReturn(t *testing.T) {
	// Arrange
	groups := []domain.GroupBoardInput{
		{
			GroupID:   "group-a",
			GroupName: "Alpha",
			PotNav:    150_000_000,
			NetUsdcIn: 100_000_000,
		},
		{
			GroupID:   "group-b",
			GroupName: "Beta",
			PotNav:    550_000_000,
			NetUsdcIn: 500_000_000,
		},
	}

	// Act
	board := BuildAppGroupBoard(groups)

	// Assert
	if len(board) != 2 {
		t.Fatalf("board len = %d, want 2", len(board))
	}
	if board[0].GroupID != "group-a" {
		t.Fatalf("board[0].GroupID = %q, want group-a", board[0].GroupID)
	}
	if board[0].PotNav >= board[1].PotNav {
		t.Fatal("expected group-b to have higher dollar pot than group-a")
	}
	if board[0].PercentReturn <= board[1].PercentReturn {
		t.Fatal("expected group-a percent return to beat group-b")
	}
}

func TestPeopleBoard_aggregatesCrossGroupNetInAndEquity(t *testing.T) {
	// Arrange
	alexGroupA := domain.MemberPnL{
		UserID:    "alex",
		Equity:    110_000_000,
		NetUsdcIn: 100_000_000,
	}
	alexGroupB := domain.MemberPnL{
		UserID:    "alex",
		Equity:    55_000_000,
		NetUsdcIn: 50_000_000,
	}
	blairGroupA := domain.MemberPnL{
		UserID:    "blair",
		Equity:    100_000_000,
		NetUsdcIn: 100_000_000,
	}

	// Act
	alex := AggregateCrossGroupPerson([]domain.MemberPnL{alexGroupA, alexGroupB})
	blair := AggregateCrossGroupPerson([]domain.MemberPnL{blairGroupA})
	board := BuildAppPeopleBoard([]domain.PersonBoardInput{alex, blair})

	// Assert
	if alex.TotalEquity != 165_000_000 {
		t.Fatalf("alex total equity = %d, want %d", alex.TotalEquity, 165_000_000)
	}
	if alex.TotalNetUsdcIn != 150_000_000 {
		t.Fatalf("alex total net in = %d, want %d", alex.TotalNetUsdcIn, 150_000_000)
	}
	if len(board) != 2 {
		t.Fatalf("board len = %d, want 2", len(board))
	}
	if board[0].UserID != "alex" {
		t.Fatalf("board[0].UserID = %q, want alex", board[0].UserID)
	}
	if board[0].PercentReturn < 0.099 || board[0].PercentReturn > 0.101 {
		t.Fatalf("alex percent return = %v, want ~0.10", board[0].PercentReturn)
	}
}
