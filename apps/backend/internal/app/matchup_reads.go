package app

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// Matchup reads (a cabal's matchup, the viewer's matchups for Home, the season table) and
// friendly challenges.
//
// Authorization: every read needs a signed-in user. A cabal's matchup, record and results are
// readable by anyone signed in, the same way its pot and P&L are on the leaderboard; only its
// open challenges, and sending or accepting one, are for members.

const (
	// MatchupRecentLimit is how many past results a cabal's matchup screen shows.
	MatchupRecentLimit = 4
	// MatchupMaxOpenChallenges caps a cabal's unanswered challenges for one week.
	MatchupMaxOpenChallenges = 5
	MatchupTableDefaultLimit = 20
	MatchupTableMaxLimit     = 50
)

var (
	// ErrMatchupOpponentNotFound means the cabal named in a challenge does not exist.
	ErrMatchupOpponentNotFound = errors.New("cabal to challenge not found")
	// ErrMatchupChallengeSelf means a cabal tried to challenge itself.
	ErrMatchupChallengeSelf = errors.New("a cabal cannot challenge itself")
	// ErrMatchupChallengeNotFound means no challenge with that id was sent to this cabal.
	ErrMatchupChallengeNotFound = errors.New("challenge not found")
)

// Reasons a challenge write is refused with a conflict.
const (
	MatchupConflictAlreadyMatched    = "already_matched"
	MatchupConflictIncomingChallenge = "incoming_challenge"
	MatchupConflictTooManyChallenges = "too_many_challenges"
	MatchupConflictChallengeClosed   = "challenge_closed"
)

// MatchupConflictError is a challenge the rules refuse right now. Reason is machine-readable.
type MatchupConflictError struct {
	Reason string
}

func (e *MatchupConflictError) Error() string { return "matchup challenge refused: " + e.Reason }

// MatchupFace is the public face of a cabal on a matchup surface.
type MatchupFace struct {
	GroupID     string
	Name        string
	PictureURL  string
	MemberCount int
}

// MatchupSide is one cabal in a live matchup with its score for the week so far.
type MatchupSide struct {
	MatchupFace
	// Score is the week's return as a ratio. Nil for a cabal on a bye.
	Score *float64
}

// MatchupCurrent is a live matchup, oriented so A is the cabal the caller asked about.
type MatchupCurrent struct {
	ID        string
	WeekStart time.Time
	WeekEnd   time.Time
	DaysLeft  int
	A         MatchupSide
	// B is nil on a bye.
	B *MatchupSide
	// Leading is MatchupLeadingA, MatchupLeadingB or MatchupLeadingTie; empty on a bye.
	Leading string
	// FromChallenge is true when the pair was a friendly challenge rather than the draw.
	FromChallenge bool
}

// MatchupResult is one frozen week from a cabal's side.
type MatchupPastResult struct {
	ID        string
	WeekStart time.Time
	// Outcome is "win", "loss", "tie" or "bye".
	Outcome string
	// Opponent is nil on a bye.
	Opponent      *MatchupFace
	Score         *float64
	OpponentScore *float64
	// WinnerGroupID is empty on a tie or a bye.
	WinnerGroupID string
}

// MatchupChallenge is a friendly challenge from one cabal's side.
type MatchupChallenge struct {
	ID        string
	WeekStart time.Time
	// Direction is "incoming" (sent to this cabal) or "outgoing" (sent by it).
	Direction  string
	Status     string
	Opponent   MatchupFace
	CreatedAt  time.Time
	AcceptedAt *time.Time
}

// GroupMatchupView is GET /v1/groups/{id}/matchup.
type GroupMatchupView struct {
	// NextDrawAt is the next Monday 00:00 UTC, when this week ends and the next is drawn.
	NextDrawAt time.Time
	// Current is nil when the cabal is not in this week's draw.
	Current    *MatchupCurrent
	Record     MatchupRecord
	Recent     []MatchupPastResult
	Challenges []MatchupChallenge
	// CanChallenge is true for members, who may send and accept challenges.
	CanChallenge bool
}

