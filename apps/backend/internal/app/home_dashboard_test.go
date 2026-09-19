package app

import (
	"context"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func TestParseHomeLeaderboardRange_acceptsKnownValues(t *testing.T) {
	t.Parallel()

	cases := map[string]HomeLeaderboardRange{
		"":    HomeLeaderboardRangeALL,
		"ALL": HomeLeaderboardRangeALL,
		"1h":  HomeLeaderboardRange1H,
		"1D":  HomeLeaderboardRange1D,
		"1W":  HomeLeaderboardRange1W,
		"1M":  HomeLeaderboardRange1M,
	}
	for raw, want := range cases {
		got, err := ParseHomeLeaderboardRange(raw)
		if err != nil {
			t.Fatalf("ParseHomeLeaderboardRange(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseHomeLeaderboardRange(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseHomeLeaderboardRange_rejectsUnknown(t *testing.T) {
	t.Parallel()
	if _, err := ParseHomeLeaderboardRange("YTD"); err == nil {
		t.Fatal("expected error for unknown range")
	}
}

func TestMemberEquityAtSnapshot_usesShareFraction(t *testing.T) {
	t.Parallel()
	snap := postgres.NavSnapshotRow{
		PotNavMicros: 1_000_000,
		TotalShares:  1_000_000,
	}
	got := memberEquityAtSnapshot(500_000, snap)
	if got != 500_000 {
		t.Fatalf("equity = %d, want 500000", got)
	}
}

func TestLatestSnapshotAtOrBefore_picksLatestEligible(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	snapshots := []postgres.NavSnapshotRow{
		{CreatedAt: base.Add(-2 * time.Hour), PotNavMicros: 100},
		{CreatedAt: base.Add(-30 * time.Minute), PotNavMicros: 200},
		{CreatedAt: base.Add(10 * time.Minute), PotNavMicros: 300},
	}
	got, ok := latestSnapshotAtOrBefore(snapshots, base)
	if !ok {
		t.Fatal("expected snapshot")
	}
	if got.PotNavMicros != 200 {
		t.Fatalf("pot nav = %d, want 200", got.PotNavMicros)
	}
}

func TestFormatMyGroups_aggregatesNetWorth(t *testing.T) {
	t.Parallel()
	positions := []viewerGroupPosition{
		{
			GroupID:          "g1",
			Name:             "Alpha",
			ShareUnitsMicro:  1_000_000,
			NetUsdcInMicro:   900_000,
			EquityMicro:      1_000_000,
			TotalSharesMicro: 1_000_000,
		},
		{
			GroupID:          "g2",
			Name:             "Beta",
			ShareUnitsMicro:  500_000,
			NetUsdcInMicro:   400_000,
			EquityMicro:      450_000,
			TotalSharesMicro: 1_000_000,
		},
	}
	myGroups, netEquity, netDeposits := formatMyGroups(positions)
	if len(myGroups) != 2 {
		t.Fatalf("myGroups len = %d, want 2", len(myGroups))
	}
	if netEquity != 1_450_000 {
		t.Fatalf("netEquity = %d, want 1450000", netEquity)
	}
	if netDeposits != 1_300_000 {
		t.Fatalf("netDeposits = %d, want 1300000", netDeposits)
	}
	if myGroups[0].EquityUsd != "1.00" {
		t.Fatalf("equityUsd = %q, want 1.00", myGroups[0].EquityUsd)
	}
}

func TestBuildViewerPnLSeries_noInWindowSnapshots_returnsEmpty(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)
	ctx := context.Background()

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "pnl-empty", "PnL Empty")
	token := string(privy.AccessToken(h.ISO.UniqueToken("pnl-empty")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "pnl-empty"))
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

	positions, err := home.viewerGroupPositions(ctx, session.UserID, []string{group.GroupID})
	if err != nil {
		t.Fatalf("viewerGroupPositions: %v", err)
	}
	series, err := home.buildViewerPnLSeries(ctx, positions, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("buildViewerPnLSeries: %v", err)
	}
	if len(series) != 0 {
		t.Fatalf("series len = %d, want 0 (no duplicate live-only points)", len(series))
	}
}

func TestBuildViewerPnLSeries_oldSnapshot_includesWindowStartAndNow(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)
	ctx := context.Background()

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "pnl-old", "PnL Old")
	token := string(privy.AccessToken(h.ISO.UniqueToken("pnl-old")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "pnl-old"))
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
	insertNavSnapshotAt(t, ctx, h, group.GroupID, time.Now().UTC().Add(-3*time.Hour), depositMicros, depositMicros)

	positions, err := home.viewerGroupPositions(ctx, session.UserID, []string{group.GroupID})
	if err != nil {
		t.Fatalf("viewerGroupPositions: %v", err)
	}
	series, err := home.buildViewerPnLSeries(ctx, positions, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("buildViewerPnLSeries: %v", err)
	}
	if len(series) < 2 {
		t.Fatalf("series len = %d, want at least 2 (window start + now)", len(series))
	}
}

func TestBuildRangedLeaderboard_noWindowBaseline_excludesLifetimeRanking(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)
	ctx := context.Background()

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "lb-no-base", "No Baseline")
	token := string(privy.AccessToken(h.ISO.UniqueToken("lb-no-base")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "lb-no-base"))
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

	section, err := home.buildRangedLeaderboard(ctx, []string{group.GroupID}, HomeLeaderboardRange1D)
	if err != nil {
		t.Fatalf("buildRangedLeaderboard: %v", err)
	}
	if len(section.People) != 0 {
		t.Fatalf("people len = %d, want 0 without window baseline snapshot", len(section.People))
	}
}

func TestBuildRangedLeaderboard_withWindowBaseline_usesWindowDeltaNotLifetime(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)
	ctx := context.Background()

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "lb-window", "Window Delta")
	token := string(privy.AccessToken(h.ISO.UniqueToken("lb-window")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "lb-window"))
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

	windowStart := time.Now().UTC().Add(-48 * time.Hour)
	insertNavSnapshotAt(t, ctx, h, group.GroupID, windowStart, depositMicros, depositMicros)

	section, err := home.buildRangedLeaderboard(ctx, []string{group.GroupID}, HomeLeaderboardRange1D)
	if err != nil {
		t.Fatalf("buildRangedLeaderboard: %v", err)
	}
	if len(section.People) != 1 {
		t.Fatalf("people len = %d, want 1", len(section.People))
	}
	row := section.People[0]
	if row.DollarPnL != "+0.00" {
		t.Fatalf("dollarPnL = %q, want +0.00 window delta (not lifetime +100.00)", row.DollarPnL)
	}
	if row.PercentReturn == nil || *row.PercentReturn != "0" {
		t.Fatalf("percentReturn = %v, want 0 window return", row.PercentReturn)
	}
}

func insertNavSnapshotAt(t *testing.T, ctx context.Context, h integrationHarness, groupID string, createdAt time.Time, potNavMicros, totalShares int64) {
	t.Helper()
	_, err := h.DB.ExecContext(ctx, `
INSERT INTO nav_snapshots (group_id, pot_nav_micros, nav_per_share_micros, total_shares, reason, created_at)
VALUES ($1, $2, $3, $4, 'deposit', $5)`,
		groupID, potNavMicros, 1_000_000, totalShares, createdAt.UTC())
	if err != nil {
		t.Fatalf("insert nav snapshot: %v", err)
	}
}
