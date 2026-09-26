package app

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// fakeMatchupMarks serves canned pot values and P&L points, so a matchup's score is exactly
// what the test wrote down.
type fakeMatchupMarks struct {
	marks  map[string]MatchupMark
	series map[string][]GroupPnLPoint
}

func newFakeMatchupMarks() *fakeMatchupMarks {
	return &fakeMatchupMarks{marks: map[string]MatchupMark{}, series: map[string][]GroupPnLPoint{}}
}

func (f *fakeMatchupMarks) Marks(_ context.Context, ids []string) (map[string]MatchupMark, error) {
	out := map[string]MatchupMark{}
	for _, id := range ids {
		if m, ok := f.marks[id]; ok {
			out[id] = m
		}
	}
	return out, nil
}

func (f *fakeMatchupMarks) SeriesSince(_ context.Context, ids []string, since time.Time) (map[string][]GroupPnLPoint, error) {
	out := map[string][]GroupPnLPoint{}
	for _, id := range ids {
		out[id] = pointsAfter(f.series[id], since)
	}
	return out, nil
}

// pot sets a cabal's live pot, with net money in equal to it (no gain yet).
func (f *fakeMatchupMarks) pot(id string, usd float64) {
	f.marks[id] = MatchupMark{PotMicros: micros(usd), NetInMicros: micros(usd), Live: true}
}

type matchupFixture struct {
	h        integrationHarness
	sessions *SessionService
	service  *MatchupService
	marks    *fakeMatchupMarks
	clock    time.Time
	cabals   []string
}

// testMatchupWeek picks a Monday far in the future, different for every test run, so a test's
// weeks never meet another test's in the shared database.
func testMatchupWeek(t *testing.T) time.Time {
	t.Helper()
	n, err := rand.Int(rand.Reader, big.NewInt(4000))
	if err != nil {
		t.Fatalf("random week: %v", err)
	}
	base := MatchupWeekStart(time.Date(2100, 1, 4, 0, 0, 0, 0, time.UTC))
	return base.Add(time.Duration(n.Int64()) * 2 * MatchupWeek)
}

func newMatchupFixture(t *testing.T) *matchupFixture {
	t.Helper()
	h := integrationApp(t)
	f := &matchupFixture{h: h, sessions: NewSessionService(h.Store, h.Privy), marks: newFakeMatchupMarks()}
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)
	f.service = NewMatchupService(h.Store, home).
		WithMarks(f.marks).
		WithClock(func() time.Time { return f.clock }).
		WithCandidates(func(ctx context.Context) ([]postgres.GroupDirectoryRow, error) {
			// Only this test's cabals: the database is shared with other tests.
			return h.Store.ListGroupDirectoryByIDs(ctx, f.cabals)
		})
	return f
}

// forgetWeeks removes the weeks a test drew once it ends; their pairs go with them.
func (f *matchupFixture) forgetWeeks(t *testing.T, weeks ...time.Time) {
	t.Helper()
	t.Cleanup(func() {
		for _, w := range weeks {
			if _, err := f.h.DB.ExecContext(context.Background(), `DELETE FROM matchup_weeks WHERE week_start = $1::date`, w.Format("2006-01-02")); err != nil {
				t.Errorf("forget matchup week: %v", err)
			}
		}
	})
}

// member opens a session and returns the user id and access token.
func (f *matchupFixture) member(t *testing.T, label string) (string, string) {
	t.Helper()
	result := openTestSession(t, f.h.ISO, f.sessions, f.h.Privy, label, "Member "+label)
	return result.UserID, f.h.ISO.UniqueToken(label)
}

// cabal creates a cabal with the given members (the first is its creator).
func (f *matchupFixture) cabal(t *testing.T, label string, memberIDs ...string) string {
	t.Helper()
	ctx := context.Background()
	group, err := f.h.Store.InsertGroup(ctx, testGroupName(f.h.ISO, label), memberIDs[0])
	if err != nil {
		t.Fatalf("insert cabal: %v", err)
	}
	f.h.ISO.TrackGroup(group.ID)
	tx, err := f.h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, id := range memberIDs {
		if err := f.h.Store.InsertGroupMemberTx(ctx, tx, group.ID, id); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert member: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit members: %v", err)
	}
	f.cabals = append(f.cabals, group.ID)
	return group.ID
}

