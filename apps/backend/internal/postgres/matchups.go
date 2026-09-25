package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Weekly matchups: one row per pair per week (a bye is a row with no group_b), plus the week
// itself and the friendly challenges that lock a pair ahead of the draw. See migration 000035.

// Matchup week statuses.
const (
	MatchupWeekLive  = "live"
	MatchupWeekFinal = "final"
)

// Matchup challenge statuses.
const (
	MatchupChallengePending   = "pending"
	MatchupChallengeAccepted  = "accepted"
	MatchupChallengeScheduled = "scheduled"
	MatchupChallengeVoid      = "void"
	MatchupChallengeExpired   = "expired"
)

var (
	// ErrMatchupWeekNotFound means the week was never drawn.
	ErrMatchupWeekNotFound = errors.New("matchup week not found")
	// ErrMatchupDrawStale means an accepted challenge changed between planning the draw and
	// writing it. Nothing was written; the next attempt plans again.
	ErrMatchupDrawStale = errors.New("matchup draw planned against stale challenges")
	// ErrMatchupChallengeNotFound means no challenge has that id.
	ErrMatchupChallengeNotFound = errors.New("matchup challenge not found")
	// ErrMatchupChallengeClosed means the challenge is no longer pending (expired, void, or
	// already scheduled by a draw).
	ErrMatchupChallengeClosed = errors.New("matchup challenge closed")
	// ErrMatchupWeekDrawn means the challenge's week has already been drawn.
	ErrMatchupWeekDrawn = errors.New("matchup week already drawn")
	// ErrMatchupPairTaken means one of the two cabals already has an accepted challenge that week.
	ErrMatchupPairTaken = errors.New("cabal already has an opponent that week")
)

// MatchupWeek is a row in matchup_weeks.
type MatchupWeek struct {
	WeekStart   time.Time
	Status      string
	DrawnAt     time.Time
	FinalizedAt sql.NullTime
}

// MatchupGroup is the public face of one cabal in a matchup or challenge.
type MatchupGroup struct {
	ID          string
	Name        string
	PictureURL  sql.NullString
	MemberCount int
}

// MatchupRow is one pair (or bye) in one week.
type MatchupRow struct {
	ID        string
	WeekStart time.Time
	GroupA    MatchupGroup
	// GroupB is nil for a bye.
	GroupB      *MatchupGroup
	StartPotA   int64
	StartNetInA int64
	StartPotB   sql.NullInt64
	StartNetInB sql.NullInt64
	StartedAt   time.Time
	ScoreA      sql.NullFloat64
	ScoreB      sql.NullFloat64
	Winner      sql.NullString
	FrozenAt    sql.NullTime
	ChallengeID sql.NullString
}

// IsBye reports whether the row is a cabal sitting the week out.
func (m MatchupRow) IsBye() bool { return m.GroupB == nil }

// MatchupInsert is one pair (or bye, with GroupB empty) to write for a week.
type MatchupInsert struct {
	GroupA      string
	GroupB      string
	StartPotA   int64
	StartNetInA int64
	StartPotB   int64
	StartNetInB int64
	ChallengeID string
}

// MatchupDraw is everything one week's draw writes, in one transaction.
type MatchupDraw struct {
	WeekStart time.Time
	DrawnAt   time.Time
	Pairs     []MatchupInsert
	// PlannedChallengeIDs is every accepted challenge the draw was planned with. If the set
	// of accepted challenges for the week is different by the time the draw writes, the draw
	// writes nothing and returns ErrMatchupDrawStale.
	PlannedChallengeIDs []string
}

// MatchupResult freezes one row at the week's end. Nil scores are left null (a bye).
type MatchupResult struct {
	ID     string
	ScoreA *string
	ScoreB *string
	// Winner is the winning group id, or empty for a tie or a bye.
	Winner string
}

// MatchupChallengeRow is one friendly challenge with both cabals' public faces.
type MatchupChallengeRow struct {
	ID         string
	WeekStart  time.Time
	Challenger MatchupGroup
	Challenged MatchupGroup
	Status     string
	CreatedAt  time.Time
	AcceptedAt sql.NullTime
}

// matchupWeekLockKey serialises a week's draw with the challenge accepts for that week, so a
// challenge is never accepted into a week that is being drawn.
const matchupWeekLockSQL = `SELECT pg_advisory_xact_lock(hashtext('matchup_week:' || $1::date::text))`

