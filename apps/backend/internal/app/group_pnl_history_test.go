package app

import (
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

var pnlT0 = time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)

func micros(usd float64) int64 { return int64(usd * 1_000_000) }

func netPtr(v int64) *int64 { return &v }

func snapAt(id string, at time.Time, pot int64, net *int64) postgres.NavSnapshotRow {
	return postgres.NavSnapshotRow{ID: id, GroupID: "g", CreatedAt: at, PotNavMicros: pot, NetContributedMicros: net}
}

type wantPoint struct {
	at     time.Time
	pot    int64
	net    int64
	pnl    int64
	reason string
}

func assertPoints(t *testing.T, got []GroupPnLPoint, want []wantPoint) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("points = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if !g.At.Equal(w.at) || g.PotNavMicros != w.pot || g.NetInMicros != w.net || g.DollarPnLMicros != w.pnl {
			t.Fatalf("point %d (%s) = {at %s pot %d net %d pnl %d}, want {at %s pot %d net %d pnl %d}",
				i, w.reason, g.At.Format(time.RFC3339), g.PotNavMicros, g.NetInMicros, g.DollarPnLMicros,
				w.at.Format(time.RFC3339), w.pot, w.net, w.pnl)
		}
	}
}

func TestAssembleGroupPnLPoints_fundBuyPriceMoveWithdrawal(t *testing.T) {
	// Arrange: Ada funds $100, the cabal buys $60 of stock (snapshot marks at
	// cost), the stock rises 25%, Ada takes $23 out. Live pot = $17 cash + $75 stock.
	fund := pnlT0
	buy := pnlT0.Add(1 * time.Hour)
	payout := pnlT0.Add(3 * time.Hour)
	now := pnlT0.Add(4 * time.Hour)
	in := pnlSeriesInput{
		Since: pnlT0.Add(-24 * time.Hour),
		Snapshots: []postgres.NavSnapshotRow{
			snapAt("s1", fund, micros(100), netPtr(micros(100))),
			snapAt("s2", buy, micros(100), netPtr(micros(100))),
			snapAt("s3", payout, micros(77), netPtr(micros(77))),
		},
		Live: groupValuation{PotNavMicros: micros(92), NetUsdcInMicros: micros(77), ValuedAt: now},
	}

	// Act
	points := assembleGroupPnLPoints(in)

	// Assert
	assertPoints(t, points, []wantPoint{
		{fund, micros(100), micros(100), 0, "funded"},
		{buy, micros(100), micros(100), 0, "bought at cost"},
		{payout, micros(77), micros(77), 0, "paid out at cost basis"},
		{now, micros(92), micros(77), micros(15), "live mark shows the 25% move"},
	})
}

func TestAssembleGroupPnLPoints_baselineBeforeWindowCarriesToWindowStart(t *testing.T) {
	// Arrange
	since := pnlT0
	in := pnlSeriesInput{
		Since: since,
		Snapshots: []postgres.NavSnapshotRow{
			snapAt("old", since.Add(-10*24*time.Hour), micros(120), netPtr(micros(100))),
			snapAt("new", since.Add(2*time.Hour), micros(130), netPtr(micros(110))),
		},
		Live: groupValuation{PotNavMicros: micros(135), NetUsdcInMicros: micros(110), ValuedAt: since.Add(5 * time.Hour)},
	}

	// Act
	points := assembleGroupPnLPoints(in)

	// Assert
	assertPoints(t, points, []wantPoint{
		{since, micros(120), micros(100), micros(20), "baseline carried to window start"},
		{since.Add(2 * time.Hour), micros(130), micros(110), micros(20), "in window"},
		{since.Add(5 * time.Hour), micros(135), micros(110), micros(25), "live"},
	})
}