// HomeMatchupsView is GET /v1/home/matchups.
type HomeMatchupsView struct {
	WeekStart  time.Time
	WeekEnd    time.Time
	DaysLeft   int
	NextDrawAt time.Time
	// Drawn is false in the minutes between Monday 00:00 UTC and the draw landing.
	Drawn     bool
	Matchups  []MatchupCurrent
	HasCabals bool
}

// MatchupTableRow is one cabal on the season table.
type MatchupTableRow struct {
	Rank       int
	GroupID    string
	Name       string
	PictureURL string
	Record     MatchupRecord
	IsMine     bool
}

// MatchupTableView is GET /v1/matchups/table.
type MatchupTableView struct {
	// ThroughWeek is the last frozen week the table includes; zero when no week has finished.
	ThroughWeek time.Time
	Rows        []MatchupTableRow
}

// ClampMatchupTableLimit applies the default for 0 and caps at MatchupTableMaxLimit.
func ClampMatchupTableLimit(limit int) int {
	if limit <= 0 {
		return MatchupTableDefaultLimit
	}
	if limit > MatchupTableMaxLimit {
		return MatchupTableMaxLimit
	}
	return limit
}

// matchupDaysLeft counts whole or part days until the week ends: 7 just after the draw, 1 on
// the last day, 0 once it has ended and is waiting to freeze.
func matchupDaysLeft(weekEnd, now time.Time) int {
	remaining := weekEnd.Sub(now)
	if remaining <= 0 {
		return 0
	}
	return int(math.Ceil(remaining.Hours() / 24))
}

func matchupFace(g postgres.MatchupGroup) MatchupFace {
	return MatchupFace{GroupID: g.ID, Name: g.Name, PictureURL: nullStringValue(g.PictureURL), MemberCount: g.MemberCount}
}

func scorePtr(v float64) *float64 {
	rounded := roundMatchupScore(v)
	return &rounded
}

// currentMatchup builds a live matchup view with viewerSide ("a" or "b") shown as A.
func currentMatchup(m postgres.MatchupRow, scores map[string]matchupSideScore, flip bool, now time.Time) MatchupCurrent {
	weekEnd := m.WeekStart.Add(MatchupWeek)
	out := MatchupCurrent{
		ID:            m.ID,
		WeekStart:     m.WeekStart,
		WeekEnd:       weekEnd,
		DaysLeft:      matchupDaysLeft(weekEnd, now),
		A:             MatchupSide{MatchupFace: matchupFace(m.GroupA)},
		FromChallenge: m.ChallengeID.Valid,
	}
	if m.GroupB == nil {
		return out
	}
	a := scores[m.ID+"|a"].Score
	b := scores[m.ID+"|b"].Score
	sideA := MatchupSide{MatchupFace: matchupFace(m.GroupA), Score: scorePtr(a)}
	sideB := MatchupSide{MatchupFace: matchupFace(*m.GroupB), Score: scorePtr(b)}
	out.Leading = matchupLeader(a, b)
	if flip {
		sideA, sideB = sideB, sideA
		switch out.Leading {
		case MatchupLeadingA:
			out.Leading = MatchupLeadingB
		case MatchupLeadingB:
			out.Leading = MatchupLeadingA
		}
	}
	out.A = sideA
	out.B = &sideB
	return out
}

// pastResult reads a frozen row from groupID's side.
func pastResult(m postgres.MatchupRow, groupID string) MatchupPastResult {
	out := MatchupPastResult{
		ID:        m.ID,
		WeekStart: m.WeekStart,
		Outcome:   string(outcomeFor(m, groupID)),
	}
	if m.Winner.Valid {
		out.WinnerGroupID = m.Winner.String
	}
	if m.GroupB == nil {
		return out
	}
	mine, theirs, opponent := m.ScoreA, m.ScoreB, *m.GroupB
	if strings.EqualFold(m.GroupB.ID, groupID) {
		mine, theirs, opponent = m.ScoreB, m.ScoreA, m.GroupA
	}
	face := matchupFace(opponent)
	out.Opponent = &face
	if mine.Valid {
		out.Score = scorePtr(mine.Float64)
	}
	if theirs.Valid {
		out.OpponentScore = scorePtr(theirs.Float64)
	}
	return out
}