func dateOnly(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// dateParam passes a week as a plain date. A time.Time would travel as a timestamptz and be
// cast to a date in the session's time zone, which is only the UTC day when the session is UTC.
func dateParam(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// GetMatchupWeek returns the week drawn on weekStart.
func (s *Store) GetMatchupWeek(ctx context.Context, weekStart time.Time) (MatchupWeek, bool, error) {
	var w MatchupWeek
	err := s.db.QueryRowContext(ctx, `
SELECT week_start, status, drawn_at, finalized_at
  FROM matchup_weeks
 WHERE week_start = $1::date`, dateParam(weekStart)).Scan(&w.WeekStart, &w.Status, &w.DrawnAt, &w.FinalizedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return MatchupWeek{}, false, nil
	}
	if err != nil {
		return MatchupWeek{}, false, fmt.Errorf("get matchup week: %w", err)
	}
	w.WeekStart = dateOnly(w.WeekStart)
	w.DrawnAt = w.DrawnAt.UTC()
	return w, true, nil
}

// ListLiveMatchupWeeksBefore returns every week still live that started before weekStart,
// oldest first: the weeks whose end has passed without being frozen.
func (s *Store) ListLiveMatchupWeeksBefore(ctx context.Context, weekStart time.Time) ([]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT week_start FROM matchup_weeks
 WHERE status = 'live' AND week_start < $1::date
 ORDER BY week_start ASC`, dateParam(weekStart))
	if err != nil {
		return nil, fmt.Errorf("list live matchup weeks: %w", err)
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var w time.Time
		if err := rows.Scan(&w); err != nil {
			return nil, fmt.Errorf("scan live matchup week: %w", err)
		}
		out = append(out, dateOnly(w))
	}
	return out, rows.Err()
}

// LatestFinalMatchupWeek returns the most recent frozen week, which is when records last changed.
func (s *Store) LatestFinalMatchupWeek(ctx context.Context) (time.Time, bool, error) {
	var w sql.NullTime
	if err := s.db.QueryRowContext(ctx, `SELECT max(week_start) FROM matchup_weeks WHERE status = 'final'`).Scan(&w); err != nil {
		return time.Time{}, false, fmt.Errorf("latest final matchup week: %w", err)
	}
	if !w.Valid {
		return time.Time{}, false, nil
	}
	return dateOnly(w.Time), true, nil
}

// CreateMatchupDraw writes a week, its pairs, and the challenges it settled, exactly once.
// It returns false without writing when the week was already drawn (another API instance got
// there first), and ErrMatchupDrawStale when the accepted challenges changed under the plan.
func (s *Store) CreateMatchupDraw(ctx context.Context, draw MatchupDraw) (bool, error) {
	week := dateOnly(draw.WeekStart)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin matchup draw: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, matchupWeekLockSQL, dateParam(week)); err != nil {
		return false, fmt.Errorf("lock matchup week: %w", err)
	}

	accepted, err := acceptedChallengeIDsTx(ctx, tx, week)
	if err != nil {
		return false, err
	}
	if !sameIDSet(accepted, draw.PlannedChallengeIDs) {
		return false, ErrMatchupDrawStale
	}

	var inserted time.Time
	err = tx.QueryRowContext(ctx, `
INSERT INTO matchup_weeks (week_start, status, drawn_at)
VALUES ($1::date, 'live', $2)
ON CONFLICT (week_start) DO NOTHING
RETURNING week_start`, dateParam(week), draw.DrawnAt.UTC()).Scan(&inserted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert matchup week: %w", err)
	}

	scheduled := make([]string, 0, len(draw.Pairs))
	for _, p := range draw.Pairs {
		var groupB, challengeID any
		var potB, netB any
		if p.GroupB != "" {
			groupB, potB, netB = p.GroupB, p.StartPotB, p.StartNetInB
		}
		if p.ChallengeID != "" {
			challengeID = p.ChallengeID
			scheduled = append(scheduled, p.ChallengeID)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO matchups (week_start, group_a, group_b, start_pot_a_micros, start_net_in_a_micros,
                      start_pot_b_micros, start_net_in_b_micros, started_at, challenge_id)
VALUES ($1::date, $2, $3, $4, $5, $6, $7, $8, $9)`,
			dateParam(week), p.GroupA, groupB, p.StartPotA, p.StartNetInA, potB, netB, draw.DrawnAt.UTC(), challengeID); err != nil {
			return false, fmt.Errorf("insert matchup: %w", err)
		}
	}

	// Challenges the draw paired are scheduled; accepted ones it could not honour (a side was
	// not eligible) are void; anything still pending missed its week.
	if len(scheduled) > 0 {
		if _, err := tx.ExecContext(ctx, `
UPDATE matchup_challenges SET status = 'scheduled'
 WHERE id = ANY($1::uuid[]) AND status = 'accepted'`, scheduled); err != nil {
			return false, fmt.Errorf("schedule matchup challenges: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE matchup_challenges SET status = 'void'
 WHERE week_start = $1::date AND status = 'accepted'`, dateParam(week)); err != nil {
		return false, fmt.Errorf("void matchup challenges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE matchup_challenges SET status = 'expired'
 WHERE week_start <= $1::date AND status = 'pending'`, dateParam(week)); err != nil {
		return false, fmt.Errorf("expire matchup challenges: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit matchup draw: %w", err)
	}
	return true, nil
}

func acceptedChallengeIDsTx(ctx context.Context, tx *sql.Tx, week time.Time) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM matchup_challenges WHERE week_start = $1::date AND status = 'accepted'`, dateParam(week))
	if err != nil {
		return nil, fmt.Errorf("list accepted matchup challenges: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan accepted matchup challenge: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func sameIDSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// FinalizeMatchupWeek freezes a live week's scores and winners, exactly once. It returns false
// without writing when the week is already final.
func (s *Store) FinalizeMatchupWeek(ctx context.Context, weekStart, finalizedAt time.Time, results []MatchupResult) (bool, error) {
	week := dateOnly(weekStart)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin matchup finalize: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM matchup_weeks WHERE week_start = $1::date FOR UPDATE`, dateParam(week)).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrMatchupWeekNotFound
	}
	if err != nil {
		return false, fmt.Errorf("lock matchup week for finalize: %w", err)
	}
	if status == MatchupWeekFinal {
		return false, nil
	}

	for _, r := range results {
		var winner any
		if r.Winner != "" {
			winner = r.Winner
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE matchups
   SET score_a = $2::numeric, score_b = $3::numeric, winner = $4::uuid, frozen_at = $5
 WHERE id = $1 AND week_start = $6::date AND frozen_at IS NULL`,
			r.ID, nullableString(r.ScoreA), nullableString(r.ScoreB), winner, finalizedAt.UTC(), dateParam(week)); err != nil {
			return false, fmt.Errorf("freeze matchup %s: %w", r.ID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE matchup_weeks SET status = 'final', finalized_at = $2 WHERE week_start = $1::date`, dateParam(week), finalizedAt.UTC()); err != nil {
		return false, fmt.Errorf("finalize matchup week: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit matchup finalize: %w", err)
	}
	return true, nil
}

