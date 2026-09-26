package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// Weekly matchups: cabal vs cabal.
//
// The week. A matchup week runs Monday 00:00 UTC to the next Monday 00:00 UTC. The weekly job
// (RunWeekly, driven by the matchup poller) first freezes any live week that has ended, then
// draws the current week if it has not been drawn. It runs on boot too, so a server that
// starts mid-week with no draw draws straight away.
//
// The draw. Every cabal with at least two members, a pot above $1, and a creation time before
// the week started is in it; a cabal created mid-week waits for the next Monday. Accepted
// friendly challenges are paired first, when both sides are eligible. The rest are sorted by
// pot value, largest first, and paired with their nearest neighbour so pots are comparable,
// skipping last week's opponent when another neighbour is free. An odd cabal out gets a bye:
// the smallest pot that did not sit out last week.
//
// The score. A side's score is its percent return from the draw to now, read from the series
// GET /v1/groups/{id}/pnl-history serves (nav snapshots plus the live valuation). The pot and
// net money in at the draw are recorded on the row, because history marks stock at cost and
// the week's start cannot be read back at market later. Money added or taken out during the
// week is a flow, not a gain: the return is Modified Dietz, gain over the starting pot plus
// each flow weighted by the share of the week it was in the pot. Live scores are computed on
// read; the week's end freezes them to six decimals.
//
// The result. The higher frozen score wins; equal scores tie. A bye scores nothing and records
// nothing. Records are folded from frozen rows rather than stored, so they cannot drift from
// the results they summarise.

const (
	// MatchupMinMembers is the smallest cabal the draw pairs.
	MatchupMinMembers = 2
	// MatchupMinPotMicros is the pot a cabal must beat to be drawn ("above $1").
	MatchupMinPotMicros = 1_000_000
	// MatchupWeek is how long a matchup runs.
	MatchupWeek = 7 * 24 * time.Hour
	// matchupFreezeGrace is how long past the week's end freezing waits for every pot to have
	// a live mark. After it, the week freezes on the latest point each side has.
	matchupFreezeGrace = 6 * time.Hour
	// matchupScoreDecimals is the precision a frozen score keeps; equal at this precision ties.
	matchupScoreDecimals = 6
)

// MatchupWeekStart returns Monday 00:00 UTC of the week containing t.
func MatchupWeekStart(t time.Time) time.Time {
	u := t.UTC()
	day := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	sinceMonday := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -sinceMonday)
}

// MatchupMark is one cabal's pot value and net money in at a moment. Live is false when the pot
// could not be marked and the value fell back to net money in.
type MatchupMark struct {
	PotMicros   int64
	NetInMicros int64
	At          time.Time
	Live        bool
}

// MatchupMarks is where matchups read pot values and P&L history. The live implementation is
// the Groups tab's valuation and P&L history (pnlHistoryMarks); tests pass a fake.
type MatchupMarks interface {
	// Marks returns each cabal's live pot and net money in.
	Marks(ctx context.Context, groupIDs []string) (map[string]MatchupMark, error)
	// SeriesSince returns each cabal's P&L points strictly after since, oldest first.
	SeriesSince(ctx context.Context, groupIDs []string, since time.Time) (map[string][]GroupPnLPoint, error)
}

// pnlHistoryMarks reads marks and history through the Groups tab service, so a matchup scores
// exactly the curve GET /v1/groups/{id}/pnl-history draws.
type pnlHistoryMarks struct {
	tab *GroupsTabService
}

func (m pnlHistoryMarks) Marks(ctx context.Context, groupIDs []string) (map[string]MatchupMark, error) {
	valuations, err := m.tab.valueGroups(ctx, groupIDs, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]MatchupMark, len(valuations))
	for id, v := range valuations {
		out[id] = MatchupMark{PotMicros: v.PotNavMicros, NetInMicros: v.NetUsdcInMicros, At: v.ValuedAt, Live: !v.Degraded}
	}
	return out, nil
}

