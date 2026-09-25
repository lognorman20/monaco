package app

import (
	"database/sql"
	"math"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// Monday 21 September 2026, 00:00 UTC.
var matchupMonday = time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)

func candidate(id string, potUSD float64) matchupCandidate {
	return matchupCandidate{GroupID: id, PotMicros: micros(potUSD), NetInMicros: micros(potUSD)}
}

func pairIDs(plan []plannedMatchup) [][2]string {
	out := make([][2]string, 0, len(plan))
	for _, p := range plan {
		b := ""
		if p.B != nil {
			b = p.B.GroupID
		}
		out = append(out, [2]string{p.A.GroupID, b})
	}
	return out
}

func assertPairs(t *testing.T, plan []plannedMatchup, want [][2]string) {
	t.Helper()
	got := pairIDs(plan)
	if len(got) != len(want) {
		t.Fatalf("pairs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pairs = %v, want %v", got, want)
		}
	}
}

func TestMatchupWeekStart_isMondayMidnightUTC(t *testing.T) {
	cases := map[string]time.Time{
		"monday midnight":         matchupMonday,
		"monday afternoon":        matchupMonday.Add(15 * time.Hour),
		"wednesday":               matchupMonday.Add(2*24*time.Hour + 9*time.Hour),
		"sunday last second":      matchupMonday.Add(MatchupWeek - time.Second),
		"sunday evening in tokyo": time.Date(2026, 9, 27, 23, 30, 0, 0, time.FixedZone("JST", 9*3600)),
	}
	for name, at := range cases {
		t.Run(name, func(t *testing.T) {
			if got := MatchupWeekStart(at); !got.Equal(matchupMonday) {
				t.Fatalf("MatchupWeekStart(%s) = %s, want %s", at, got, matchupMonday)
			}
		})
	}
	if got := MatchupWeekStart(matchupMonday.Add(MatchupWeek)); !got.Equal(matchupMonday.Add(MatchupWeek)) {
		t.Fatalf("next Monday starts its own week, got %s", got)
	}
}

func TestPlanMatchups_evenCountPairsNeighboursByPot(t *testing.T) {
	// Arrange: shuffled input; pots decide the order, not the list.
	cands := []matchupCandidate{
		candidate("c", 300), candidate("a", 5000), candidate("d", 120), candidate("b", 4200),
	}

	// Act
	plan := planMatchups(cands, nil, matchupHistory{})

	// Assert: largest with second largest, third with fourth, no bye.
	assertPairs(t, plan, [][2]string{{"a", "b"}, {"c", "d"}})
}

func TestPlanMatchups_oddCountGivesTheSmallestPotABye(t *testing.T) {
	// Arrange
	cands := []matchupCandidate{
		candidate("a", 900), candidate("b", 800), candidate("c", 700), candidate("d", 600), candidate("e", 12),
	}

	// Act
	plan := planMatchups(cands, nil, matchupHistory{})

	// Assert
	assertPairs(t, plan, [][2]string{{"a", "b"}, {"c", "d"}, {"e", ""}})
}

func TestPlanMatchups_byeSkipsWhoeverSatOutLastWeek(t *testing.T) {
	// Arrange: e had the bye last week, so d (next smallest) sits out and e plays c.
	cands := []matchupCandidate{
		candidate("a", 900), candidate("b", 800), candidate("c", 700), candidate("d", 600), candidate("e", 12),
	}
	last := matchupHistory{HadBye: map[string]bool{"e": true}}

	// Act
	plan := planMatchups(cands, nil, last)

	// Assert
	assertPairs(t, plan, [][2]string{{"a", "b"}, {"c", "e"}, {"d", ""}})
}