func nullableString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

const matchupSelect = `
SELECT m.id, m.week_start,
       m.group_a, ga.name, ga.picture_url,
       (SELECT COUNT(*) FROM group_members gm WHERE gm.group_id = m.group_a),
       m.group_b, gb.name, gb.picture_url,
       CASE WHEN m.group_b IS NULL THEN 0
            ELSE (SELECT COUNT(*) FROM group_members gm WHERE gm.group_id = m.group_b) END,
       m.start_pot_a_micros, m.start_net_in_a_micros, m.start_pot_b_micros, m.start_net_in_b_micros,
       m.started_at, m.score_a::float8, m.score_b::float8, m.winner, m.frozen_at, m.challenge_id
  FROM matchups m
  JOIN groups ga ON ga.id = m.group_a
  LEFT JOIN groups gb ON gb.id = m.group_b`

func scanMatchupRows(rows *sql.Rows) ([]MatchupRow, error) {
	defer rows.Close()
	out := []MatchupRow{}
	for rows.Next() {
		var m MatchupRow
		var bID, bName sql.NullString
		var bPicture sql.NullString
		var bCount int
		if err := rows.Scan(
			&m.ID, &m.WeekStart,
			&m.GroupA.ID, &m.GroupA.Name, &m.GroupA.PictureURL, &m.GroupA.MemberCount,
			&bID, &bName, &bPicture, &bCount,
			&m.StartPotA, &m.StartNetInA, &m.StartPotB, &m.StartNetInB,
			&m.StartedAt, &m.ScoreA, &m.ScoreB, &m.Winner, &m.FrozenAt, &m.ChallengeID,
		); err != nil {
			return nil, fmt.Errorf("scan matchup: %w", err)
		}
		m.WeekStart = dateOnly(m.WeekStart)
		m.StartedAt = m.StartedAt.UTC()
		if bID.Valid {
			m.GroupB = &MatchupGroup{ID: bID.String, Name: bName.String, PictureURL: bPicture, MemberCount: bCount}
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matchups: %w", err)
	}
	return out, nil
}

// ListMatchupsForWeek returns every pair and bye drawn for a week.
func (s *Store) ListMatchupsForWeek(ctx context.Context, weekStart time.Time) ([]MatchupRow, error) {
	rows, err := s.db.QueryContext(ctx, matchupSelect+`
 WHERE m.week_start = $1::date
 ORDER BY m.created_at ASC, m.id ASC`, dateParam(weekStart))
	if err != nil {
		return nil, fmt.Errorf("list matchups for week: %w", err)
	}
	return scanMatchupRows(rows)
}

// ListMatchupsForGroupsInWeek returns the week's rows that involve any of groupIDs, on either side.
func (s *Store) ListMatchupsForGroupsInWeek(ctx context.Context, weekStart time.Time, groupIDs []string) ([]MatchupRow, error) {
	if len(groupIDs) == 0 {
		return []MatchupRow{}, nil
	}
	rows, err := s.db.QueryContext(ctx, matchupSelect+`
 WHERE m.week_start = $1::date
   AND (m.group_a = ANY($2::uuid[]) OR m.group_b = ANY($2::uuid[]))
 ORDER BY m.created_at ASC, m.id ASC`, dateParam(weekStart), groupIDs)
	if err != nil {
		return nil, fmt.Errorf("list matchups for groups: %w", err)
	}
	return scanMatchupRows(rows)
}

// ListFrozenMatchupsForGroup returns a cabal's frozen rows, byes included, newest first.
// limit <= 0 returns all of them.
func (s *Store) ListFrozenMatchupsForGroup(ctx context.Context, groupID string, limit int) ([]MatchupRow, error) {
	query := matchupSelect + `
 WHERE m.frozen_at IS NOT NULL AND (m.group_a = $1 OR m.group_b = $1)
 ORDER BY m.week_start DESC`
	args := []any{groupID}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list frozen matchups for group: %w", err)
	}
	return scanMatchupRows(rows)
}

