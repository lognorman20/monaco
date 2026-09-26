package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrWatchlistFull means the member already follows the most stocks a watchlist holds.
	ErrWatchlistFull = errors.New("watchlist is full")
	// ErrWatchlistOrderMismatch means a reorder did not name exactly the stocks on the
	// watchlist: one was added or removed elsewhere since the client last read it.
	ErrWatchlistOrderMismatch = errors.New("watchlist changed")
)

// WatchlistEntry is one row in watchlist.
type WatchlistEntry struct {
	Symbol    string
	Position  int
	CreatedAt time.Time
}

// ListWatchlist returns userID's watchlist in position order.
func (s *Store) ListWatchlist(ctx context.Context, userID string) ([]WatchlistEntry, error) {
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	return listWatchlist(ctx, s.db, userID)
}

type watchlistQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func listWatchlist(ctx context.Context, q watchlistQueryer, userID string) ([]WatchlistEntry, error) {
	const selectSQL = `
SELECT symbol, position, created_at
FROM watchlist
WHERE user_id = $1
ORDER BY position ASC, created_at ASC, symbol ASC`
	rows, err := q.QueryContext(ctx, selectSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("list watchlist: %w", err)
	}
	defer rows.Close()

	out := []WatchlistEntry{}
	for rows.Next() {
		var entry WatchlistEntry
		if err := rows.Scan(&entry.Symbol, &entry.Position, &entry.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan watchlist: %w", err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate watchlist: %w", err)
	}
	return out, nil
}

// AddToWatchlist puts symbol at the end of userID's watchlist. A stock already on it (in
// any casing) stays where it is and comes back with created false. Past maxSymbols the
// add is refused with ErrWatchlistFull.
//
// The member's users row is locked for the length of the add, so two adds racing from
// two devices take distinct positions and cannot both slip under the cap.
func (s *Store) AddToWatchlist(ctx context.Context, userID, symbol string, maxSymbols int) (WatchlistEntry, bool, error) {
	symbol = strings.TrimSpace(symbol)
	if userID == "" || symbol == "" {
		return WatchlistEntry{}, false, fmt.Errorf("user_id and symbol are required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WatchlistEntry{}, false, fmt.Errorf("begin add to watchlist: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockWatchlistOwner(ctx, tx, userID); err != nil {
		return WatchlistEntry{}, false, err
	}

	var existing WatchlistEntry
	err = tx.QueryRowContext(ctx, `
SELECT symbol, position, created_at FROM watchlist
WHERE user_id = $1 AND lower(symbol) = lower($2)`, userID, symbol).
		Scan(&existing.Symbol, &existing.Position, &existing.CreatedAt)
	switch {
	case err == nil:
		return existing, false, tx.Commit()
	case !errors.Is(err, sql.ErrNoRows):
		return WatchlistEntry{}, false, fmt.Errorf("find watchlist entry: %w", err)
	}

	var count, next int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(MAX(position) + 1, 0) FROM watchlist WHERE user_id = $1`, userID).
		Scan(&count, &next); err != nil {
		return WatchlistEntry{}, false, fmt.Errorf("count watchlist: %w", err)
	}
	if maxSymbols > 0 && count >= maxSymbols {
		return WatchlistEntry{}, false, ErrWatchlistFull
	}

	var entry WatchlistEntry
	if err := tx.QueryRowContext(ctx, `
INSERT INTO watchlist (user_id, symbol, position)
VALUES ($1, $2, $3)
RETURNING symbol, position, created_at`, userID, symbol, next).
		Scan(&entry.Symbol, &entry.Position, &entry.CreatedAt); err != nil {
		return WatchlistEntry{}, false, fmt.Errorf("insert watchlist entry: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return WatchlistEntry{}, false, fmt.Errorf("commit add to watchlist: %w", err)
	}
	return entry, true, nil
}

// RemoveFromWatchlist drops symbol (any casing) from userID's watchlist and reports whether
// it was there. The positions after it keep their gap; order is all positions promise.
func (s *Store) RemoveFromWatchlist(ctx context.Context, userID, symbol string) (bool, error) {
	symbol = strings.TrimSpace(symbol)
	if userID == "" || symbol == "" {
		return false, fmt.Errorf("user_id and symbol are required")
	}
	result, err := s.db.ExecContext(ctx, `
DELETE FROM watchlist WHERE user_id = $1 AND lower(symbol) = lower($2)`, userID, symbol)
	if err != nil {
		return false, fmt.Errorf("remove watchlist entry: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("remove watchlist entry rows: %w", err)
	}
	return n > 0, nil
}

// ReorderWatchlist rewrites userID's positions to follow symbols, which must name every
// stock on the watchlist exactly once (any casing). Anything else is
// ErrWatchlistOrderMismatch and nothing moves.
func (s *Store) ReorderWatchlist(ctx context.Context, userID string, symbols []string) ([]WatchlistEntry, error) {
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reorder watchlist: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockWatchlistOwner(ctx, tx, userID); err != nil {
		return nil, err
	}
	current, err := listWatchlist(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	stored := make(map[string]string, len(current))
	for _, entry := range current {
		stored[strings.ToLower(entry.Symbol)] = entry.Symbol
	}
	if len(symbols) != len(current) {
		return nil, ErrWatchlistOrderMismatch
	}
	ordered := make([]string, 0, len(symbols))
	seen := make(map[string]bool, len(symbols))
	for _, raw := range symbols {
		key := strings.ToLower(strings.TrimSpace(raw))
		canonical, ok := stored[key]
		if !ok || seen[key] {
			return nil, ErrWatchlistOrderMismatch
		}
		seen[key] = true
		ordered = append(ordered, canonical)
	}

	if len(ordered) > 0 {
		if _, err := tx.ExecContext(ctx, `
UPDATE watchlist AS w
SET position = o.ord - 1
FROM unnest($2::text[]) WITH ORDINALITY AS o(symbol, ord)
WHERE w.user_id = $1 AND w.symbol = o.symbol`, userID, ordered); err != nil {
			return nil, fmt.Errorf("reorder watchlist: %w", err)
		}
	}
	out, err := listWatchlist(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reorder watchlist: %w", err)
	}
	return out, nil
}

// IsWatching reports whether symbol (any casing) is on userID's watchlist.
func (s *Store) IsWatching(ctx context.Context, userID, symbol string) (bool, error) {
	var watching bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM watchlist WHERE user_id = $1 AND lower(symbol) = lower($2))`,
		userID, strings.TrimSpace(symbol)).Scan(&watching)
	if err != nil {
		return false, fmt.Errorf("check watchlist: %w", err)
	}
	return watching, nil
}

// lockWatchlistOwner serialises one member's watchlist and alert writes behind their users row.
func lockWatchlistOwner(ctx context.Context, tx *sql.Tx, userID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("lock user %s: %w", userID, sql.ErrNoRows)
	}
	if err != nil {
		return fmt.Errorf("lock user: %w", err)
	}
	return nil
}
