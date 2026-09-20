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
	// Ghost is true for a faker user's position inside a real (non-faker) group (#153 mixed club).
	// Ghost positions are display-only: they never back treasury USDC, so they are excluded from
	// share sums, net USDC in, surplus credits, and NAV snapshots. Only ListPositionsByGroup sets it.
	Ghost bool
}

// potPositionPredicate keeps positions that are backed by the group's pot: every position in a
// faker group (the whole club is fake), and only non-faker users in a real group.
// Requires aliases u (users) and g (groups).
const potPositionPredicate = `(g.is_faker OR NOT u.is_faker)`

// GetPositionForUpdateTx row-locks the position for a user in a group within tx.
func (s *Store) GetPositionForUpdateTx(ctx context.Context, tx *sql.Tx, userID, groupID string) (PositionRow, bool, error) {
	if userID == "" || groupID == "" {
		return PositionRow{}, false, fmt.Errorf("user_id and group_id are required")
	}

	const selectSQL = `
SELECT user_id, group_id, share_units, amount_deposited, amount_withdrawn
FROM positions
WHERE user_id = $1 AND group_id = $2
FOR UPDATE`

	var row PositionRow
	err := tx.QueryRowContext(ctx, selectSQL, userID, groupID).Scan(
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
		return PositionRow{}, false, fmt.Errorf("get position for update: %w", err)
	}
	return row, true, nil
}

// NetUSDCInByGroupTx returns net deposited USDC micros for groupID within tx.
func (s *Store) NetUSDCInByGroupTx(ctx context.Context, tx *sql.Tx, groupID string) (int64, error) {
	if groupID == "" {
		return 0, fmt.Errorf("group_id is required")
	}
	const selectSQL = `
SELECT COALESCE(SUM(p.amount_deposited - p.amount_withdrawn), 0)
FROM positions p
JOIN users u ON u.id = p.user_id
JOIN groups g ON g.id = p.group_id
WHERE p.group_id = $1 AND ` + potPositionPredicate
	var total int64
	if err := tx.QueryRowContext(ctx, selectSQL, groupID).Scan(&total); err != nil {
		return 0, fmt.Errorf("net usdc in by group tx: %w", err)
	}
	return total, nil
}