// ListFrozenMatchupPairs returns every frozen pair (byes excluded), newest week first: the
// whole season's results, which the season table is folded from.
func (s *Store) ListFrozenMatchupPairs(ctx context.Context) ([]MatchupRow, error) {
	rows, err := s.db.QueryContext(ctx, matchupSelect+`
 WHERE m.frozen_at IS NOT NULL AND m.group_b IS NOT NULL
 ORDER BY m.week_start DESC, m.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list frozen matchup pairs: %w", err)
	}
	return scanMatchupRows(rows)
}

const matchupChallengeSelect = `
SELECT c.id, c.week_start,
       c.challenger_group_id, gc.name, gc.picture_url,
       (SELECT COUNT(*) FROM group_members gm WHERE gm.group_id = c.challenger_group_id),
       c.challenged_group_id, gd.name, gd.picture_url,
       (SELECT COUNT(*) FROM group_members gm WHERE gm.group_id = c.challenged_group_id),
       c.status, c.created_at, c.accepted_at
  FROM matchup_challenges c
  JOIN groups gc ON gc.id = c.challenger_group_id
  JOIN groups gd ON gd.id = c.challenged_group_id`

func scanMatchupChallenge(row interface{ Scan(dest ...any) error }) (MatchupChallengeRow, error) {
	var c MatchupChallengeRow
	err := row.Scan(
		&c.ID, &c.WeekStart,
		&c.Challenger.ID, &c.Challenger.Name, &c.Challenger.PictureURL, &c.Challenger.MemberCount,
		&c.Challenged.ID, &c.Challenged.Name, &c.Challenged.PictureURL, &c.Challenged.MemberCount,
		&c.Status, &c.CreatedAt, &c.AcceptedAt,
	)
	if err != nil {
		return MatchupChallengeRow{}, err
	}
	c.WeekStart = dateOnly(c.WeekStart)
	c.CreatedAt = c.CreatedAt.UTC()
	return c, nil
}

// GetMatchupChallenge returns one challenge.
func (s *Store) GetMatchupChallenge(ctx context.Context, id string) (MatchupChallengeRow, bool, error) {
	c, err := scanMatchupChallenge(s.db.QueryRowContext(ctx, matchupChallengeSelect+` WHERE c.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return MatchupChallengeRow{}, false, nil
	}
	if err != nil {
		return MatchupChallengeRow{}, false, fmt.Errorf("get matchup challenge: %w", err)
	}
	return c, true, nil
}