// sideOf returns the row that holds groupID this week.
func sideOf(t *testing.T, rows []postgres.MatchupRow, groupID string) postgres.MatchupRow {
	t.Helper()
	for _, m := range rows {
		if m.GroupA.ID == groupID || (m.GroupB != nil && m.GroupB.ID == groupID) {
			return m
		}
	}
	t.Fatalf("cabal %s is not in the week", groupID)
	return postgres.MatchupRow{}
}

func opponentOf(m postgres.MatchupRow, groupID string) string {
	if m.GroupB == nil {
		return ""
	}
	if m.GroupA.ID == groupID {
		return m.GroupB.ID
	}
	return m.GroupA.ID
}

func TestMatchupDraw_pairsEligibleCabalsByPotExactlyOnce(t *testing.T) {
	// Arrange: five eligible cabals, and three that are not.
	f := newMatchupFixture(t)
	week := testMatchupWeek(t)
	f.forgetWeeks(t, week)
	ada, _ := f.member(t, "ada")
	bo, _ := f.member(t, "bo")
	big1 := f.cabal(t, "big", ada, bo)
	big2 := f.cabal(t, "big2", ada, bo)
	mid1 := f.cabal(t, "mid", ada, bo)
	mid2 := f.cabal(t, "mid2", ada, bo)
	small := f.cabal(t, "small", ada, bo)
	solo := f.cabal(t, "solo", ada)
	dollar := f.cabal(t, "dollar", ada, bo)
	late := f.cabal(t, "late", ada, bo)
	for id, usd := range map[string]float64{big1: 5000, big2: 4200, mid1: 300, mid2: 250, small: 20, solo: 900, dollar: 1, late: 800} {
		f.marks.pot(id, usd)
	}
	if _, err := f.h.DB.ExecContext(context.Background(), `UPDATE groups SET created_at = $2 WHERE id = $1`, late, week.Add(26*time.Hour)); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	f.clock = week.Add(time.Minute)

	// Act
	first, err := f.service.DrawWeek(context.Background(), week)
	if err != nil {
		t.Fatalf("draw: %v", err)
	}
	second, err := f.service.DrawWeek(context.Background(), week)
	if err != nil {
		t.Fatalf("second draw: %v", err)
	}

	// Assert
	if !first || second {
		t.Fatalf("drew = %v then %v, want true then false", first, second)
	}
	rows, err := f.h.Store.ListMatchupsForWeek(context.Background(), week)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 2 pairs and a bye", len(rows))
	}
	if got := opponentOf(sideOf(t, rows, big1), big1); got != big2 {
		t.Fatalf("largest pot plays %s, want the second largest", got)
	}
	if got := opponentOf(sideOf(t, rows, mid1), mid1); got != mid2 {
		t.Fatalf("third pot plays %s, want the fourth", got)
	}
	if bye := sideOf(t, rows, small); !bye.IsBye() {
		t.Fatalf("smallest pot is not on a bye")
	}
	pair := sideOf(t, rows, big1)
	if pair.StartPotA != micros(5000) || pair.StartPotB.Int64 != micros(4200) {
		t.Fatalf("recorded starting pots = %d / %d, want the pots at the draw", pair.StartPotA, pair.StartPotB.Int64)
	}
}

