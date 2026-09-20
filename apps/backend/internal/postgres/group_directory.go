package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// GroupDirectoryRow is the public projection of a group used by discovery
// surfaces (search, platform leaderboard). It carries no treasury address and
// no member identities.
type GroupDirectoryRow struct {
	ID              string
	Name            string
	JoinMode        domain.JoinMode
	MemberCount     int
	NetUsdcInMicros int64
	CreatedAt       time.Time
	// PictureURL is the cabal picture, null until the creator uploads one.
	PictureURL sql.NullString
}

// GroupSearchRow is a directory row plus the keyset fields search pages on.
type GroupSearchRow struct {
	GroupDirectoryRow
	// MatchRank is 0 when the name starts with the query, 1 when it only contains it.
	MatchRank int
	// SortName is lower(name); pages order by (MatchRank, SortName, ID) in C collation.
	SortName string
}

// GroupSearchCursor is the last row of the previous page.
type GroupSearchCursor struct {
	MatchRank int
	SortName  string
	GroupID   string
}

// directorySelect projects public group fields. Member count and net USDC in
// are correlated subqueries against indexed primary keys (group_members and
// positions are both keyed by group_id first). Net USDC in counts pot-backed
// positions only (ghost faker positions in a real group are excluded), the
// same basis groupValuation uses.
const directorySelect = `
SELECT g.id,
       g.name,
       g.join_mode,
       (SELECT COUNT(*) FROM group_members gm WHERE gm.group_id = g.id) AS member_count,
       (SELECT COALESCE(SUM(p.amount_deposited - p.amount_withdrawn), 0)
          FROM positions p JOIN users u ON u.id = p.user_id
         WHERE p.group_id = g.id AND ` + potPositionPredicate + `) AS net_usdc_in,
       g.created_at,
       g.picture_url`

