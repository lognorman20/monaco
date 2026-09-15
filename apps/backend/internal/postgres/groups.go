package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Group is a row in groups.
type Group struct {
	ID            string
	Name          string
	CreatorUserID string
	CreatedAt     time.Time
}

// InsertGroup persists a new group row.
func (s *Store) InsertGroup(ctx context.Context, name string, creatorUserID string) (Group, error) {
	return insertGroup(ctx, s.db, name, creatorUserID)
}

// InsertGroupTx persists a new group row within tx.
func (s *Store) InsertGroupTx(ctx context.Context, tx *sql.Tx, name string, creatorUserID string) (Group, error) {
	return insertGroup(ctx, tx, name, creatorUserID)
}

func insertGroup(ctx context.Context, q queryRower, name string, creatorUserID string) (Group, error) {
	if name == "" {
		return Group{}, fmt.Errorf("name is required")
	}
	if creatorUserID == "" {
		return Group{}, fmt.Errorf("creator_user_id is required")
	}

	const insertSQL = `
INSERT INTO groups (name, creator_user_id)
VALUES ($1, $2)
RETURNING id, name, creator_user_id, created_at`

	var group Group
	err := q.QueryRowContext(ctx, insertSQL, name, creatorUserID).Scan(
		&group.ID,
		&group.Name,
		&group.CreatorUserID,
		&group.CreatedAt,
	)
	if err != nil {
		return Group{}, fmt.Errorf("insert group: %w", err)
	}

	return group, nil
}

// GetGroupByID returns the group for id, or false if none exists.
func (s *Store) GetGroupByID(ctx context.Context, id string) (Group, bool, error) {
	if id == "" {
		return Group{}, false, fmt.Errorf("id is required")
	}

	const selectSQL = `
SELECT id, name, creator_user_id, created_at
FROM groups
WHERE id = $1`

	var group Group
	err := s.db.QueryRowContext(ctx, selectSQL, id).Scan(
		&group.ID,
		&group.Name,
		&group.CreatorUserID,
		&group.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Group{}, false, nil
	}
	if err != nil {
		return Group{}, false, fmt.Errorf("get group by id: %w", err)
	}

	return group, true, nil
}