func TestMatchupFinalize_freezesScoresAndWinnersExactlyOnce(t *testing.T) {
	// Arrange: a pair and a bye.
	f := newMatchupFixture(t)
	week := testMatchupWeek(t)
	f.forgetWeeks(t, week)
	ada, _ := f.member(t, "ada")
	bo, _ := f.member(t, "bo")
	up := f.cabal(t, "up", ada, bo)
	flat := f.cabal(t, "flat", ada, bo)
	sitter := f.cabal(t, "sitter", ada, bo)
	f.marks.pot(up, 1000)
	f.marks.pot(flat, 900)
	f.marks.pot(sitter, 10)
	f.clock = week.Add(time.Minute)
	if _, err := f.service.DrawWeek(context.Background(), week); err != nil {
		t.Fatalf("draw: %v", err)
	}
	end := week.Add(MatchupWeek)
	f.marks.series[up] = []GroupPnLPoint{point(end.Add(time.Minute), 1025, 1000)}
	f.marks.series[flat] = []GroupPnLPoint{point(end.Add(time.Minute), 909, 900)}
	f.marks.series[sitter] = []GroupPnLPoint{point(end.Add(time.Minute), 20, 10)}

	// Act: too early, then on time twice.
	f.clock = end.Add(-time.Hour)
	early, errEarly := f.service.FinalizeWeek(context.Background(), week)
	f.clock = end.Add(2 * time.Minute)
	first, errFirst := f.service.FinalizeWeek(context.Background(), week)
	second, errSecond := f.service.FinalizeWeek(context.Background(), week)

	// Assert
	if errEarly != nil || errFirst != nil || errSecond != nil {
		t.Fatalf("finalize errors = %v, %v, %v", errEarly, errFirst, errSecond)
	}
	if early || !first || second {
		t.Fatalf("froze = %v, %v, %v; want false before the end, then true, then false", early, first, second)
	}
	rows, err := f.h.Store.ListMatchupsForWeek(context.Background(), week)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	pair := sideOf(t, rows, up)
	if !pair.Winner.Valid || pair.Winner.String != up {
		t.Fatalf("winner = %+v, want the cabal up 2.5%%", pair.Winner)
	}
	if pair.ScoreA.Float64 != 0.025 || pair.ScoreB.Float64 != 0.01 {
		t.Fatalf("scores = %v / %v, want 0.025 / 0.01", pair.ScoreA.Float64, pair.ScoreB.Float64)
	}
	bye := sideOf(t, rows, sitter)
	if bye.ScoreA.Valid || bye.Winner.Valid || !bye.FrozenAt.Valid {
		t.Fatalf("bye = score %+v winner %+v frozen %v; want no score, no winner, frozen", bye.ScoreA, bye.Winner, bye.FrozenAt.Valid)
	}
	week2, found, err := f.h.Store.GetMatchupWeek(context.Background(), week)
	if err != nil || !found || week2.Status != postgres.MatchupWeekFinal {
		t.Fatalf("week = %+v, %v, %v; want final", week2, found, err)
	}
}

func TestMatchupFinalize_waitsForALiveMarkWithinTheGrace(t *testing.T) {
	// Arrange
	f := newMatchupFixture(t)
	week := testMatchupWeek(t)
	f.forgetWeeks(t, week)
	ada, _ := f.member(t, "ada")
	bo, _ := f.member(t, "bo")
	a := f.cabal(t, "a", ada, bo)
	b := f.cabal(t, "b", ada, bo)
	f.marks.pot(a, 100)
	f.marks.pot(b, 100)
	f.clock = week.Add(time.Minute)
	if _, err := f.service.DrawWeek(context.Background(), week); err != nil {
		t.Fatalf("draw: %v", err)
	}
	unmarked := f.marks.marks[b]
	unmarked.Live = false
	f.marks.marks[b] = unmarked
	end := week.Add(MatchupWeek)

	// Act
	f.clock = end.Add(time.Hour)
	withinGrace, _ := f.service.FinalizeWeek(context.Background(), week)
	f.clock = end.Add(matchupFreezeGrace + time.Minute)
	afterGrace, err := f.service.FinalizeWeek(context.Background(), week)

	// Assert
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if withinGrace || !afterGrace {
		t.Fatalf("froze = %v within the grace, %v after; want false then true", withinGrace, afterGrace)
	}
}

func TestMatchupRunWeekly_freezesLastWeekThenDrawsThisWeek(t *testing.T) {
	// Arrange: last week drawn and ended; this week not drawn.
	f := newMatchupFixture(t)
	last := testMatchupWeek(t)
	this := last.Add(MatchupWeek)
	f.forgetWeeks(t, last, this)
	ada, _ := f.member(t, "ada")
	bo, _ := f.member(t, "bo")
	a := f.cabal(t, "a", ada, bo)
	b := f.cabal(t, "b", ada, bo)
	c := f.cabal(t, "c", ada, bo)
	d := f.cabal(t, "d", ada, bo)
	for id, usd := range map[string]float64{a: 900, b: 800, c: 700, d: 600} {
		f.marks.pot(id, usd)
	}
	f.clock = last.Add(time.Minute)
	if _, err := f.service.DrawWeek(context.Background(), last); err != nil {
		t.Fatalf("draw last week: %v", err)
	}

	// Act
	f.clock = this.Add(3 * time.Minute)
	if err := f.service.RunWeekly(context.Background()); err != nil {
		t.Fatalf("run weekly: %v", err)
	}

	// Assert: last week final, this week drawn without last week's pairings.
	w, _, err := f.h.Store.GetMatchupWeek(context.Background(), last)
	if err != nil || w.Status != postgres.MatchupWeekFinal {
		t.Fatalf("last week = %+v, %v; want final", w, err)
	}
	rows, err := f.h.Store.ListMatchupsForWeek(context.Background(), this)
	if err != nil {
		t.Fatalf("list this week: %v", err)
	}
	if got := opponentOf(sideOf(t, rows, a), a); got != c {
		t.Fatalf("a plays %s this week, want c (it played b last week)", got)
	}
}