func (m pnlHistoryMarks) SeriesSince(ctx context.Context, groupIDs []string, since time.Time) (map[string][]GroupPnLPoint, error) {
	if len(groupIDs) == 0 {
		return map[string][]GroupPnLPoint{}, nil
	}
	// The smallest window that still reaches back to since.
	rng := GroupPnLRange3M
	lookback := m.tab.now().UTC().Sub(since)
	for _, r := range []GroupPnLRange{GroupPnLRange1W, GroupPnLRange1M} {
		if lookback <= r.Window() {
			rng = r
			break
		}
	}
	dirs := make([]postgres.GroupDirectoryRow, 0, len(groupIDs))
	for _, id := range groupIDs {
		dirs = append(dirs, postgres.GroupDirectoryRow{ID: id})
	}
	series, err := m.tab.buildPnLSeries(ctx, dirs, rng)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]GroupPnLPoint, len(series))
	for _, s := range series {
		out[s.GroupID] = pointsAfter(s.Points, since)
	}
	return out, nil
}

func pointsAfter(points []GroupPnLPoint, since time.Time) []GroupPnLPoint {
	out := make([]GroupPnLPoint, 0, len(points))
	for _, p := range points {
		if p.At.After(since) {
			out = append(out, p)
		}
	}
	return out
}

// matchupAnchor is where a side's week starts: its pot and net money in at the draw.
type matchupAnchor struct {
	At          time.Time
	PotMicros   int64
	NetInMicros int64
}

// weekReturn is the Modified Dietz return from the anchor to the latest point, as a ratio.
//
// Gain is the change in P&L (pot minus net money in), so money added or taken out is never
// counted as a gain. The base is the starting pot plus each flow weighted by the fraction of
// the period it spent in the pot: $1,000 added an hour before the end barely moves the base,
// so it cannot dilute a week's return that the starting pot earned. No points after the anchor,
// or a base that is not positive, is a return of zero.
func weekReturn(anchor matchupAnchor, points []GroupPnLPoint) float64 {
	after := make([]GroupPnLPoint, 0, len(points))
	for _, p := range points {
		if p.At.After(anchor.At) {
			after = append(after, p)
		}
	}
	if len(after) == 0 {
		return 0
	}
	sort.SliceStable(after, func(i, j int) bool { return after[i].At.Before(after[j].At) })
	end := after[len(after)-1]
	span := end.At.Sub(anchor.At).Seconds()

	gain := float64((end.PotNavMicros - end.NetInMicros) - (anchor.PotMicros - anchor.NetInMicros))
	base := float64(anchor.PotMicros)
	prevNet := anchor.NetInMicros
	for _, p := range after {
		flow := p.NetInMicros - prevNet
		prevNet = p.NetInMicros
		if flow == 0 {
			continue
		}
		weight := 0.0
		if span > 0 {
			weight = end.At.Sub(p.At).Seconds() / span
		}
		base += weight * float64(flow)
	}
	if base <= 0 {
		return 0
	}
	return gain / base
}

// roundMatchupScore rounds a return to the precision a frozen score keeps.
func roundMatchupScore(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	scale := math.Pow10(matchupScoreDecimals)
	rounded := math.Round(v*scale) / scale
	if rounded == 0 {
		// Never a negative zero: it would print as "-0".
		return 0
	}
	return rounded
}

func formatMatchupScore(v float64) string {
	return strconv.FormatFloat(roundMatchupScore(v), 'f', matchupScoreDecimals, 64)
}

// Who is ahead, from side a's point of view.
const (
	MatchupLeadingA   = "a"
	MatchupLeadingB   = "b"
	MatchupLeadingTie = "tie"
)

// matchupLeader compares two scores at frozen precision.
func matchupLeader(scoreA, scoreB float64) string {
	a, b := roundMatchupScore(scoreA), roundMatchupScore(scoreB)
	switch {
	case a > b:
		return MatchupLeadingA
	case b > a:
		return MatchupLeadingB
	default:
		return MatchupLeadingTie
	}
}

// matchupCandidate is one eligible cabal as the draw sees it.
type matchupCandidate struct {
	GroupID     string
	PotMicros   int64
	NetInMicros int64
}

// matchupLock is an accepted challenge the draw honours when both sides are eligible.
type matchupLock struct {
	ChallengeID  string
	ChallengerID string
	ChallengedID string
}

// matchupHistory is what the draw remembers from last week.
type matchupHistory struct {
	// LastOpponent maps a cabal to the one it played last week.
	LastOpponent map[string]string
	// HadBye is every cabal that sat last week out.
	HadBye map[string]bool
}

// plannedMatchup is one pair (or bye, B nil) the draw will write.
type plannedMatchup struct {
	A           matchupCandidate
	B           *matchupCandidate
	ChallengeID string
}

