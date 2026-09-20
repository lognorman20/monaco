package app

import (
	"context"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"testing"
)

func TestHomeDiscoveryNeedsMarkedPot(t *testing.T) {
	t.Parallel()
	if homeDiscoveryNeedsMarkedPot(false, 0) {
		t.Fatal("unjoined unfunded discovery row must skip marked pot")
	}
	if !homeDiscoveryNeedsMarkedPot(true, 0) {
		t.Fatal("joined groups must still mark pot for people board")
	}
	if !homeDiscoveryNeedsMarkedPot(false, 1) {
		t.Fatal("funded discovery rows need marked pot for ranking")
	}
}

func TestGetHomeDashboard_computesPotNavOncePerJoinedGroup(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Auth, h.Wallets, h.Pyth, h.Deposits, h.Symbols)
	ctx := HomeContextWithPotNavCache(context.Background())

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Auth, h.Wallets), h.Auth, "dash-dedup", "Dash Dedup")
	token := string(auth.AccessToken(h.ISO.UniqueToken("dash-dedup")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "dash-dedup"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	if err := insertGroupMember(t, ctx, h.Store, group.GroupID, session.UserID); err != nil {
		t.Fatalf("insertGroupMember: %v", err)
	}

	const depositMicros = int64(100_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, depositMicros, depositMicros); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, depositMicros)

	if _, err := home.GetHomeDashboard(ctx, token, HomeLeaderboardRangeALL); err != nil {
		t.Fatalf("GetHomeDashboard: %v", err)
	}
	if got := HomePotNavComputeCount(ctx); got != 1 {
		t.Fatalf("groupPotNavAndShares computes = %d, want 1 for one joined group on ALL dashboard", got)
	}
}