func TestAssembleGroupPnLPoints_legacySnapshotsEstimateNetFromLedger(t *testing.T) {
	// Arrange: two snapshots from before net contributed was recorded. Between
	// them Ada deposited $50; after the second, Ben deposited $30 and Ada took
	// $20 out. Live net in is $160.
	s1 := pnlT0
	s2 := pnlT0.Add(2 * time.Hour)
	in := pnlSeriesInput{
		Since: pnlT0.Add(-time.Hour),
		Snapshots: []postgres.NavSnapshotRow{
			snapAt("s1", s1, micros(100), nil),
			snapAt("s2", s2, micros(150), nil),
		},
		Events: []postgres.ContributionEvent{
			{GroupID: "g", At: s1.Add(-time.Minute), AmountMicros: micros(100)},
			{GroupID: "g", At: s1.Add(30 * time.Minute), AmountMicros: micros(50)},
			{GroupID: "g", At: s2.Add(10 * time.Minute), AmountMicros: micros(30)},
			{GroupID: "g", At: s2.Add(20 * time.Minute), AmountMicros: -micros(20)},
		},
		Live: groupValuation{PotNavMicros: micros(170), NetUsdcInMicros: micros(160), ValuedAt: s2.Add(time.Hour)},
	}

	// Act
	points := assembleGroupPnLPoints(in)

	// Assert
	assertPoints(t, points, []wantPoint{
		{s1, micros(100), micros(100), 0, "160 - 30 + 20 - 50"},
		{s2, micros(150), micros(150), 0, "160 - 30 + 20"},
		{s2.Add(time.Hour), micros(170), micros(160), micros(10), "live"},
	})
}

func TestAssembleGroupPnLPoints_exactSnapshotResetsLegacyEstimate(t *testing.T) {
	// Arrange: a legacy snapshot followed by an exact one. The estimate walks
	// back from the exact value, not from live, so a reconcile credit with no
	// ledger row after the exact snapshot cannot leak into older points.
	legacy := pnlT0
	exact := pnlT0.Add(time.Hour)
	in := pnlSeriesInput{
		Since: pnlT0.Add(-time.Hour),
		Snapshots: []postgres.NavSnapshotRow{
			snapAt("a", legacy, micros(40), nil),
			snapAt("b", exact, micros(90), netPtr(micros(90))),
		},
		Events: []postgres.ContributionEvent{
			{GroupID: "g", At: legacy.Add(10 * time.Minute), AmountMicros: micros(50)},
		},
		Live: groupValuation{PotNavMicros: micros(300), NetUsdcInMicros: micros(290), ValuedAt: exact.Add(time.Hour)},
	}

	// Act
	points := assembleGroupPnLPoints(in)

	// Assert
	if points[0].NetInMicros != micros(40) {
		t.Fatalf("legacy net = %d, want 40_000_000 (90 exact - 50 deposit)", points[0].NetInMicros)
	}
}

func TestAssembleGroupPnLPoints_ignoresEventsAfterLiveValuation(t *testing.T) {
	// Arrange: a cached live valuation is 20s old; a deposit landed after it.
	snap := pnlT0
	live := pnlT0.Add(time.Hour)
	in := pnlSeriesInput{
		Since:     pnlT0.Add(-time.Hour),
		Snapshots: []postgres.NavSnapshotRow{snapAt("a", snap, micros(100), nil)},
		Events: []postgres.ContributionEvent{
			{GroupID: "g", At: live.Add(20 * time.Second), AmountMicros: micros(500)},
		},
		Live: groupValuation{PotNavMicros: micros(110), NetUsdcInMicros: micros(100), ValuedAt: live},
	}

	// Act
	points := assembleGroupPnLPoints(in)

	// Assert
	if points[0].NetInMicros != micros(100) {
		t.Fatalf("net = %d, want 100_000_000", points[0].NetInMicros)
	}
}

func TestAssembleGroupPnLPoints_neverFundedReturnsEmpty(t *testing.T) {
	points := assembleGroupPnLPoints(pnlSeriesInput{Since: pnlT0, Live: groupValuation{ValuedAt: pnlT0}})
	if len(points) != 0 {
		t.Fatalf("points = %+v, want none", points)
	}
}

func TestAssembleGroupPnLPoints_fundedWithoutSnapshotsReturnsLivePointOnly(t *testing.T) {
	points := assembleGroupPnLPoints(pnlSeriesInput{
		Since: pnlT0,
		Live:  groupValuation{PotNavMicros: micros(10), NetUsdcInMicros: micros(10), ValuedAt: pnlT0.Add(time.Hour)},
	})
	assertPoints(t, points, []wantPoint{{pnlT0.Add(time.Hour), micros(10), micros(10), 0, "live"}})
}