// matchupEligible reports whether a cabal is in the draw for the week starting weekStart.
func matchupEligible(dir postgres.GroupDirectoryRow, mark MatchupMark, weekStart time.Time) bool {
	return dir.MemberCount >= MatchupMinMembers &&
		dir.CreatedAt.Before(weekStart) &&
		mark.PotMicros > MatchupMinPotMicros
}

// planMatchups pairs the week. Pure, so every rule of the draw is testable without a database.
func planMatchups(candidates []matchupCandidate, locks []matchupLock, last matchupHistory) []plannedMatchup {
	byID := make(map[string]matchupCandidate, len(candidates))
	for _, c := range candidates {
		byID[c.GroupID] = c
	}
	used := make(map[string]bool, len(candidates))
	out := make([]plannedMatchup, 0, len(candidates)/2+1)

	// Challenges first, in the order they were accepted.
	for _, lock := range locks {
		challenger, okA := byID[lock.ChallengerID]
		challenged, okB := byID[lock.ChallengedID]
		if !okA || !okB || used[lock.ChallengerID] || used[lock.ChallengedID] {
			continue
		}
		used[lock.ChallengerID], used[lock.ChallengedID] = true, true
		b := challenged
		out = append(out, plannedMatchup{A: challenger, B: &b, ChallengeID: lock.ChallengeID})
	}

	pool := make([]matchupCandidate, 0, len(candidates))
	for _, c := range candidates {
		if !used[c.GroupID] {
			pool = append(pool, c)
		}
	}
	sort.SliceStable(pool, func(i, j int) bool {
		if pool[i].PotMicros != pool[j].PotMicros {
			return pool[i].PotMicros > pool[j].PotMicros
		}
		return pool[i].GroupID < pool[j].GroupID
	})

	var bye *matchupCandidate
	if len(pool)%2 == 1 {
		// The smallest pot sits out, unless it sat out last week; then the next smallest.
		pick := len(pool) - 1
		for i := len(pool) - 1; i >= 0; i-- {
			if !last.HadBye[pool[i].GroupID] {
				pick = i
				break
			}
		}
		chosen := pool[pick]
		bye = &chosen
		pool = append(pool[:pick:pick], pool[pick+1:]...)
	}

	for len(pool) > 0 {
		head := pool[0]
		partner := 1
		for j := 1; j < len(pool); j++ {
			if last.LastOpponent[head.GroupID] != pool[j].GroupID {
				partner = j
				break
			}
		}
		b := pool[partner]
		out = append(out, plannedMatchup{A: head, B: &b})
		rest := make([]matchupCandidate, 0, len(pool)-2)
		for k, c := range pool {
			if k != 0 && k != partner {
				rest = append(rest, c)
			}
		}
		pool = rest
	}
	if bye != nil {
		out = append(out, plannedMatchup{A: *bye})
	}
	return out
}

// MatchupService runs the weekly draw and freeze and serves the matchup screens.
type MatchupService struct {
	store      *postgres.Store
	home       *HomeService
	marks      MatchupMarks
	candidates func(ctx context.Context) ([]postgres.GroupDirectoryRow, error)
	now        func() time.Time
	table      *matchupTableCache
}

// NewMatchupService wires matchups over the Groups tab's valuation and P&L history.
func NewMatchupService(store *postgres.Store, home *HomeService) *MatchupService {
	return &MatchupService{
		store:      store,
		home:       home,
		marks:      pnlHistoryMarks{tab: NewGroupsTabService(home, store)},
		candidates: store.ListGroupDirectory,
		now:        time.Now,
		table:      &matchupTableCache{ttl: matchupTableTTL},
	}
}

// WithMarks replaces where pot values and history come from. Intended for tests.
func (s *MatchupService) WithMarks(marks MatchupMarks) *MatchupService {
	s.marks = marks
	return s
}

// WithClock replaces the time source. Intended for tests.
func (s *MatchupService) WithClock(now func() time.Time) *MatchupService {
	s.now = now
	return s
}

// WithCandidates replaces the list of cabals the draw considers. Intended for tests, which
// share a database with other tests' cabals.
func (s *MatchupService) WithCandidates(list func(ctx context.Context) ([]postgres.GroupDirectoryRow, error)) *MatchupService {
	s.candidates = list
	return s
}