// FindOpenMatchupChallenge returns the pending challenge from challenger to challenged for a week.
func (s *Store) FindOpenMatchupChallenge(ctx context.Context, weekStart time.Time, challengerID, challengedID string) (MatchupChallengeRow, bool, error) {
	c, err := scanMatchupChallenge(s.db.QueryRowContext(ctx, matchupChallengeSelect+`
 WHERE c.week_start = $1::date AND c.challenger_group_id = $2 AND c.challenged_group_id = $3
   AND c.status = 'pending'`, dateParam(weekStart), challengerID, challengedID))
	if errors.Is(err, sql.ErrNoRows) {
		return MatchupChallengeRow{}, false, nil
	}
	if err != nil {
		return MatchupChallengeRow{}, false, fmt.Errorf("find open matchup challenge: %w", err)
	}
	return c, true, nil
}

// InsertMatchupChallenge opens a challenge. When the same challenger already has one open to
// the same cabal for that week, it returns that one with created false.
func (s *Store) InsertMatchupChallenge(ctx context.Context, weekStart time.Time, challengerID, challengedID, userID string) (MatchupChallengeRow, bool, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO matchup_challenges (week_start, challenger_group_id, challenged_group_id, created_by_user_id)
VALUES ($1::date, $2, $3, $4)
ON CONFLICT (week_start, challenger_group_id, challenged_group_id) WHERE status = 'pending' DO NOTHING
RETURNING id`, dateParam(weekStart), challengerID, challengedID, userID).Scan(&id)
	created := true
	if errors.Is(err, sql.ErrNoRows) {
		existing, found, findErr := s.FindOpenMatchupChallenge(ctx, weekStart, challengerID, challengedID)
		if findErr != nil {
			return MatchupChallengeRow{}, false, findErr
		}
		if !found {
			return MatchupChallengeRow{}, false, fmt.Errorf("insert matchup challenge: conflict without an open row")
		}
		return existing, false, nil
	}
	if err != nil {
		return MatchupChallengeRow{}, false, fmt.Errorf("insert matchup challenge: %w", err)
	}
	row, found, err := s.GetMatchupChallenge(ctx, id)
	if err != nil {
		return MatchupChallengeRow{}, false, err
	}
	if !found {
		return MatchupChallengeRow{}, false, ErrMatchupChallengeNotFound
	}
	return row, created, nil
}

// CountOpenMatchupChallengesFrom counts a cabal's pending challenges for a week.
func (s *Store) CountOpenMatchupChallengesFrom(ctx context.Context, weekStart time.Time, groupID string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM matchup_challenges
 WHERE week_start = $1::date AND challenger_group_id = $2 AND status = 'pending'`, dateParam(weekStart), groupID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count open matchup challenges: %w", err)
	}
	return n, nil
}

// HasAcceptedMatchupChallenge reports whether any of groupIDs already has an accepted
// challenge for the week, on either side.
func (s *Store) HasAcceptedMatchupChallenge(ctx context.Context, weekStart time.Time, groupIDs []string) (bool, error) {
	return hasAcceptedMatchupChallenge(ctx, s.db, weekStart, groupIDs)
}

