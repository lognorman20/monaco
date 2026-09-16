package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PositionRow is a row in positions.
type PositionRow struct {
	UserID          string
	GroupID         string
	ShareUnits      int64
	AmountDeposited int64
	AmountWithdrawn int64
}

// GetPosition returns the position for a user in a group.
func (s *Store) GetPosition(ctx context.Context, userID, groupID string) (PositionRow, bool, error) {
	if userID == "" || groupID == "" {
		return PositionRow{}, false, fmt.Errorf("user_id and group_id are required")
	}

	const selectSQL = `
SELECT user_id, group_id, share_units, amount_deposited, amount_withdrawn
FROM positions
WHERE user_id = $1 AND group_id = $2`

	var row PositionRow
	err := s.db.QueryRowContext(ctx, selectSQL, userID, groupID).Scan(
		&row.UserID,
		&row.GroupID,
		&row.ShareUnits,
		&row.AmountDeposited,
		&row.AmountWithdrawn,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PositionRow{}, false, nil
	}
	if err != nil {
		return PositionRow{}, false, fmt.Errorf("get position: %w", err)
	}
	return row, true, nil
}

// IncrementPositionTx credits share units and amount_deposited by amount.
func (s *Store) IncrementPositionTx(ctx context.Context, tx *sql.Tx, userID, groupID string, amount int64) (PositionRow, error) {
	if userID == "" || groupID == "" {
		return PositionRow{}, fmt.Errorf("user_id and group_id are required")
	}
	if amount <= 0 {
		return PositionRow{}, fmt.Errorf("amount must be positive")
	}

	const upsertSQL = `
INSERT INTO positions (user_id, group_id, share_units, amount_deposited, amount_withdrawn)
VALUES ($1, $2, $3, $3, 0)
ON CONFLICT (user_id, group_id) DO UPDATE
SET share_units = positions.share_units + EXCLUDED.share_units,
    amount_deposited = positions.amount_deposited + EXCLUDED.amount_deposited
RETURNING user_id, group_id, share_units, amount_deposited, amount_withdrawn`

	var row PositionRow
	err := tx.QueryRowContext(ctx, upsertSQL, userID, groupID, amount).Scan(
		&row.UserID,
		&row.GroupID,
		&row.ShareUnits,
		&row.AmountDeposited,
		&row.AmountWithdrawn,
	)
	if err != nil {
		return PositionRow{}, fmt.Errorf("increment position: %w", err)
	}
	return row, nil
}