func TestAssembleGroupPnLPoints_degradedValuationDropsLivePoint(t *testing.T) {
	// Arrange
	in := pnlSeriesInput{
		Since:     pnlT0.Add(-time.Hour),
		Snapshots: []postgres.NavSnapshotRow{snapAt("a", pnlT0, micros(100), netPtr(micros(90)))},
		Live:      groupValuation{PotNavMicros: micros(90), NetUsdcInMicros: micros(90), ValuedAt: pnlT0.Add(time.Hour), Degraded: true},
	}

	// Act
	points := assembleGroupPnLPoints(in)

	// Assert
	assertPoints(t, points, []wantPoint{{pnlT0, micros(100), micros(90), micros(10), "history only"}})
}

func TestAssembleGroupPnLPoints_liveBehindSnapshotClockIsClampedInOrder(t *testing.T) {
	// Arrange: database clock 2s ahead of the API server.
	snap := pnlT0.Add(2 * time.Second)
	in := pnlSeriesInput{
		Since:     pnlT0.Add(-time.Hour),
		Snapshots: []postgres.NavSnapshotRow{snapAt("a", snap, micros(10), netPtr(micros(10)))},
		Live:      groupValuation{PotNavMicros: micros(10), NetUsdcInMicros: micros(10), ValuedAt: pnlT0},
	}

	// Act
	points := assembleGroupPnLPoints(in)

	// Assert
	if points[1].At.Before(points[0].At) {
		t.Fatalf("live point %s precedes snapshot %s", points[1].At, points[0].At)
	}
}

func TestAssembleGroupPnLPoints_sortsUnorderedSnapshotsAndEmitsUTC(t *testing.T) {
	// Arrange
	pst := time.FixedZone("PST", -8*3600)
	in := pnlSeriesInput{
		Since: pnlT0.Add(-time.Hour),
		Snapshots: []postgres.NavSnapshotRow{
			snapAt("b", pnlT0.Add(time.Hour).In(pst), micros(20), netPtr(micros(20))),
			snapAt("a", pnlT0.In(pst), micros(10), netPtr(micros(10))),
		},
		Live: groupValuation{PotNavMicros: micros(20), NetUsdcInMicros: micros(20), ValuedAt: pnlT0.Add(2 * time.Hour).In(pst)},
	}

	// Act
	points := assembleGroupPnLPoints(in)

	// Assert
	for i, p := range points {
		if p.At.Location() != time.UTC {
			t.Fatalf("point %d location = %v, want UTC", i, p.At.Location())
		}
		if i > 0 && p.At.Before(points[i-1].At) {
			t.Fatalf("points out of order at %d", i)
		}
	}
}

func TestParseGroupPnLRange_acceptsKnownRangesCaseInsensitively(t *testing.T) {
	cases := map[string]GroupPnLRange{"": GroupPnLRange1M, "1d": GroupPnLRange1D, "1W": GroupPnLRange1W, " 3m ": GroupPnLRange3M}
	for raw, want := range cases {
		got, err := ParseGroupPnLRange(raw)
		if err != nil || got != want {
			t.Fatalf("ParseGroupPnLRange(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, bad := range []string{"ALL", "1Y", "90", "1H"} {
		if _, err := ParseGroupPnLRange(bad); err == nil {
			t.Fatalf("ParseGroupPnLRange(%q) accepted", bad)
		}
	}
}

func TestGroupPnLRange_windowNeverExceedsNinetyDays(t *testing.T) {
	for _, r := range []GroupPnLRange{GroupPnLRange1D, GroupPnLRange1W, GroupPnLRange1M, GroupPnLRange3M, "bogus"} {
		if r.Window() > GroupPnLMaxWindow {
			t.Fatalf("%s window %s exceeds cap", r, r.Window())
		}
	}
	if GroupPnLRange3M.Window() != 90*24*time.Hour {
		t.Fatalf("3M window = %s", GroupPnLRange3M.Window())
	}
}