func challengeFromSide(c postgres.MatchupChallengeRow, groupID string) MatchupChallenge {
	out := MatchupChallenge{
		ID:        c.ID,
		WeekStart: c.WeekStart,
		Status:    c.Status,
		CreatedAt: c.CreatedAt,
	}
	if strings.EqualFold(c.Challenged.ID, groupID) {
		out.Direction = "incoming"
		out.Opponent = matchupFace(c.Challenger)
	} else {
		out.Direction = "outgoing"
		out.Opponent = matchupFace(c.Challenged)
	}
	if c.AcceptedAt.Valid {
		at := c.AcceptedAt.Time.UTC()
		out.AcceptedAt = &at
	}
	return out
}

func containsGroupID(ids []string, id string) bool {
	for _, candidate := range ids {
		if strings.EqualFold(candidate, id) {
			return true
		}
	}
	return false
}

// GroupMatchup returns a cabal's live matchup, record, recent results, and (for members) its
// open challenges for next week.
func (s *MatchupService) GroupMatchup(ctx context.Context, accessToken, groupID string) (GroupMatchupView, error) {
	_, joinedIDs, err := s.home.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return GroupMatchupView{}, err
	}
	groupID = strings.ToLower(strings.TrimSpace(groupID))
	if !IsWellFormedGroupID(groupID) {
		return GroupMatchupView{}, ErrGroupNotFound
	}
	if _, found, err := s.store.GetGroupDirectoryRow(ctx, groupID); err != nil {
		return GroupMatchupView{}, err
	} else if !found {
		return GroupMatchupView{}, ErrGroupNotFound
	}

	now := s.now().UTC()
	week := MatchupWeekStart(now)
	view := GroupMatchupView{
		NextDrawAt:   week.Add(MatchupWeek),
		Recent:       []MatchupPastResult{},
		Challenges:   []MatchupChallenge{},
		CanChallenge: containsGroupID(joinedIDs, groupID),
	}

	rows, err := s.store.ListMatchupsForGroupsInWeek(ctx, week, []string{groupID})
	if err != nil {
		return GroupMatchupView{}, err
	}
	if len(rows) > 0 {
		scores, err := s.scoreMatchups(ctx, rows[:1])
		if err != nil {
			return GroupMatchupView{}, err
		}
		flip := rows[0].GroupB != nil && strings.EqualFold(rows[0].GroupB.ID, groupID)
		current := currentMatchup(rows[0], scores, flip, now)
		view.Current = &current
	}

	frozen, err := s.store.ListFrozenMatchupsForGroup(ctx, groupID, 0)
	if err != nil {
		return GroupMatchupView{}, err
	}
	view.Record = matchupRecordFor(groupID, frozen)
	for i, m := range frozen {
		if i == MatchupRecentLimit {
			break
		}
		view.Recent = append(view.Recent, pastResult(m, groupID))
	}

	if view.CanChallenge {
		challenges, err := s.store.ListMatchupChallengesForGroup(ctx, view.NextDrawAt, groupID)
		if err != nil {
			return GroupMatchupView{}, err
		}
		for _, c := range challenges {
			view.Challenges = append(view.Challenges, challengeFromSide(c, groupID))
		}
	}
	return view, nil
}