func TestMatchupChallenge_acceptedChallengeIsPairedAheadOfTheDraw(t *testing.T) {
	// Arrange: ada runs the small cabal, bo the big one; without the challenge big plays mid.
	f := newMatchupFixture(t)
	week := testMatchupWeek(t)
	next := week.Add(MatchupWeek)
	f.forgetWeeks(t, week, next)
	ada, adaToken := f.member(t, "ada")
	bo, boToken := f.member(t, "bo")
	cy, cyToken := f.member(t, "cy")
	small := f.cabal(t, "small", ada, cy)
	big := f.cabal(t, "big", bo, cy)
	mid1 := f.cabal(t, "mid", ada, bo)
	mid2 := f.cabal(t, "mid2", ada, bo)
	for id, usd := range map[string]float64{big: 9000, mid1: 800, mid2: 700, small: 30} {
		f.marks.pot(id, usd)
	}
	f.clock = week.Add(2*24*time.Hour + 10*time.Hour) // Wednesday
	ctx := context.Background()

	// Act
	sent, created, err := f.service.Challenge(ctx, adaToken, small, big)
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	resent, createdAgain, err := f.service.Challenge(ctx, adaToken, small, big)
	if err != nil {
		t.Fatalf("resend: %v", err)
	}
	_, _, reverseErr := f.service.Challenge(ctx, boToken, big, small)
	_, wrongSideErr := f.service.AcceptChallenge(ctx, adaToken, small, sent.ID)
	_, outsiderErr := f.service.AcceptChallenge(ctx, adaToken, big, sent.ID)
	accepted, err := f.service.AcceptChallenge(ctx, boToken, big, sent.ID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	_, _, lockedErr := f.service.Challenge(ctx, cyToken, small, mid1)
	f.clock = next.Add(time.Minute)
	if _, err := f.service.DrawWeek(ctx, next); err != nil {
		t.Fatalf("draw: %v", err)
	}

	// Assert
	if !created || createdAgain || resent.ID != sent.ID {
		t.Fatalf("sent twice: created %v then %v, ids %s / %s; want one challenge", created, createdAgain, sent.ID, resent.ID)
	}
	if sent.Direction != "outgoing" || sent.Status != postgres.MatchupChallengePending || !sent.WeekStart.Equal(next) {
		t.Fatalf("sent = %+v, want an outgoing pending challenge for next week", sent)
	}
	var conflict *MatchupConflictError
	if !errors.As(reverseErr, &conflict) || conflict.Reason != MatchupConflictIncomingChallenge {
		t.Fatalf("reverse challenge err = %v, want incoming_challenge", reverseErr)
	}
	if !errors.Is(wrongSideErr, ErrMatchupChallengeNotFound) {
		t.Fatalf("challenger accepting own challenge err = %v, want not found", wrongSideErr)
	}
	if !errors.Is(outsiderErr, ErrNotGroupMember) {
		t.Fatalf("non-member accepting err = %v, want not a member", outsiderErr)
	}
	if accepted.Direction != "incoming" || accepted.Status != postgres.MatchupChallengeAccepted {
		t.Fatalf("accepted = %+v, want an incoming accepted challenge", accepted)
	}
	if !errors.As(lockedErr, &conflict) || conflict.Reason != MatchupConflictAlreadyMatched {
		t.Fatalf("challenge from a locked cabal err = %v, want already_matched", lockedErr)
	}
	rows, err := f.h.Store.ListMatchupsForWeek(ctx, next)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	pair := sideOf(t, rows, small)
	if opponentOf(pair, small) != big || !pair.ChallengeID.Valid || pair.ChallengeID.String != sent.ID {
		t.Fatalf("small plays %s (challenge %+v), want big from the challenge", opponentOf(pair, small), pair.ChallengeID)
	}
	if got := opponentOf(sideOf(t, rows, mid1), mid1); got != mid2 {
		t.Fatalf("mid plays %s, want mid2 from the draw", got)
	}
	settled, _, err := f.h.Store.GetMatchupChallenge(ctx, sent.ID)
	if err != nil || settled.Status != postgres.MatchupChallengeScheduled {
		t.Fatalf("challenge after the draw = %+v, %v; want scheduled", settled, err)
	}
	_, lateErr := f.service.AcceptChallenge(ctx, boToken, big, sent.ID)
	if !errors.As(lateErr, &conflict) || conflict.Reason != MatchupConflictChallengeClosed {
		t.Fatalf("accept after the draw err = %v, want challenge_closed", lateErr)
	}
}

func TestMatchupChallenge_pendingChallengeExpiresAtTheDraw(t *testing.T) {
	// Arrange
	f := newMatchupFixture(t)
	week := testMatchupWeek(t)
	next := week.Add(MatchupWeek)
	f.forgetWeeks(t, week, next)
	ada, adaToken := f.member(t, "ada")
	bo, boToken := f.member(t, "bo")
	a := f.cabal(t, "a", ada, bo)
	b := f.cabal(t, "b", bo, ada)
	f.marks.pot(a, 100)
	f.marks.pot(b, 100)
	f.clock = week.Add(24 * time.Hour)
	ctx := context.Background()
	sent, _, err := f.service.Challenge(ctx, adaToken, a, b)
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}

	// Act
	f.clock = next.Add(time.Minute)
	if _, err := f.service.DrawWeek(ctx, next); err != nil {
		t.Fatalf("draw: %v", err)
	}
	_, acceptErr := f.service.AcceptChallenge(ctx, boToken, b, sent.ID)

	// Assert
	var conflict *MatchupConflictError
	if !errors.As(acceptErr, &conflict) || conflict.Reason != MatchupConflictChallengeClosed {
		t.Fatalf("accept after the draw err = %v, want challenge_closed", acceptErr)
	}
	row, _, err := f.h.Store.GetMatchupChallenge(ctx, sent.ID)
	if err != nil || row.Status != postgres.MatchupChallengeExpired {
		t.Fatalf("challenge = %+v, %v; want expired", row, err)
	}
}