func TestPlanMatchups_avoidsLastWeeksOpponentWhenANeighbourIsFree(t *testing.T) {
	// Arrange: a and b played last week.
	cands := []matchupCandidate{
		candidate("a", 900), candidate("b", 800), candidate("c", 700), candidate("d", 600),
	}
	last := matchupHistory{LastOpponent: map[string]string{"a": "b", "b": "a", "c": "d", "d": "c"}}

	// Act
	plan := planMatchups(cands, nil, last)

	// Assert: a skips b for the next pot down; b takes d.
	assertPairs(t, plan, [][2]string{{"a", "c"}, {"b", "d"}})
}

func TestPlanMatchups_rematchOnlyWhenNoOneElseIsLeft(t *testing.T) {
	// Arrange
	cands := []matchupCandidate{candidate("a", 900), candidate("b", 800)}
	last := matchupHistory{LastOpponent: map[string]string{"a": "b", "b": "a"}}

	// Act
	plan := planMatchups(cands, nil, last)

	// Assert
	assertPairs(t, plan, [][2]string{{"a", "b"}})
}

func TestPlanMatchups_acceptedChallengePairsFirst(t *testing.T) {
	// Arrange: without the challenge a would play b; the challenge pairs a with the smallest pot.
	cands := []matchupCandidate{
		candidate("a", 900), candidate("b", 800), candidate("c", 700), candidate("d", 5),
	}
	locks := []matchupLock{{ChallengeID: "ch1", ChallengerID: "d", ChallengedID: "a"}}

	// Act
	plan := planMatchups(cands, locks, matchupHistory{})

	// Assert
	assertPairs(t, plan, [][2]string{{"d", "a"}, {"b", "c"}})
	if plan[0].ChallengeID != "ch1" {
		t.Fatalf("challenge id = %q, want ch1", plan[0].ChallengeID)
	}
}

func TestPlanMatchups_challengeWithAnIneligibleSideFallsBackToTheDraw(t *testing.T) {
	// Arrange: z is not among the candidates (too few members, or an empty pot).
	cands := []matchupCandidate{candidate("a", 900), candidate("b", 800)}
	locks := []matchupLock{{ChallengeID: "ch1", ChallengerID: "a", ChallengedID: "z"}}

	// Act
	plan := planMatchups(cands, locks, matchupHistory{})

	// Assert
	assertPairs(t, plan, [][2]string{{"a", "b"}})
	if plan[0].ChallengeID != "" {
		t.Fatalf("challenge id = %q, want none", plan[0].ChallengeID)
	}
}

func TestPlanMatchups_noCandidatesDrawsNothing(t *testing.T) {
	if plan := planMatchups(nil, nil, matchupHistory{}); len(plan) != 0 {
		t.Fatalf("plan = %v, want empty", pairIDs(plan))
	}
}

func TestMatchupEligible(t *testing.T) {
	week := matchupMonday
	base := postgres.GroupDirectoryRow{MemberCount: 2, CreatedAt: week.Add(-time.Hour)}
	pot := MatchupMark{PotMicros: micros(1.01)}
	cases := []struct {
		name string
		dir  postgres.GroupDirectoryRow
		mark MatchupMark
		want bool
	}{
		{"two members and a pot above a dollar", base, pot, true},
		{"one member", postgres.GroupDirectoryRow{MemberCount: 1, CreatedAt: base.CreatedAt}, pot, false},
		{"pot of exactly a dollar", base, MatchupMark{PotMicros: micros(1)}, false},
		{"created mid-week waits for next week", postgres.GroupDirectoryRow{MemberCount: 5, CreatedAt: week.Add(36 * time.Hour)}, pot, false},
		{"created on the stroke of the draw waits too", postgres.GroupDirectoryRow{MemberCount: 5, CreatedAt: week}, pot, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchupEligible(tc.dir, tc.mark, week); got != tc.want {
				t.Fatalf("eligible = %v, want %v", got, tc.want)
			}
		})
	}
}

func point(at time.Time, potUSD, netUSD float64) GroupPnLPoint {
	return GroupPnLPoint{At: at, PotNavMicros: micros(potUSD), NetInMicros: micros(netUSD), DollarPnLMicros: micros(potUSD - netUSD)}
}