// HomeMatchups returns this week's matchups for every cabal the viewer is in, each oriented so
// the viewer's cabal is A, in the order the viewer joined them.
func (s *MatchupService) HomeMatchups(ctx context.Context, accessToken string) (HomeMatchupsView, error) {
	_, joinedIDs, err := s.home.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return HomeMatchupsView{}, err
	}
	now := s.now().UTC()
	week := MatchupWeekStart(now)
	view := HomeMatchupsView{
		WeekStart:  week,
		WeekEnd:    week.Add(MatchupWeek),
		DaysLeft:   matchupDaysLeft(week.Add(MatchupWeek), now),
		NextDrawAt: week.Add(MatchupWeek),
		Matchups:   []MatchupCurrent{},
		HasCabals:  len(joinedIDs) > 0,
	}
	_, drawn, err := s.store.GetMatchupWeek(ctx, week)
	if err != nil {
		return HomeMatchupsView{}, err
	}
	view.Drawn = drawn
	if !drawn || len(joinedIDs) == 0 {
		return view, nil
	}

	rows, err := s.store.ListMatchupsForGroupsInWeek(ctx, week, joinedIDs)
	if err != nil {
		return HomeMatchupsView{}, err
	}
	scores, err := s.scoreMatchups(ctx, rows)
	if err != nil {
		return HomeMatchupsView{}, err
	}
	order := make(map[string]int, len(joinedIDs))
	for i, id := range joinedIDs {
		order[strings.ToLower(id)] = i
	}
	type placed struct {
		rank    int
		current MatchupCurrent
	}
	var list []placed
	for _, m := range rows {
		// The viewer's side goes first; when they are in both cabals the row stays as drawn.
		flip := !containsGroupID(joinedIDs, m.GroupA.ID)
		viewerSide := m.GroupA.ID
		if flip {
			viewerSide = m.GroupB.ID
		}
		list = append(list, placed{rank: order[strings.ToLower(viewerSide)], current: currentMatchup(m, scores, flip, now)})
	}
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].rank < list[j-1].rank; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	for _, p := range list {
		view.Matchups = append(view.Matchups, p.current)
	}
	return view, nil
}

// SeasonTable ranks every cabal with a frozen result. The viewer's cabals are flagged, and
// any that fall below limit are appended with their real rank so they can always be found.
func (s *MatchupService) SeasonTable(ctx context.Context, accessToken string, limit int) (MatchupTableView, error) {
	_, joinedIDs, err := s.home.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return MatchupTableView{}, err
	}
	limit = ClampMatchupTableLimit(limit)
	now := s.now().UTC()

	through, found, err := s.store.LatestFinalMatchupWeek(ctx)
	if err != nil {
		return MatchupTableView{}, err
	}
	view := MatchupTableView{Rows: []MatchupTableRow{}}
	if !found {
		return view, nil
	}
	view.ThroughWeek = through

	table, ok := s.table.get(through, now)
	if !ok {
		pairs, err := s.store.ListFrozenMatchupPairs(ctx)
		if err != nil {
			return MatchupTableView{}, err
		}
		table = buildSeasonTable(pairs)
		s.table.put(through, now, table)
	}
	for i, row := range table {
		mine := containsGroupID(joinedIDs, row.GroupID)
		if i >= limit && !mine {
			continue
		}
		view.Rows = append(view.Rows, MatchupTableRow{
			Rank:       i + 1,
			GroupID:    row.GroupID,
			Name:       row.Name,
			PictureURL: row.PictureURL,
			Record:     row.Record,
			IsMine:     mine,
		})
	}
	return view, nil
}

// authorizeMatchupMember resolves the caller and checks they belong to groupID.
func (s *MatchupService) authorizeMatchupMember(ctx context.Context, accessToken, groupID string) (string, error) {
	user, joinedIDs, err := s.home.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return "", err
	}
	if !IsWellFormedGroupID(groupID) {
		return "", ErrGroupNotFound
	}
	if _, found, err := s.store.GetGroupByID(ctx, groupID); err != nil {
		return "", err
	} else if !found {
		return "", ErrGroupNotFound
	}
	if !containsGroupID(joinedIDs, groupID) {
		return "", ErrNotGroupMember
	}
	return user.ID, nil
}