func hasAcceptedMatchupChallenge(ctx context.Context, q queryRower, weekStart time.Time, groupIDs []string) (bool, error) {
	var taken bool
	if err := q.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM matchup_challenges
   WHERE week_start = $1::date AND status = 'accepted'
     AND (challenger_group_id = ANY($2::uuid[]) OR challenged_group_id = ANY($2::uuid[])))`,
		dateParam(weekStart), groupIDs).Scan(&taken); err != nil {
		return false, fmt.Errorf("check accepted matchup challenge: %w", err)
	}
	return taken, nil
}

// ListMatchupChallengesForGroup returns a cabal's pending and accepted challenges for a week,
// sent or received, oldest first.
func (s *Store) ListMatchupChallengesForGroup(ctx context.Context, weekStart time.Time, groupID string) ([]MatchupChallengeRow, error) {
	rows, err := s.db.QueryContext(ctx, matchupChallengeSelect+`
 WHERE c.week_start = $1::date AND c.status IN ('pending', 'accepted')
   AND (c.challenger_group_id = $2 OR c.challenged_group_id = $2)
 ORDER BY c.created_at ASC, c.id ASC`, dateParam(weekStart), groupID)
	if err != nil {
		return nil, fmt.Errorf("list matchup challenges: %w", err)
	}
	defer rows.Close()
	out := []MatchupChallengeRow{}
	for rows.Next() {
		c, err := scanMatchupChallenge(rows)
		if err != nil {
			return nil, fmt.Errorf("scan matchup challenge: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListAcceptedMatchupChallenges returns the week's accepted challenges, oldest acceptance first.
func (s *Store) ListAcceptedMatchupChallenges(ctx context.Context, weekStart time.Time) ([]MatchupChallengeRow, error) {
	rows, err := s.db.QueryContext(ctx, matchupChallengeSelect+`
 WHERE c.week_start = $1::date AND c.status = 'accepted'
 ORDER BY c.accepted_at ASC, c.id ASC`, dateParam(weekStart))
	if err != nil {
		return nil, fmt.Errorf("list accepted matchup challenges: %w", err)
	}
	defer rows.Close()
	out := []MatchupChallengeRow{}
	for rows.Next() {
		c, err := scanMatchupChallenge(rows)
		if err != nil {
			return nil, fmt.Errorf("scan accepted matchup challenge: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AcceptMatchupChallenge locks a pending challenge's pair for its week. It is serialised with
// that week's draw, so an accept either lands before the draw (and the draw honours it) or
// fails with ErrMatchupWeekDrawn. Accepting an already accepted challenge returns it unchanged.
func (s *Store) AcceptMatchupChallenge(ctx context.Context, id, userID string, acceptedAt time.Time) (MatchupChallengeRow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MatchupChallengeRow{}, fmt.Errorf("begin accept challenge: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var week time.Time
	var status, challenger, challenged string
	err = tx.QueryRowContext(ctx, `
SELECT week_start, status, challenger_group_id, challenged_group_id
  FROM matchup_challenges WHERE id = $1`, id).Scan(&week, &status, &challenger, &challenged)
	if errors.Is(err, sql.ErrNoRows) {
		return MatchupChallengeRow{}, ErrMatchupChallengeNotFound
	}
	if err != nil {
		return MatchupChallengeRow{}, fmt.Errorf("read challenge: %w", err)
	}
	week = dateOnly(week)

	if _, err := tx.ExecContext(ctx, matchupWeekLockSQL, dateParam(week)); err != nil {
		return MatchupChallengeRow{}, fmt.Errorf("lock matchup week: %w", err)
	}
	// Re-read under the lock: the draw may have settled it while we waited.
	if err := tx.QueryRowContext(ctx, `SELECT status FROM matchup_challenges WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		return MatchupChallengeRow{}, fmt.Errorf("lock challenge: %w", err)
	}
	switch status {
	case MatchupChallengeAccepted:
		_ = tx.Rollback()
		row, _, err := s.GetMatchupChallenge(ctx, id)
		return row, err
	case MatchupChallengePending:
	default:
		return MatchupChallengeRow{}, ErrMatchupChallengeClosed
	}

	var drawn bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM matchup_weeks WHERE week_start = $1::date)`, dateParam(week)).Scan(&drawn); err != nil {
		return MatchupChallengeRow{}, fmt.Errorf("check matchup week drawn: %w", err)
	}
	if drawn {
		return MatchupChallengeRow{}, ErrMatchupWeekDrawn
	}
	taken, err := hasAcceptedMatchupChallenge(ctx, tx, week, []string{challenger, challenged})
	if err != nil {
		return MatchupChallengeRow{}, err
	}
	if taken {
		return MatchupChallengeRow{}, ErrMatchupPairTaken
	}

	var acceptedBy any
	if userID != "" {
		acceptedBy = userID
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE matchup_challenges
   SET status = 'accepted', accepted_at = $2, accepted_by_user_id = $3
 WHERE id = $1`, id, acceptedAt.UTC(), acceptedBy); err != nil {
		return MatchupChallengeRow{}, fmt.Errorf("accept challenge: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return MatchupChallengeRow{}, fmt.Errorf("commit accept challenge: %w", err)
	}
	row, found, err := s.GetMatchupChallenge(ctx, id)
	if err != nil {
		return MatchupChallengeRow{}, err
	}
	if !found {
		return MatchupChallengeRow{}, ErrMatchupChallengeNotFound
	}
	return row, nil
}
