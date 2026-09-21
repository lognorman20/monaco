package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Treasury is a row in treasuries.
type Treasury struct {
	ID        string
	GroupID   string
	WalletID  string
	Address   string
	CreatedAt time.Time
}

// InsertTreasury persists a treasury wallet row for a group.
func (s *Store) InsertTreasury(ctx context.Context, groupID string, walletID string, address string) (Treasury, error) {
	return insertTreasury(ctx, s.db, groupID, walletID, address)
}

// InsertTreasuryTx persists a treasury wallet row for a group within tx.
func (s *Store) InsertTreasuryTx(ctx context.Context, tx *sql.Tx, groupID string, walletID string, address string) (Treasury, error) {
	return insertTreasury(ctx, tx, groupID, walletID, address)
}

func insertTreasury(ctx context.Context, q queryRower, groupID string, walletID string, address string) (Treasury, error) {
	if groupID == "" {
		return Treasury{}, fmt.Errorf("group_id is required")
	}
	if walletID == "" {
		return Treasury{}, fmt.Errorf("wallet_id is required")
	}
	if address == "" {
		return Treasury{}, fmt.Errorf("address is required")
	}

	const insertSQL = `
INSERT INTO treasuries (group_id, wallet_id, address)
VALUES ($1, $2, $3)
ON CONFLICT (group_id) DO NOTHING
RETURNING id, group_id, wallet_id, address, created_at`

	var treasury Treasury
	err := q.QueryRowContext(ctx, insertSQL, groupID, walletID, address).Scan(
		&treasury.ID,
		&treasury.GroupID,
		&treasury.WalletID,
		&treasury.Address,
		&treasury.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Treasury{}, fmt.Errorf("insert treasury: %w", sql.ErrNoRows)
	}
	if err != nil {
		return Treasury{}, fmt.Errorf("insert treasury: %w", err)
	}

	return treasury, nil
}

// GetTreasuryByGroupID returns the treasury for groupID, or false if none exists.
func (s *Store) GetTreasuryByGroupID(ctx context.Context, groupID string) (Treasury, bool, error) {
	if groupID == "" {
		return Treasury{}, false, fmt.Errorf("group_id is required")
	}

	const selectSQL = `
SELECT id, group_id, wallet_id, address, created_at
FROM treasuries
WHERE group_id = $1`

	var treasury Treasury
	err := s.db.QueryRowContext(ctx, selectSQL, groupID).Scan(
		&treasury.ID,
		&treasury.GroupID,
		&treasury.WalletID,
		&treasury.Address,
		&treasury.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Treasury{}, false, nil
	}
	if err != nil {
		return Treasury{}, false, fmt.Errorf("get treasury by group_id: %w", err)
	}

	return treasury, true, nil
}

// ListTreasuries returns chain-backed group treasury wallet rows. Faker scale club (#153)
// dummy treasuries are excluded: they are not Dynamic wallets and must never be swept or read.
func (s *Store) ListTreasuries(ctx context.Context) ([]Treasury, error) {
	const selectSQL = `
SELECT t.id, t.group_id, t.wallet_id, t.address, t.created_at
FROM treasuries t
JOIN groups g ON g.id = t.group_id
WHERE NOT g.is_faker
ORDER BY t.created_at ASC`

	rows, err := s.db.QueryContext(ctx, selectSQL)
	if err != nil {
		return nil, fmt.Errorf("list treasuries: %w", err)
	}
	defer rows.Close()

	var treasuries []Treasury
	for rows.Next() {
		var treasury Treasury
		if err := rows.Scan(
			&treasury.ID,
			&treasury.GroupID,
			&treasury.WalletID,
			&treasury.Address,
			&treasury.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan treasury: %w", err)
		}
		treasuries = append(treasuries, treasury)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate treasuries: %w", err)
	}
	return treasuries, nil
}