// ListGroupDirectory returns every group's public projection, oldest first.
func (s *Store) ListGroupDirectory(ctx context.Context) ([]GroupDirectoryRow, error) {
	rows, err := s.db.QueryContext(ctx, directorySelect+`
FROM groups g
ORDER BY g.created_at ASC, g.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list group directory: %w", err)
	}
	defer rows.Close()
	return scanDirectoryRows(rows)
}

// ListGroupDirectoryByIDs returns public projections for ids that exist, oldest first.
func (s *Store) ListGroupDirectoryByIDs(ctx context.Context, ids []string) ([]GroupDirectoryRow, error) {
	if len(ids) == 0 {
		return []GroupDirectoryRow{}, nil
	}
	rows, err := s.db.QueryContext(ctx, directorySelect+`
FROM groups g
WHERE g.id = ANY($1::uuid[])
ORDER BY g.created_at ASC, g.id ASC`, ids)
	if err != nil {
		return nil, fmt.Errorf("list group directory by ids: %w", err)
	}
	defer rows.Close()
	return scanDirectoryRows(rows)
}

// GetGroupDirectoryRow returns one group's public projection.
func (s *Store) GetGroupDirectoryRow(ctx context.Context, id string) (GroupDirectoryRow, bool, error) {
	rows, err := s.ListGroupDirectoryByIDs(ctx, []string{id})
	if err != nil {
		return GroupDirectoryRow{}, false, err
	}
	if len(rows) == 0 {
		return GroupDirectoryRow{}, false, nil
	}
	return rows[0], true, nil
}

func scanDirectoryRows(rows *sql.Rows) ([]GroupDirectoryRow, error) {
	out := []GroupDirectoryRow{}
	for rows.Next() {
		row, err := scanDirectoryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group directory: %w", err)
	}
	return out, nil
}

func scanDirectoryRow(rows *sql.Rows, extra ...any) (GroupDirectoryRow, error) {
	var row GroupDirectoryRow
	var joinMode string
	// This order must match directorySelect. Callers' extras (search's match_rank
	// and sort_name) are appended here and are selected last for the same reason.
	dest := []any{&row.ID, &row.Name, &joinMode, &row.MemberCount, &row.NetUsdcInMicros, &row.CreatedAt, &row.PictureURL}
	dest = append(dest, extra...)
	if err := rows.Scan(dest...); err != nil {
		return GroupDirectoryRow{}, fmt.Errorf("scan group directory row: %w", err)
	}
	row.JoinMode = domain.JoinMode(joinMode)
	row.CreatedAt = row.CreatedAt.UTC()
	return row, nil
}

// EscapeLikePattern escapes LIKE metacharacters so user text matches literally
// under `ESCAPE '\'`.
func EscapeLikePattern(raw string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(raw)
}

// SearchGroupDirectory finds groups whose name contains query, case-insensitive.
// Prefix matches rank ahead of substring matches; within a rank, names sort
// alphabetically (C collation on lower(name)) with id as the final tiebreak.
// Pass the previous page's last row as after for keyset pagination.
func (s *Store) SearchGroupDirectory(ctx context.Context, query string, limit int, after *GroupSearchCursor) ([]GroupSearchRow, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil, fmt.Errorf("search query is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("search limit must be positive")
	}
	pattern := EscapeLikePattern(query)

	base := `
WITH matched AS (
  SELECT g.*,
         CASE WHEN lower(g.name) LIKE $1 || '%' ESCAPE '\' THEN 0 ELSE 1 END AS match_rank,
         lower(g.name) COLLATE "C" AS sort_name
  FROM groups g
  WHERE lower(g.name) LIKE '%' || $1 || '%' ESCAPE '\'
)` + directorySelect + `,
       g.match_rank,
       g.sort_name
FROM matched g`

	var (
		rows *sql.Rows
		err  error
	)
	if after == nil {
		rows, err = s.db.QueryContext(ctx, base+`
ORDER BY g.match_rank ASC, g.sort_name ASC, g.id ASC
LIMIT $2`, pattern, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, base+`
WHERE (g.match_rank, g.sort_name, g.id) > ($2::int, $3::text COLLATE "C", $4::uuid)
ORDER BY g.match_rank ASC, g.sort_name ASC, g.id ASC
LIMIT $5`, pattern, after.MatchRank, after.SortName, after.GroupID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("search group directory: %w", err)
	}
	defer rows.Close()

	out := []GroupSearchRow{}
	for rows.Next() {
		var matchRank int
		var sortName string
		dir, err := scanDirectoryRow(rows, &matchRank, &sortName)
		if err != nil {
			return nil, err
		}
		out = append(out, GroupSearchRow{GroupDirectoryRow: dir, MatchRank: matchRank, SortName: sortName})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group search: %w", err)
	}
	return out, nil
}

// ListNavSnapshotsForGroupsWindow returns, per group, the latest snapshot
// strictly before since (the window baseline) followed by every snapshot at or
// after since. Rows are ordered by group, then created_at, then id.
func (s *Store) ListNavSnapshotsForGroupsWindow(ctx context.Context, groupIDs []string, since time.Time) ([]NavSnapshotRow, error) {
	if len(groupIDs) == 0 {
		return []NavSnapshotRow{}, nil
	}
	selectSQL := `
SELECT ` + navSnapshotColumns + ` FROM (
  (SELECT DISTINCT ON (group_id) ` + navSnapshotColumns + `
     FROM nav_snapshots
    WHERE group_id = ANY($1::uuid[]) AND created_at < $2
    ORDER BY group_id, created_at DESC, id DESC)
  UNION ALL
  (SELECT ` + navSnapshotColumns + `
     FROM nav_snapshots
    WHERE group_id = ANY($1::uuid[]) AND created_at >= $2)
) window_rows
ORDER BY group_id, created_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupIDs, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("list nav snapshots window: %w", err)
	}
	defer rows.Close()

	out := []NavSnapshotRow{}
	for rows.Next() {
		row, err := scanNavSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan nav snapshot: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nav snapshots window: %w", err)
	}
	return out, nil
}

// ContributionEvent is one confirmed movement of member money into or out of a
// group treasury: a credited deposit (positive) or a paid-out withdrawal
// (negative). At is the row's created_at; the ledger does not store a separate
// confirmation time.
type ContributionEvent struct {
	GroupID      string
	At           time.Time
	AmountMicros int64
}

// ListContributionEventsAfter returns confirmed deposits and withdrawals for
// groupIDs created strictly after after, oldest first. Ghost faker rows in a
// real group are skipped: they never moved money through the pot.
func (s *Store) ListContributionEventsAfter(ctx context.Context, groupIDs []string, after time.Time) ([]ContributionEvent, error) {
	if len(groupIDs) == 0 {
		return []ContributionEvent{}, nil
	}
	const selectSQL = `
SELECT group_id, created_at, amount FROM (
  SELECT d.group_id, d.created_at, d.amount, d.id
    FROM deposits d
    JOIN users u ON u.id = d.user_id
    JOIN groups g ON g.id = d.group_id
   WHERE d.group_id = ANY($1::uuid[]) AND d.status = 'confirmed' AND d.created_at > $2
     AND ` + potPositionPredicate + `
  UNION ALL
  SELECT w.group_id, w.created_at, -w.amount, w.id
    FROM withdrawals w
    JOIN users u ON u.id = w.user_id
    JOIN groups g ON g.id = w.group_id
   WHERE w.group_id = ANY($1::uuid[]) AND w.status = 'confirmed' AND w.created_at > $2
     AND ` + potPositionPredicate + `
) events
ORDER BY created_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupIDs, after.UTC())
	if err != nil {
		return nil, fmt.Errorf("list contribution events: %w", err)
	}
	defer rows.Close()

	out := []ContributionEvent{}
	for rows.Next() {
		var ev ContributionEvent
		if err := rows.Scan(&ev.GroupID, &ev.At, &ev.AmountMicros); err != nil {
			return nil, fmt.Errorf("scan contribution event: %w", err)
		}
		ev.At = ev.At.UTC()
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contribution events: %w", err)
	}
	return out, nil
}
