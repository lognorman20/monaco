package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// GroupMember is a row in group_members.
type GroupMember struct {
	GroupID  string
	UserID   string
	JoinedAt time.Time
}

// InsertGroupMemberTx adds a user to group_members within tx.
func (s *Store) InsertGroupMemberTx(ctx context.Context, tx *sql.Tx, groupID, userID string) error {
	if groupID == "" || userID == "" {
		return fmt.Errorf("group_id and user_id are required")
	}
	const insertSQL = `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT (group_id, user_id) DO NOTHING`
	if _, err := tx.ExecContext(ctx, insertSQL, groupID, userID); err != nil {
		return fmt.Errorf("insert group member: %w", err)
	}
	return nil
}

// IsGroupMember reports whether userID belongs to groupID.
func (s *Store) IsGroupMember(ctx context.Context, groupID, userID string) (bool, error) {
	if groupID == "" || userID == "" {
		return false, fmt.Errorf("group_id and user_id are required")
	}
	const selectSQL = `SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2 LIMIT 1`
	var exists int
	err := s.db.QueryRowContext(ctx, selectSQL, groupID, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("is group member: %w", err)
	}
	return true, nil
}

// ListGroupMemberIDs returns user ids for groupID.
func (s *Store) ListGroupMemberIDs(ctx context.Context, groupID string) ([]string, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}
	const selectSQL = `SELECT user_id FROM group_members WHERE group_id = $1 ORDER BY joined_at`
	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan group member: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// InsertGroupVotersTx persists named voter subset rows within tx.
func (s *Store) InsertGroupVotersTx(ctx context.Context, tx *sql.Tx, groupID string, userIDs []string) error {
	if groupID == "" {
		return fmt.Errorf("group_id is required")
	}
	const insertSQL = `INSERT INTO group_voters (group_id, user_id) VALUES ($1, $2) ON CONFLICT (group_id, user_id) DO NOTHING`
	for _, userID := range userIDs {
		if userID == "" {
			return fmt.Errorf("user_id is required")
		}
		if _, err := tx.ExecContext(ctx, insertSQL, groupID, userID); err != nil {
			return fmt.Errorf("insert group voter: %w", err)
		}
	}
	return nil
}

// ListGroupVoterIDs returns user ids in group_voters for groupID.
func (s *Store) ListGroupVoterIDs(ctx context.Context, groupID string) ([]string, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}
	const selectSQL = `SELECT user_id FROM group_voters WHERE group_id = $1 ORDER BY user_id`
	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group voters: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan group voter: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DeleteGroupMemberTx removes membership and named-voter rows within tx.
func (s *Store) DeleteGroupMemberTx(ctx context.Context, tx *sql.Tx, groupID, userID string) error {
	if groupID == "" || userID == "" {
		return fmt.Errorf("group_id and user_id are required")
	}
	const deleteVoterSQL = `DELETE FROM group_voters WHERE group_id = $1 AND user_id = $2`
	if _, err := tx.ExecContext(ctx, deleteVoterSQL, groupID, userID); err != nil {
		return fmt.Errorf("delete group voter: %w", err)
	}
	const deleteMemberSQL = `DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`
	result, err := tx.ExecContext(ctx, deleteMemberSQL, groupID, userID)
	if err != nil {
		return fmt.Errorf("delete group member: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete group member rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("group member not found")
	}
	return nil
}

// ListUserGroupIDs returns group ids where userID is a member.
func (s *Store) ListUserGroupIDs(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	const selectSQL = `SELECT group_id FROM group_members WHERE user_id = $1 ORDER BY joined_at`
	rows, err := s.db.QueryContext(ctx, selectSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("list user groups: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan user group: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
