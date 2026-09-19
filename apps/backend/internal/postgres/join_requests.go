package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

type JoinRequest struct {
	ID              string
	GroupID         string
	UserID          string
	DisplayName     string
	ProfilePhotoURL string
	Status          domain.JoinRequestStatus
	CreatedAt       time.Time
	DecidedAt       sql.NullTime
}

func (s *Store) InsertJoinRequest(ctx context.Context, groupID, userID string) (JoinRequest, error) {
	const insertSQL = `INSERT INTO group_join_requests (group_id, user_id, status) VALUES ($1,$2,'pending') RETURNING id, group_id, user_id, status, created_at, decided_at`
	var row JoinRequest
	err := s.db.QueryRowContext(ctx, insertSQL, groupID, userID).Scan(&row.ID, &row.GroupID, &row.UserID, &row.Status, &row.CreatedAt, &row.DecidedAt)
	if err != nil {
		return JoinRequest{}, fmt.Errorf("insert join request: %w", err)
	}
	return row, nil
}

func (s *Store) GetPendingJoinRequest(ctx context.Context, groupID, userID string) (JoinRequest, bool, error) {
	const selectSQL = `SELECT id, group_id, user_id, status, created_at, decided_at FROM group_join_requests WHERE group_id=$1 AND user_id=$2 AND status='pending'`
	var row JoinRequest
	err := s.db.QueryRowContext(ctx, selectSQL, groupID, userID).Scan(&row.ID, &row.GroupID, &row.UserID, &row.Status, &row.CreatedAt, &row.DecidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return JoinRequest{}, false, nil
	}
	if err != nil {
		return JoinRequest{}, false, err
	}
	return row, true, nil
}

func (s *Store) ListPendingJoinRequests(ctx context.Context, groupID string) ([]JoinRequest, error) {
	const selectSQL = `SELECT r.id, r.group_id, r.user_id, COALESCE(u.display_name,''), COALESCE(u.profile_photo_url,''), r.status, r.created_at, r.decided_at FROM group_join_requests r JOIN users u ON u.id=r.user_id WHERE r.group_id=$1 AND r.status='pending' ORDER BY r.created_at ASC`
	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []JoinRequest
	for rows.Next() {
		var row JoinRequest
		if err := rows.Scan(&row.ID, &row.GroupID, &row.UserID, &row.DisplayName, &row.ProfilePhotoURL, &row.Status, &row.CreatedAt, &row.DecidedAt); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func (s *Store) GetJoinRequestByID(ctx context.Context, requestID string) (JoinRequest, bool, error) {
	const selectSQL = `SELECT id, group_id, user_id, status, created_at, decided_at FROM group_join_requests WHERE id=$1`
	var row JoinRequest
	err := s.db.QueryRowContext(ctx, selectSQL, requestID).Scan(&row.ID, &row.GroupID, &row.UserID, &row.Status, &row.CreatedAt, &row.DecidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return JoinRequest{}, false, nil
	}
	if err != nil {
		return JoinRequest{}, false, err
	}
	return row, true, nil
}

func (s *Store) UpdateJoinRequestStatusTx(ctx context.Context, tx *sql.Tx, requestID string, status domain.JoinRequestStatus) error {
	result, err := tx.ExecContext(ctx, `UPDATE group_join_requests SET status=$2, decided_at=now() WHERE id=$1 AND status='pending'`, requestID, string(status))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