func assertReturn(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("return = %.9f, want %.9f", got, want)
	}
}

func TestWeekReturn(t *testing.T) {
	start := matchupMonday
	end := start.Add(MatchupWeek)
	anchor := matchupAnchor{At: start, PotMicros: micros(1000), NetInMicros: micros(800)}
	cases := []struct {
		name   string
		points []GroupPnLPoint
		want   float64
	}{
		{
			name:   "price move with no money in or out",
			points: []GroupPnLPoint{point(end, 1020, 800)},
			want:   0.02,
		},
		{
			name:   "a deposit is not a gain",
			points: []GroupPnLPoint{point(start.Add(MatchupWeek/2), 1500, 1300), point(end, 1500, 1300)},
			want:   0,
		},
		{
			// $500 in for half the week: base = 1000 + 0.5 × 500 = 1250; gain $25.
			name:   "a deposit halfway weighs half",
			points: []GroupPnLPoint{point(start.Add(MatchupWeek/2), 1500, 1300), point(end, 1525, 1300)},
			want:   25.0 / 1250.0,
		},
		{
			// $400 out a quarter of the way in: base = 1000 − 0.75 × 400 = 700; gain $14.
			name:   "a cash out lowers the base",
			points: []GroupPnLPoint{point(start.Add(MatchupWeek/4), 600, 400), point(end, 614, 400)},
			want:   14.0 / 700.0,
		},
		{
			name:   "a loss is negative",
			points: []GroupPnLPoint{point(end, 950, 800)},
			want:   -0.05,
		},
		{
			name:   "nothing after the draw scores zero",
			points: []GroupPnLPoint{point(start.Add(-time.Hour), 1300, 800)},
			want:   0,
		},
		{
			name:   "no points scores zero",
			points: nil,
			want:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertReturn(t, weekReturn(anchor, tc.points), tc.want)
		})
	}
}

func TestWeekReturn_baseThatIsNotPositiveScoresZero(t *testing.T) {
	// Arrange: everything but a cent left at the start of the week.
	anchor := matchupAnchor{At: matchupMonday, PotMicros: micros(1000), NetInMicros: micros(1000)}
	points := []GroupPnLPoint{point(matchupMonday.Add(time.Minute), 0, 0), point(matchupMonday.Add(MatchupWeek), 0, 0)}

	// Act
	got := weekReturn(anchor, points)

	// Assert
	assertReturn(t, got, 0)
}

