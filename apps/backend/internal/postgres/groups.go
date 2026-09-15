package postgres

import (
	"context"
	"fmt"
	"time"
)

// Group is a row in groups.
type Group struct {
	ID            string
	Name          string
	CreatorUserID string
	CreatedAt     time.Time
}

// InsertGroup persists a new group row.
func (s *Store) InsertGroup(ctx context.Context, name string, creatorUserID string) (Group, error) {
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
	err := s.db.QueryRowContext(ctx, insertSQL, name, creatorUserID).Scan(
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
