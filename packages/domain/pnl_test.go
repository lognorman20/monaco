package domain

import "testing"

func buildPosition(overrides func(*MemberPosition)) MemberPosition {
	pos := MemberPosition{
		UserID:          "user-1",
		ShareUnits:      ShareUnits("100"),
		AmountDeposited: 100_000_000,
		AmountWithdrawn: 0,
	}
	if overrides != nil {
		overrides(&pos)
	}
	return pos
}

func TestMemberEquity_matchesShareFractionTimesPotNav(t *testing.T) {
	// Arrange
	memberShares := ShareUnits("50")
	totalShares := ShareUnits("100")
	potNav := USDCMicros(110_000_000)

	// Act
	equity, err := MemberEquity(memberShares, totalShares, potNav)

	// Assert
	if err != nil {
		t.Fatalf("MemberEquity: %v", err)
	}
	if equity != 55_000_000 {
		t.Fatalf("equity = %d, want %d", equity, 55_000_000)
	}
}

func TestPercentReturn_equityOverNetInMinusOne(t *testing.T) {
	// Arrange
	equity := USDCMicros(110_000_000)
	netIn := USDCMicros(100_000_000)

	// Act
	pct := PercentReturn(equity, netIn)

	// Assert
	if pct == nil {
		t.Fatal("PercentReturn: expected non-nil")
	}
	if *pct < 0.099 || *pct > 0.101 {
		t.Fatalf("percent return = %v, want ~0.10", *pct)
	}
}

func TestPercentReturn_zeroNetIn_returnsNil(t *testing.T) {
	// Arrange
	equity := USDCMicros(100_000_000)

	// Act
	pct := PercentReturn(equity, 0)

	// Assert
	if pct != nil {
		t.Fatalf("PercentReturn: got %v, want nil", *pct)
	}
}

func TestBoards_skipRowsWhenNetUsdcInZero(t *testing.T) {
	// Arrange
	members := []MemberPosition{
		buildPosition(func(p *MemberPosition) {
			p.UserID = "alice"
			p.ShareUnits = ShareUnits("100")
			p.AmountDeposited = 100_000_000
		}),
		buildPosition(func(p *MemberPosition) {
			p.UserID = "bob"
			p.ShareUnits = ShareUnits("100")
			p.AmountDeposited = 100_000_000
			p.AmountWithdrawn = 100_000_000
		}),
	}
	totalShares := ShareUnits("200")
	potNav := USDCMicros(220_000_000)

	// Act
	board, err := BuildInGroupBoard(members, totalShares, potNav)

	// Assert
	if err != nil {
		t.Fatalf("BuildInGroupBoard: %v", err)
	}
	if len(board) != 1 {
		t.Fatalf("board len = %d, want 1", len(board))
	}
	if board[0].UserID != "alice" {
		t.Fatalf("board[0].UserID = %q, want alice", board[0].UserID)
	}
}

func TestInGroupBoard_fullExit_dropsMemberFromBoard(t *testing.T) {
	// Arrange
	members := []MemberPosition{
		buildPosition(func(p *MemberPosition) {
			p.UserID = "alex"
			p.ShareUnits = ShareUnits("0")
			p.AmountDeposited = 100_000_000
			p.AmountWithdrawn = 110_000_000
		}),
		buildPosition(func(p *MemberPosition) {
			p.UserID = "blair"
			p.ShareUnits = ShareUnits("100")
			p.AmountDeposited = 110_000_000
		}),
	}
	totalShares := ShareUnits("100")
	potNav := USDCMicros(110_000_000)

	// Act
	board, err := BuildInGroupBoard(members, totalShares, potNav)

	// Assert
	if err != nil {
		t.Fatalf("BuildInGroupBoard: %v", err)
	}
	if len(board) != 1 {
		t.Fatalf("board len = %d, want 1", len(board))
	}
	if board[0].UserID != "blair" {
		t.Fatalf("board[0].UserID = %q, want blair", board[0].UserID)
	}
	for _, row := range board {
		if row.UserID == "alex" {
			t.Fatal("full-exit member alex must be dropped from in-group board")
		}
	}
}
