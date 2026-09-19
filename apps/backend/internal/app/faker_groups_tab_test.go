package app

import (
	"context"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// #153 x #148: the Groups tab lists faker scale clubs read-only (search, leaderboard, P&L
// history) without Privy, and ghost faker rows in a real club never count as money in:
// not in the directory net-in, not in snapshot net contributed, not in legacy contribution events.
func TestFakerGroupsTab_scaleClubReadOnlyAndGhostsNotMoneyIn(t *testing.T) {
	fx := newSpectatorFixture(t)
	ctx := context.Background()
	tab := NewGroupsTabService(fx.home, fx.h.Store)

	// Arrange: the ghost's seeded deposit is a confirmed ledger row that never reached the pot.
	execSQL(t, fx.h.DB, `INSERT INTO deposits (user_id, group_id, amount, from_address, status, tx_signature) VALUES ($1, $2, 2000000, 'faker-wallet-ghost', 'confirmed', $3)`,
		fx.ghostID, fx.realGroupID, "faker-sig-ghost-"+fx.h.ISO.Suffix())

	// Directory net-in (leaderboard candidate filter) counts the operator's 10 USDC only.
	dir, found, err := fx.h.Store.GetGroupDirectoryRow(ctx, fx.realGroupID)
	if err != nil || !found {
		t.Fatalf("GetGroupDirectoryRow(real) found=%v err=%v", found, err)
	}
	if dir.NetUsdcInMicros != 10_000_000 {
		t.Errorf("directory net in = %d, want 10000000 (ghost excluded)", dir.NetUsdcInMicros)
	}

	// Legacy P&L estimation never undoes a ghost deposit.
	events, err := fx.h.Store.ListContributionEventsAfter(ctx, []string{fx.realGroupID}, time.Time{})
	if err != nil {
		t.Fatalf("ListContributionEventsAfter: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("contribution events = %+v, want none (ghost deposit excluded)", events)
	}

	// A real snapshot records pot-backed net contributed, matching the live net-in.
	tx, err := fx.h.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	snap, err := fx.h.Store.InsertNavSnapshotTx(ctx, tx, fx.realGroupID, postgres.NavSnapshotReasonDeposit, postgres.NavSnapshotValues{
		PotNavMicros: 10_000_000, NavPerShareMicros: 1_000_000, TotalShares: 10_000_000,
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("InsertNavSnapshotTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if snap.NetContributedMicros == nil || *snap.NetContributedMicros != 10_000_000 {
		t.Errorf("snapshot net contributed = %v, want 10000000 (ghost excluded)", snap.NetContributedMicros)
	}
	realSeries, err := tab.GroupPnLHistory(ctx, fx.operatorToken, fx.realGroupID, GroupPnLRange1W)
	if err != nil {
		t.Fatalf("GroupPnLHistory(real): %v", err)
	}
	for _, p := range realSeries.Points {
		if p.NetInMicros != 10_000_000 {
			t.Errorf("real P&L point net in = %d at %s, want 10000000", p.NetInMicros, p.At)
		}
	}

	// Act: a non-member finds the scale club, ranks the board, and reads its P&L history.
	found2, err := tab.SearchGroups(ctx, fx.operatorToken, "Scale "+fx.h.ISO.Suffix(), GroupsTabMaxLimit, "")
	if err != nil {
		t.Fatalf("SearchGroups: %v", err)
	}
	if len(found2.Groups) != 1 || found2.Groups[0].GroupID != fx.fakerGroupID || found2.Groups[0].IsJoined {
		t.Fatalf("search = %+v, want the scale club, not joined", found2.Groups)
	}
	if _, err := tab.Leaderboard(ctx, fx.operatorToken, GroupsTabMaxLimit); err != nil {
		t.Fatalf("Leaderboard: %v", err)
	}
	if _, err := tab.GroupPnLHistory(ctx, fx.operatorToken, fx.fakerGroupID, GroupPnLRange1W); err != nil {
		t.Fatalf("GroupPnLHistory(scale): %v", err)
	}

	// Assert: valuing the scale club never reached Privy.
	fx.assertNoFakerPrivy(t)
}