// RunWeekly freezes every live week that has ended, then draws the current week if it has no
// draw yet. Safe to call on every tick and from several API instances: each write happens once.
func (s *MatchupService) RunWeekly(ctx context.Context) error {
	now := s.now().UTC()
	week := MatchupWeekStart(now)

	var errs []error
	ended, err := s.store.ListLiveMatchupWeeksBefore(ctx, week)
	if err != nil {
		errs = append(errs, err)
	}
	for _, w := range ended {
		if _, err := s.FinalizeWeek(ctx, w); err != nil {
			errs = append(errs, fmt.Errorf("finalize week %s: %w", w.Format("2006-01-02"), err))
		}
	}
	if _, err := s.DrawWeek(ctx, week); err != nil {
		errs = append(errs, fmt.Errorf("draw week %s: %w", week.Format("2006-01-02"), err))
	}
	return errors.Join(errs...)
}

// DrawWeek pairs the week starting weekStart. It returns false when the week was already drawn.
func (s *MatchupService) DrawWeek(ctx context.Context, weekStart time.Time) (bool, error) {
	week := MatchupWeekStart(weekStart)
	if _, found, err := s.store.GetMatchupWeek(ctx, week); err != nil {
		return false, err
	} else if found {
		return false, nil
	}
	now := s.now().UTC()

	dirs, err := s.candidates(ctx)
	if err != nil {
		return false, fmt.Errorf("list cabals: %w", err)
	}
	var ids []string
	byID := make(map[string]postgres.GroupDirectoryRow, len(dirs))
	for _, d := range dirs {
		// Member count and age are known without a valuation; only value who could qualify.
		if d.MemberCount >= MatchupMinMembers && d.CreatedAt.Before(week) {
			ids = append(ids, d.ID)
			byID[d.ID] = d
		}
	}
	marks, err := s.marks.Marks(ctx, ids)
	if err != nil {
		return false, fmt.Errorf("value cabals: %w", err)
	}
	candidates := make([]matchupCandidate, 0, len(ids))
	unmarked := 0
	for _, id := range ids {
		mark, ok := marks[id]
		if !ok || !matchupEligible(byID[id], mark, week) {
			continue
		}
		if !mark.Live {
			unmarked++
		}
		candidates = append(candidates, matchupCandidate{GroupID: id, PotMicros: mark.PotMicros, NetInMicros: mark.NetInMicros})
	}

	accepted, err := s.store.ListAcceptedMatchupChallenges(ctx, week)
	if err != nil {
		return false, err
	}
	locks := make([]matchupLock, 0, len(accepted))
	planned := make([]string, 0, len(accepted))
	for _, c := range accepted {
		locks = append(locks, matchupLock{ChallengeID: c.ID, ChallengerID: c.Challenger.ID, ChallengedID: c.Challenged.ID})
		planned = append(planned, c.ID)
	}

	previous, err := s.store.ListMatchupsForWeek(ctx, week.Add(-MatchupWeek))
	if err != nil {
		return false, err
	}
	history := matchupHistory{LastOpponent: map[string]string{}, HadBye: map[string]bool{}}
	for _, m := range previous {
		if m.GroupB == nil {
			history.HadBye[m.GroupA.ID] = true
			continue
		}
		history.LastOpponent[m.GroupA.ID] = m.GroupB.ID
		history.LastOpponent[m.GroupB.ID] = m.GroupA.ID
	}

	plan := planMatchups(candidates, locks, history)
	draw := postgres.MatchupDraw{WeekStart: week, DrawnAt: now, PlannedChallengeIDs: planned}
	byes := 0
	for _, p := range plan {
		insert := postgres.MatchupInsert{
			GroupA:      p.A.GroupID,
			StartPotA:   p.A.PotMicros,
			StartNetInA: p.A.NetInMicros,
			ChallengeID: p.ChallengeID,
		}
		if p.B != nil {
			insert.GroupB = p.B.GroupID
			insert.StartPotB = p.B.PotMicros
			insert.StartNetInB = p.B.NetInMicros
		} else {
			byes++
		}
		draw.Pairs = append(draw.Pairs, insert)
	}

	created, err := s.store.CreateMatchupDraw(ctx, draw)
	if errors.Is(err, postgres.ErrMatchupDrawStale) {
		// A challenge was accepted while this draw was being planned. Nothing was written;
		// the next tick plans again with it.
		slog.Warn("matchup draw replanning", "week", week.Format("2006-01-02"), "reason", "challenges changed")
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if created {
		slog.Info("matchup week drawn",
			"week", week.Format("2006-01-02"),
			"cabals", len(candidates),
			"pairs", len(plan)-byes,
			"byes", byes,
			"challenges", len(locks),
			"unmarked_pots", unmarked,
		)
	}
	return created, nil
}

// matchupSideScore is one side's live return for the week.
type matchupSideScore struct {
	Score float64
	Live  bool
}

// scoreMatchups computes both sides' returns for every row that is not a bye.
func (s *MatchupService) scoreMatchups(ctx context.Context, rows []postgres.MatchupRow) (map[string]matchupSideScore, error) {
	var ids []string
	seen := map[string]bool{}
	since := time.Time{}
	for _, m := range rows {
		if m.GroupB == nil {
			continue
		}
		for _, id := range []string{m.GroupA.ID, m.GroupB.ID} {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		if since.IsZero() || m.StartedAt.Before(since) {
			since = m.StartedAt
		}
	}
	out := make(map[string]matchupSideScore, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	marks, err := s.marks.Marks(ctx, ids)
	if err != nil {
		return nil, err
	}
	series, err := s.marks.SeriesSince(ctx, ids, since)
	if err != nil {
		return nil, err
	}
	for _, m := range rows {
		if m.GroupB == nil {
			continue
		}
		out[m.ID+"|a"] = matchupSideScore{
			Score: weekReturn(matchupAnchor{At: m.StartedAt, PotMicros: m.StartPotA, NetInMicros: m.StartNetInA}, series[m.GroupA.ID]),
			Live:  marks[m.GroupA.ID].Live,
		}
		out[m.ID+"|b"] = matchupSideScore{
			Score: weekReturn(matchupAnchor{At: m.StartedAt, PotMicros: m.StartPotB.Int64, NetInMicros: m.StartNetInB.Int64}, series[m.GroupB.ID]),
			Live:  marks[m.GroupB.ID].Live,
		}
	}
	return out, nil
}

// FinalizeWeek freezes an ended week's scores and winners. It returns false when the week is
// already final, or when a pot could not be marked and the freeze grace has not run out yet.
func (s *MatchupService) FinalizeWeek(ctx context.Context, weekStart time.Time) (bool, error) {
	week := MatchupWeekStart(weekStart)
	now := s.now().UTC()
	weekEnd := week.Add(MatchupWeek)
	if now.Before(weekEnd) {
		return false, nil
	}
	rows, err := s.store.ListMatchupsForWeek(ctx, week)
	if err != nil {
		return false, err
	}
	scores, err := s.scoreMatchups(ctx, rows)
	if err != nil {
		return false, err
	}
	for key, score := range scores {
		if !score.Live && now.Before(weekEnd.Add(matchupFreezeGrace)) {
			slog.Warn("matchup freeze waiting for a live mark",
				"week", week.Format("2006-01-02"), "side", key, "grace_until", weekEnd.Add(matchupFreezeGrace))
			return false, nil
		}
	}

	results := make([]postgres.MatchupResult, 0, len(rows))
	for _, m := range rows {
		if m.GroupB == nil {
			results = append(results, postgres.MatchupResult{ID: m.ID})
			continue
		}
		a, b := scores[m.ID+"|a"].Score, scores[m.ID+"|b"].Score
		scoreA, scoreB := formatMatchupScore(a), formatMatchupScore(b)
		result := postgres.MatchupResult{ID: m.ID, ScoreA: &scoreA, ScoreB: &scoreB}
		switch matchupLeader(a, b) {
		case MatchupLeadingA:
			result.Winner = m.GroupA.ID
		case MatchupLeadingB:
			result.Winner = m.GroupB.ID
		}
		results = append(results, result)
	}

	frozen, err := s.store.FinalizeMatchupWeek(ctx, week, now, results)
	if err != nil {
		return false, err
	}
	if frozen {
		s.table.invalidate()
		slog.Info("matchup week final", "week", week.Format("2006-01-02"), "matchups", len(rows))
	}
	return frozen, nil
}

// matchupOutcome is one side's result in a frozen pair.
type matchupOutcome string

const (
	matchupWin  matchupOutcome = "win"
	matchupLoss matchupOutcome = "loss"
	matchupTie  matchupOutcome = "tie"
	matchupBye  matchupOutcome = "bye"
)

// outcomeFor reads a frozen row from one cabal's side.
func outcomeFor(m postgres.MatchupRow, groupID string) matchupOutcome {
	if m.GroupB == nil {
		return matchupBye
	}
	if !m.Winner.Valid {
		return matchupTie
	}
	if strings.EqualFold(m.Winner.String, groupID) {
		return matchupWin
	}
	return matchupLoss
}

// MatchupRecord is a cabal's season record.
type MatchupRecord struct {
	Wins   int
	Losses int
	Ties   int
	// Points is the sum of the cabal's frozen weekly returns, the tiebreak after wins.
	Points float64
	// Streak is the current run, newest first, byes skipped: "W3", "L1", "T2". Empty with no results.
	Streak string
}

// matchupRecordFor folds a cabal's frozen rows (any order) into its record.
func matchupRecordFor(groupID string, rows []postgres.MatchupRow) MatchupRecord {
	ordered := append([]postgres.MatchupRow(nil), rows...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].WeekStart.After(ordered[j].WeekStart) })

	var rec MatchupRecord
	var streakKind matchupOutcome
	streak, streakOpen := 0, true
	for _, m := range ordered {
		if !m.FrozenAt.Valid {
			continue
		}
		outcome := outcomeFor(m, groupID)
		if outcome == matchupBye {
			continue
		}
		switch outcome {
		case matchupWin:
			rec.Wins++
		case matchupLoss:
			rec.Losses++
		case matchupTie:
			rec.Ties++
		}
		if strings.EqualFold(m.GroupA.ID, groupID) {
			rec.Points += m.ScoreA.Float64
		} else {
			rec.Points += m.ScoreB.Float64
		}
		if streakOpen {
			if streak == 0 || outcome == streakKind {
				streakKind = outcome
				streak++
			} else {
				streakOpen = false
			}
		}
	}
	rec.Points = roundMatchupScore(rec.Points)
	if streak > 0 {
		letter := map[matchupOutcome]string{matchupWin: "W", matchupLoss: "L", matchupTie: "T"}[streakKind]
		rec.Streak = letter + strconv.Itoa(streak)
	}
	return rec
}

// seasonTableRow is one cabal on the season table before viewer-specific fields.
type seasonTableRow struct {
	GroupID    string
	Name       string
	PictureURL string
	Record     MatchupRecord
}

// buildSeasonTable folds every frozen pair into the table, ordered by wins, then points, then
// fewer losses, then name.
func buildSeasonTable(pairs []postgres.MatchupRow) []seasonTableRow {
	rowsByGroup := map[string][]postgres.MatchupRow{}
	faces := map[string]postgres.MatchupGroup{}
	newest := map[string]time.Time{}
	for _, m := range pairs {
		if m.GroupB == nil || !m.FrozenAt.Valid {
			continue
		}
		for _, g := range []postgres.MatchupGroup{m.GroupA, *m.GroupB} {
			rowsByGroup[g.ID] = append(rowsByGroup[g.ID], m)
			if m.WeekStart.After(newest[g.ID]) || newest[g.ID].IsZero() {
				newest[g.ID] = m.WeekStart
				faces[g.ID] = g
			}
		}
	}
	out := make([]seasonTableRow, 0, len(rowsByGroup))
	for id, rows := range rowsByGroup {
		face := faces[id]
		out = append(out, seasonTableRow{
			GroupID:    id,
			Name:       face.Name,
			PictureURL: nullStringValue(face.PictureURL),
			Record:     matchupRecordFor(id, rows),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Record, out[j].Record
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		if a.Losses != b.Losses {
			return a.Losses < b.Losses
		}
		if !strings.EqualFold(out[i].Name, out[j].Name) {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].GroupID < out[j].GroupID
	})
	return out
}

// matchupTableTTL bounds how stale a cached table's names and pictures can be. The records
// themselves only change when a week freezes, which invalidates the cache.
const matchupTableTTL = 5 * time.Minute

type matchupTableCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	builtAt time.Time
	through time.Time
	hasRows bool
	rows    []seasonTableRow
}

func (c *matchupTableCache) get(through time.Time, now time.Time) ([]seasonTableRow, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasRows || !c.through.Equal(through) || now.Sub(c.builtAt) > c.ttl {
		return nil, false
	}
	return c.rows, true
}

func (c *matchupTableCache) put(through, now time.Time, rows []seasonTableRow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.through, c.builtAt, c.rows, c.hasRows = through, now, rows, true
}

func (c *matchupTableCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hasRows = false
	c.rows = nil
}
