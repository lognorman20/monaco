package app

import (
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

func TestParseHomeLeaderboardRange_acceptsKnownValues(t *testing.T) {
	t.Parallel()

	cases := map[string]HomeLeaderboardRange{
		"":     HomeLeaderboardRangeALL,
		"ALL":  HomeLeaderboardRangeALL,
		"1h":   HomeLeaderboardRange1H,
		"1D":   HomeLeaderboardRange1D,
		"1W":   HomeLeaderboardRange1W,
		"1M":   HomeLeaderboardRange1M,
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