func TestMatchupChallenge_refusesSelfAndUnknownCabals(t *testing.T) {
	// Arrange
	f := newMatchupFixture(t)
	f.clock = testMatchupWeek(t).Add(time.Hour)
	ada, adaToken := f.member(t, "ada")
	bo, _ := f.member(t, "bo")
	a := f.cabal(t, "a", ada, bo)
	ctx := context.Background()

	// Act
	_, _, selfErr := f.service.Challenge(ctx, adaToken, a, a)
	_, _, unknownErr := f.service.Challenge(ctx, adaToken, a, "00000000-0000-4000-8000-000000000000")
	_, _, malformedErr := f.service.Challenge(ctx, adaToken, a, "not-a-cabal")

	// Assert
	if !errors.Is(selfErr, ErrMatchupChallengeSelf) {
		t.Fatalf("self err = %v", selfErr)
	}
	if !errors.Is(unknownErr, ErrMatchupOpponentNotFound) || !errors.Is(malformedErr, ErrMatchupOpponentNotFound) {
		t.Fatalf("unknown err = %v, malformed err = %v; want opponent not found", unknownErr, malformedErr)
	}
}

func TestMatchupReads_groupHomeAndSeasonTable(t *testing.T) {
	// Arrange: two weeks played between four cabals, the second still live.
	f := newMatchupFixture(t)
	first := testMatchupWeek(t)
	second := first.Add(MatchupWeek)
	f.forgetWeeks(t, first, second)
	ada, adaToken := f.member(t, "ada")
	bo, _ := f.member(t, "bo")
	a := f.cabal(t, "a", ada, bo)
	b := f.cabal(t, "b", bo, ada)
	c := f.cabal(t, "c", bo, ada)
	d := f.cabal(t, "d", bo, ada)
	outsider := f.cabal(t, "e", bo)
	for id, usd := range map[string]float64{a: 900, b: 800, c: 700, d: 600} {
		f.marks.pot(id, usd)
	}
	ctx := context.Background()
	f.clock = first.Add(time.Minute)
	if _, err := f.service.DrawWeek(ctx, first); err != nil {
		t.Fatalf("draw first: %v", err)
	}
	firstEnd := first.Add(MatchupWeek)
	f.marks.series[a] = []GroupPnLPoint{point(firstEnd.Add(time.Minute), 918, 900)} // +2%
	f.marks.series[b] = []GroupPnLPoint{point(firstEnd.Add(time.Minute), 808, 800)} // +1%
	f.marks.series[c] = []GroupPnLPoint{point(firstEnd.Add(time.Minute), 693, 700)} // −1%
	f.marks.series[d] = []GroupPnLPoint{point(firstEnd.Add(time.Minute), 612, 600)} // +2%
	f.clock = second.Add(time.Minute)
	if err := f.service.RunWeekly(ctx); err != nil {
		t.Fatalf("run weekly: %v", err)
	}
	// This week so far: a is up 1%, its opponent (c, avoiding last week's b) is down.
	f.marks.series[a] = append(f.marks.series[a], point(second.Add(2*24*time.Hour), 927.18, 900))
	f.marks.series[c] = append(f.marks.series[c], point(second.Add(2*24*time.Hour), 686.07, 700))
	f.clock = second.Add(3 * 24 * time.Hour)

	// Act
	view, err := f.service.GroupMatchup(ctx, adaToken, c)
	if err != nil {
		t.Fatalf("group matchup: %v", err)
	}
	home, err := f.service.HomeMatchups(ctx, adaToken)
	if err != nil {
		t.Fatalf("home matchups: %v", err)
	}
	table, err := f.service.SeasonTable(ctx, adaToken, 50)
	if err != nil {
		t.Fatalf("season table: %v", err)
	}
	outsiderView, err := f.service.GroupMatchup(ctx, adaToken, outsider)
	if err != nil {
		t.Fatalf("outsider matchup: %v", err)
	}

	// Assert: the cabal asked about is side a, and it is behind.
	if view.Current == nil || view.Current.A.GroupID != c || view.Current.B == nil || view.Current.B.GroupID != a {
		t.Fatalf("current = %+v, want c against a with c first", view.Current)
	}
	if view.Current.Leading != MatchupLeadingB || view.Current.DaysLeft != 4 {
		t.Fatalf("leading %q with %d days left, want b with 4", view.Current.Leading, view.Current.DaysLeft)
	}
	if view.Record.Wins != 0 || view.Record.Losses != 1 || len(view.Recent) != 1 || view.Recent[0].Outcome != "loss" {
		t.Fatalf("record %+v recent %+v, want c 0-1 with one loss", view.Record, view.Recent)
	}
	if !view.CanChallenge {
		t.Fatalf("ada is a member of c and can challenge")
	}
	if len(home.Matchups) != 2 || !home.Drawn {
		t.Fatalf("home matchups = %d (drawn %v), want both of ada's pairs", len(home.Matchups), home.Drawn)
	}
	for _, m := range home.Matchups {
		if m.A.GroupID != a && m.A.GroupID != b && m.A.GroupID != c && m.A.GroupID != d {
			t.Fatalf("home matchup %+v does not lead with one of ada's cabals", m)
		}
	}
	if outsiderView.Current != nil || outsiderView.CanChallenge {
		t.Fatalf("a one-member cabal is not in the draw and ada is not in it: %+v", outsiderView)
	}
	if len(table.Rows) != 4 || !table.ThroughWeek.Equal(first) {
		t.Fatalf("table = %+v, want four cabals through the first week", table)
	}
	// a and d won; d's +2% equals a's, so fewer losses and then name decide; both are ada's.
	if table.Rows[0].Record.Wins != 1 || table.Rows[3].Record.Wins != 0 || !table.Rows[0].IsMine {
		t.Fatalf("table order = %+v", table.Rows)
	}
	if table.Rows[0].Record.Streak != "W1" {
		t.Fatalf("leader streak = %q, want W1", table.Rows[0].Record.Streak)
	}
}