// GetPositionTx returns the position for a user in a group within a transaction.
func (s *Store) GetPositionTx(ctx context.Context, tx *sql.Tx, userID, groupID string) (PositionRow, bool, error) {
	if userID == "" || groupID == "" {
		return PositionRow{}, false, fmt.Errorf("user_id and group_id are required")
	}

	const selectSQL = `
SELECT user_id, group_id, share_units, amount_deposited, amount_withdrawn
FROM positions
WHERE user_id = $1 AND group_id = $2`

	var row PositionRow
	err := tx.QueryRowContext(ctx, selectSQL, userID, groupID).Scan(
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

// SumAmountDepositedByGroup returns total amount_deposited micros for a group.
func (s *Store) SumAmountDepositedByGroup(ctx context.Context, groupID string) (int64, error) {
	if groupID == "" {
		return 0, fmt.Errorf("group_id is required")
	}

	const selectSQL = `
SELECT COALESCE(SUM(p.amount_deposited), 0)
FROM positions p
JOIN users u ON u.id = p.user_id
JOIN groups g ON g.id = p.group_id
WHERE p.group_id = $1 AND ` + potPositionPredicate
	var total int64
	if err := s.db.QueryRowContext(ctx, selectSQL, groupID).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum amount deposited: %w", err)
	}
	return total, nil
}

// SumShareUnitsByGroup returns total outstanding pot-backed share_units micros for a group.
// Ghost (faker-in-real-group) shares are excluded so they never dilute real equity.
func (s *Store) SumShareUnitsByGroup(ctx context.Context, groupID string) (int64, error) {
	if groupID == "" {
		return 0, fmt.Errorf("group_id is required")
	}
	return sumShareUnitsByGroupQuery(ctx, s.db, groupID)
}

// IncrementPositionTx credits share units and amount_deposited independently.
func (s *Store) IncrementPositionTx(ctx context.Context, tx *sql.Tx, userID, groupID string, shareUnits, amountDeposited int64) (PositionRow, error) {
	if userID == "" || groupID == "" {
		return PositionRow{}, fmt.Errorf("user_id and group_id are required")
	}
	if shareUnits <= 0 {
		return PositionRow{}, fmt.Errorf("share units must be positive")
	}
	if amountDeposited <= 0 {
		return PositionRow{}, fmt.Errorf("amount deposited must be positive")
	}

	const upsertSQL = `
INSERT INTO positions (user_id, group_id, share_units, amount_deposited, amount_withdrawn)
VALUES ($1, $2, $3, $4, 0)
ON CONFLICT (user_id, group_id) DO UPDATE
SET share_units = positions.share_units + EXCLUDED.share_units,
    amount_deposited = positions.amount_deposited + EXCLUDED.amount_deposited
RETURNING user_id, group_id, share_units, amount_deposited, amount_withdrawn`

	var row PositionRow
	err := tx.QueryRowContext(ctx, upsertSQL, userID, groupID, shareUnits, amountDeposited).Scan(
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

// CreditPositionShareUnitsTx row-locks a position and credits share_units without touching amount_deposited.
func (s *Store) CreditPositionShareUnitsTx(ctx context.Context, tx *sql.Tx, userID, groupID string, shareUnits int64) (PositionRow, error) {
	if userID == "" || groupID == "" {
		return PositionRow{}, fmt.Errorf("user_id and group_id are required")
	}
	if shareUnits <= 0 {
		return PositionRow{}, fmt.Errorf("share units must be positive")
	}

	const lockSQL = `
SELECT user_id, group_id, share_units, amount_deposited, amount_withdrawn
FROM positions
WHERE user_id = $1 AND group_id = $2
FOR UPDATE`

	var locked PositionRow
	err := tx.QueryRowContext(ctx, lockSQL, userID, groupID).Scan(
		&locked.UserID,
		&locked.GroupID,
		&locked.ShareUnits,
		&locked.AmountDeposited,
		&locked.AmountWithdrawn,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PositionRow{}, fmt.Errorf("position not found")
	}
	if err != nil {
		return PositionRow{}, fmt.Errorf("lock position: %w", err)
	}

	const updateSQL = `
UPDATE positions
SET share_units = share_units + $3
WHERE user_id = $1 AND group_id = $2
RETURNING user_id, group_id, share_units, amount_deposited, amount_withdrawn`

	var row PositionRow
	err = tx.QueryRowContext(ctx, updateSQL, userID, groupID, shareUnits).Scan(
		&row.UserID,
		&row.GroupID,
		&row.ShareUnits,
		&row.AmountDeposited,
		&row.AmountWithdrawn,
	)
	if err != nil {
		return PositionRow{}, fmt.Errorf("credit position share units: %w", err)
	}
	return row, nil
}

// DebitPositionShareUnitsTx row-locks a position and debits share_units without touching amount_withdrawn.
func (s *Store) DebitPositionShareUnitsTx(ctx context.Context, tx *sql.Tx, userID, groupID string, shareUnits int64) (PositionRow, error) {
	if userID == "" || groupID == "" {
		return PositionRow{}, fmt.Errorf("user_id and group_id are required")
	}
	if shareUnits <= 0 {
		return PositionRow{}, fmt.Errorf("share units must be positive")
	}

	const lockSQL = `
SELECT user_id, group_id, share_units, amount_deposited, amount_withdrawn
FROM positions
WHERE user_id = $1 AND group_id = $2
FOR UPDATE`

	var locked PositionRow
	err := tx.QueryRowContext(ctx, lockSQL, userID, groupID).Scan(
		&locked.UserID,
		&locked.GroupID,
		&locked.ShareUnits,
		&locked.AmountDeposited,
		&locked.AmountWithdrawn,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PositionRow{}, fmt.Errorf("position not found")
	}
	if err != nil {
		return PositionRow{}, fmt.Errorf("lock position: %w", err)
	}
	if locked.ShareUnits < shareUnits {
		return PositionRow{}, fmt.Errorf("insufficient share units")
	}

	const updateSQL = `
UPDATE positions
SET share_units = share_units - $3
WHERE user_id = $1 AND group_id = $2
RETURNING user_id, group_id, share_units, amount_deposited, amount_withdrawn`

	var row PositionRow
	err = tx.QueryRowContext(ctx, updateSQL, userID, groupID, shareUnits).Scan(
		&row.UserID,
		&row.GroupID,
		&row.ShareUnits,
		&row.AmountDeposited,
		&row.AmountWithdrawn,
	)
	if err != nil {
		return PositionRow{}, fmt.Errorf("debit position share units: %w", err)
	}
	return row, nil
}

// IncrementAmountWithdrawnTx adds to amount_withdrawn after confirmed payout.
func (s *Store) IncrementAmountWithdrawnTx(ctx context.Context, tx *sql.Tx, userID, groupID string, amount int64) (PositionRow, error) {
	if userID == "" || groupID == "" {
		return PositionRow{}, fmt.Errorf("user_id and group_id are required")
	}
	if amount <= 0 {
		return PositionRow{}, fmt.Errorf("amount must be positive")
	}

	const updateSQL = `
UPDATE positions
SET amount_withdrawn = amount_withdrawn + $3
WHERE user_id = $1 AND group_id = $2
RETURNING user_id, group_id, share_units, amount_deposited, amount_withdrawn`

	var row PositionRow
	err := tx.QueryRowContext(ctx, updateSQL, userID, groupID, amount).Scan(
		&row.UserID,
		&row.GroupID,
		&row.ShareUnits,
		&row.AmountDeposited,
		&row.AmountWithdrawn,
	)
	if err != nil {
		return PositionRow{}, fmt.Errorf("increment amount withdrawn: %w", err)
	}
	return row, nil
}

// ListPositionsByGroup returns all positions for a group, including ghost positions (see PositionRow.Ghost).
func (s *Store) ListPositionsByGroup(ctx context.Context, groupID string) ([]PositionRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

	const selectSQL = `
SELECT p.user_id, p.group_id, p.share_units, p.amount_deposited, p.amount_withdrawn,
       (u.is_faker AND NOT g.is_faker) AS ghost
FROM positions p
JOIN users u ON u.id = p.user_id
JOIN groups g ON g.id = p.group_id
WHERE p.group_id = $1
ORDER BY p.user_id`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list positions by group: %w", err)
	}
	defer rows.Close()

	var out []PositionRow
	for rows.Next() {
		var row PositionRow
		if err := rows.Scan(
			&row.UserID,
			&row.GroupID,
			&row.ShareUnits,
			&row.AmountDeposited,
			&row.AmountWithdrawn,
			&row.Ghost,
		); err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate positions: %w", err)
	}
	return out, nil
}