// Challenge sends a friendly challenge from groupID to opponentID for next week. Sending the
// same challenge again returns the open one with created false.
func (s *MatchupService) Challenge(ctx context.Context, accessToken, groupID, opponentID string) (MatchupChallenge, bool, error) {
	groupID = strings.ToLower(strings.TrimSpace(groupID))
	opponentID = strings.ToLower(strings.TrimSpace(opponentID))
	userID, err := s.authorizeMatchupMember(ctx, accessToken, groupID)
	if err != nil {
		return MatchupChallenge{}, false, err
	}
	if !IsWellFormedGroupID(opponentID) {
		return MatchupChallenge{}, false, ErrMatchupOpponentNotFound
	}
	if opponentID == groupID {
		return MatchupChallenge{}, false, ErrMatchupChallengeSelf
	}
	if _, found, err := s.store.GetGroupByID(ctx, opponentID); err != nil {
		return MatchupChallenge{}, false, err
	} else if !found {
		return MatchupChallenge{}, false, ErrMatchupOpponentNotFound
	}

	week := MatchupWeekStart(s.now()).Add(MatchupWeek)
	if existing, found, err := s.store.FindOpenMatchupChallenge(ctx, week, groupID, opponentID); err != nil {
		return MatchupChallenge{}, false, err
	} else if found {
		return challengeFromSide(existing, groupID), false, nil
	}
	if _, found, err := s.store.FindOpenMatchupChallenge(ctx, week, opponentID, groupID); err != nil {
		return MatchupChallenge{}, false, err
	} else if found {
		return MatchupChallenge{}, false, &MatchupConflictError{Reason: MatchupConflictIncomingChallenge}
	}
	if taken, err := s.store.HasAcceptedMatchupChallenge(ctx, week, []string{groupID, opponentID}); err != nil {
		return MatchupChallenge{}, false, err
	} else if taken {
		return MatchupChallenge{}, false, &MatchupConflictError{Reason: MatchupConflictAlreadyMatched}
	}
	if open, err := s.store.CountOpenMatchupChallengesFrom(ctx, week, groupID); err != nil {
		return MatchupChallenge{}, false, err
	} else if open >= MatchupMaxOpenChallenges {
		return MatchupChallenge{}, false, &MatchupConflictError{Reason: MatchupConflictTooManyChallenges}
	}

	row, created, err := s.store.InsertMatchupChallenge(ctx, week, groupID, opponentID, userID)
	if err != nil {
		return MatchupChallenge{}, false, err
	}
	return challengeFromSide(row, groupID), created, nil
}

// AcceptChallenge locks a challenge sent to groupID, so the pair plays next week ahead of the
// draw. Accepting one already accepted returns it unchanged.
func (s *MatchupService) AcceptChallenge(ctx context.Context, accessToken, groupID, challengeID string) (MatchupChallenge, error) {
	groupID = strings.ToLower(strings.TrimSpace(groupID))
	challengeID = strings.ToLower(strings.TrimSpace(challengeID))
	userID, err := s.authorizeMatchupMember(ctx, accessToken, groupID)
	if err != nil {
		return MatchupChallenge{}, err
	}
	if !isUUID(challengeID) {
		return MatchupChallenge{}, ErrMatchupChallengeNotFound
	}
	challenge, found, err := s.store.GetMatchupChallenge(ctx, challengeID)
	if err != nil {
		return MatchupChallenge{}, err
	}
	if !found || !strings.EqualFold(challenge.Challenged.ID, groupID) {
		return MatchupChallenge{}, ErrMatchupChallengeNotFound
	}

	row, err := s.store.AcceptMatchupChallenge(ctx, challengeID, userID, s.now())
	switch {
	case errors.Is(err, postgres.ErrMatchupChallengeNotFound):
		return MatchupChallenge{}, ErrMatchupChallengeNotFound
	case errors.Is(err, postgres.ErrMatchupChallengeClosed), errors.Is(err, postgres.ErrMatchupWeekDrawn):
		return MatchupChallenge{}, &MatchupConflictError{Reason: MatchupConflictChallengeClosed}
	case errors.Is(err, postgres.ErrMatchupPairTaken):
		return MatchupChallenge{}, &MatchupConflictError{Reason: MatchupConflictAlreadyMatched}
	case err != nil:
		return MatchupChallenge{}, err
	}
	return challengeFromSide(row, groupID), nil
}