func TestMatchupLeader_comparesAtFrozenPrecision(t *testing.T) {
	cases := []struct {
		name string
		a, b float64
		want string
	}{
		{"a ahead", 0.012, 0.008, MatchupLeadingA},
		{"b ahead", -0.02, 0.001, MatchupLeadingB},
		{"both flat is a tie", 0, 0, MatchupLeadingTie},
		{"closer than a millionth is a tie", 0.0123451, 0.0123449, MatchupLeadingTie},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchupLeader(tc.a, tc.b); got != tc.want {
				t.Fatalf("leader = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatMatchupScore_neverNegativeZero(t *testing.T) {
	if got := formatMatchupScore(-0.0000001); got != "0.000000" {
		t.Fatalf("score = %q, want 0.000000", got)
	}
	if got := formatMatchupScore(0.0123456789); got != "0.012346" {
		t.Fatalf("score = %q, want 0.012346", got)
	}
}

func TestMatchupDaysLeft(t *testing.T) {
	end := matchupMonday.Add(MatchupWeek)
	cases := map[string]struct {
		now  time.Time
		want int
	}{
		"just after the draw": {matchupMonday.Add(time.Minute), 7},
		"thursday noon":       {matchupMonday.Add(3*24*time.Hour + 12*time.Hour), 4},
		"sunday afternoon":    {end.Add(-8 * time.Hour), 1},
		"ended, not frozen":   {end.Add(time.Minute), 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := matchupDaysLeft(end, tc.now); got != tc.want {
				t.Fatalf("days left = %d, want %d", got, tc.want)
			}
		})
	}
}

// frozenRow is a finished week between a and b (b empty for a bye). winner empty is a tie.
func frozenRow(week time.Time, a, b string, scoreA, scoreB float64, winner string) postgres.MatchupRow {
	row := postgres.MatchupRow{
		ID:        a + "-" + b + "-" + week.Format("0102"),
		WeekStart: week,
		GroupA:    postgres.MatchupGroup{ID: a, Name: "Cabal " + a},
		ScoreA:    sql.NullFloat64{Float64: scoreA, Valid: b != ""},
		FrozenAt:  sql.NullTime{Time: week.Add(MatchupWeek), Valid: true},
	}
	if b != "" {
		row.GroupB = &postgres.MatchupGroup{ID: b, Name: "Cabal " + b}
		row.ScoreB = sql.NullFloat64{Float64: scoreB, Valid: true}
	}
	if winner != "" {
		row.Winner = sql.NullString{String: winner, Valid: true}
	}
	return row
}

func TestMatchupRecordFor_countsResultsAndTheCurrentStreak(t *testing.T) {
	// Arrange: oldest to newest a loses, wins, sits out, wins, ties... newest first is what counts.
	w := func(n int) time.Time { return matchupMonday.Add(time.Duration(n) * MatchupWeek) }
	rows := []postgres.MatchupRow{
		frozenRow(w(0), "a", "b", -0.01, 0.02, "b"),
		frozenRow(w(1), "c", "a", 0.001, 0.004, "a"),
		frozenRow(w(2), "a", "", 0, 0, ""),
		frozenRow(w(3), "a", "d", 0.03, 0.01, "a"),
	}

	// Act
	rec := matchupRecordFor("a", rows)

	// Assert
	if rec.Wins != 2 || rec.Losses != 1 || rec.Ties != 0 {
		t.Fatalf("record = %d-%d-%d, want 2-1-0", rec.Wins, rec.Losses, rec.Ties)
	}
	assertReturn(t, rec.Points, -0.01+0.004+0.03)
	if rec.Streak != "W2" {
		t.Fatalf("streak = %q, want W2 (the bye neither breaks nor extends it)", rec.Streak)
	}
}

func TestMatchupRecordFor_byeScoresNothing(t *testing.T) {
	// Arrange
	rows := []postgres.MatchupRow{frozenRow(matchupMonday, "a", "", 0, 0, "")}

	// Act
	rec := matchupRecordFor("a", rows)

	// Assert
	if rec != (MatchupRecord{}) {
		t.Fatalf("record after a bye = %+v, want empty", rec)
	}
}

func TestMatchupRecordFor_tieIsATie(t *testing.T) {
	rec := matchupRecordFor("b", []postgres.MatchupRow{frozenRow(matchupMonday, "a", "b", 0, 0, "")})
	if rec.Ties != 1 || rec.Streak != "T1" {
		t.Fatalf("record = %+v, want one tie and streak T1", rec)
	}
}

func TestBuildSeasonTable_ordersByWinsThenPoints(t *testing.T) {
	// Arrange: a and c both win once; c's return is bigger. b and d both lose; d lost by less.
	pairs := []postgres.MatchupRow{
		frozenRow(matchupMonday, "a", "b", 0.01, -0.02, "a"),
		frozenRow(matchupMonday, "c", "d", 0.05, 0.001, "c"),
		frozenRow(matchupMonday, "e", "", 0, 0, ""),
	}

	// Act
	table := buildSeasonTable(pairs)

	// Assert
	var order []string
	for _, row := range table {
		order = append(order, row.GroupID)
	}
	want := []string{"c", "a", "d", "b"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v (a bye alone does not earn a row)", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}
