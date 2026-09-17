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

func TestInGroupViewBoard_includesMembersWithoutDepositOrPnL(t *testing.T) {
	// Arrange
	members := []MemberPosition{
		buildPosition(func(p *MemberPosition) {
			p.UserID = "alice"
			p.ShareUnits = ShareUnits("100")
			p.AmountDeposited = 100_000_000
		}),
		buildPosition(func(p *MemberPosition) {
			p.UserID = "bob"
			p.ShareUnits = ShareUnits("0")
			p.AmountDeposited = 0
		}),
	}
	totalShares := ShareUnits("100")
	potNav := USDCMicros(110_000_000)

	// Act
	board, err := BuildInGroupViewBoard(members, totalShares, potNav)

	// Assert
	if err != nil {
		t.Fatalf("BuildInGroupViewBoard: %v", err)
	}
	if len(board) != 2 {
		t.Fatalf("board len = %d, want 2", len(board))
	}
	if board[0].UserID != "alice" {
		t.Fatalf("board[0].UserID = %q, want alice", board[0].UserID)
	}
	if board[1].UserID != "bob" {
		t.Fatalf("board[1].UserID = %q, want bob", board[1].UserID)
	}
	if board[1].PercentReturn != nil {
		t.Fatalf("bob percent return = %v, want nil", *board[1].PercentReturn)
	}
	if board[1].NetUsdcIn != 0 || board[1].Equity != 0 {
		t.Fatalf("bob pnl = equity %d netIn %d, want zeros", board[1].Equity, board[1].NetUsdcIn)
	}
}

func TestComputeMemberPnL_depositOnlyPot_zeroReturn(t *testing.T) {
	// Arrange — two $0.20 deposits, no price move
	pos := MemberPosition{
		UserID:          "solo",
		ShareUnits:      ShareUnits("0.4"),
		AmountDeposited: 400_000,
	}
	totalShares := ShareUnits("0.4")
	potNav := USDCMicros(400_000)

	// Act
	pnl, err := ComputeMemberPnL(pos, totalShares, potNav)

	// Assert
	if err != nil {
		t.Fatalf("ComputeMemberPnL: %v", err)
	}
	if pnl.Equity != 400_000 {
		t.Fatalf("equity = %d, want 400000", pnl.Equity)
	}
	if pnl.NetUsdcIn != 400_000 {
		t.Fatalf("netUsdcIn = %d, want 400000", pnl.NetUsdcIn)
	}
	if pnl.PercentReturn == nil {
		t.Fatal("PercentReturn: expected non-nil")
	}
	if *pnl.PercentReturn != 0 {
		t.Fatalf("percent return = %v, want 0", *pnl.PercentReturn)
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
