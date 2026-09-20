package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// GroupMessageRow is a row in group_messages joined with the author's display name.
type GroupMessageRow struct {
	ID                string
	GroupID           string
	AuthorID          string
	AuthorDisplayName string
	Body              string
	CreatedAt         time.Time
}

// GroupMessageCursor is the keyset position of a message: (created_at, id).
type GroupMessageCursor struct {
	CreatedAt time.Time
	ID        string
}

// InsertGroupMessage persists one chat message and returns it with the author's display name.
func (s *Store) InsertGroupMessage(ctx context.Context, groupID, authorID, body string) (GroupMessageRow, error) {
	if groupID == "" || authorID == "" {
		return GroupMessageRow{}, fmt.Errorf("group_id and author_id are required")
	}
	const insertSQL = `
WITH inserted AS (
  INSERT INTO group_messages (group_id, author_id, body)
  VALUES ($1, $2, $3)
  RETURNING id, group_id, author_id, body, created_at
)
SELECT i.id, i.group_id, i.author_id, COALESCE(u.display_name, ''), i.body, i.created_at
FROM inserted i
JOIN users u ON u.id = i.author_id`
	var row GroupMessageRow
	err := s.db.QueryRowContext(ctx, insertSQL, groupID, authorID, body).Scan(
		&row.ID, &row.GroupID, &row.AuthorID, &row.AuthorDisplayName, &row.Body, &row.CreatedAt,
	)
	if err != nil {
		return GroupMessageRow{}, fmt.Errorf("insert group message: %w", err)
	}
	return row, nil
}

// ListGroupMessages returns up to limit messages for groupID, newest first.
// When before is non-nil, only messages strictly older than that keyset position are returned.
func (s *Store) ListGroupMessages(ctx context.Context, groupID string, before *GroupMessageCursor, limit int) ([]GroupMessageRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}

	var (
		rows *sql.Rows
		err  error
	)
	if before == nil {
		const selectSQL = `
SELECT m.id, m.group_id, m.author_id, COALESCE(u.display_name, ''), m.body, m.created_at
FROM group_messages m
JOIN users u ON u.id = m.author_id
WHERE m.group_id = $1
ORDER BY m.created_at DESC, m.id DESC
LIMIT $2`
		rows, err = s.db.QueryContext(ctx, selectSQL, groupID, limit)
	} else {
		const selectSQL = `
SELECT m.id, m.group_id, m.author_id, COALESCE(u.display_name, ''), m.body, m.created_at
FROM group_messages m
JOIN users u ON u.id = m.author_id
WHERE m.group_id = $1
  AND (m.created_at, m.id) < ($2::timestamptz, $3::uuid)
ORDER BY m.created_at DESC, m.id DESC
LIMIT $4`
		rows, err = s.db.QueryContext(ctx, selectSQL, groupID, before.CreatedAt, before.ID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list group messages: %w", err)
	}
	defer rows.Close()

	items := make([]GroupMessageRow, 0, limit)
	for rows.Next() {
		var row GroupMessageRow
		if err := rows.Scan(&row.ID, &row.GroupID, &row.AuthorID, &row.AuthorDisplayName, &row.Body, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan group message: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group messages: %w", err)
	}
	return items, nil
}
