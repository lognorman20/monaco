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
	ID            string
	GroupID       string
	PrivyWalletID string
	SolanaAddress string
	CreatedAt     time.Time
}

// InsertTreasury persists a treasury wallet row for a group.
func (s *Store) InsertTreasury(ctx context.Context, groupID string, privyWalletID string, solanaAddress string) (Treasury, error) {
	return insertTreasury(ctx, s.db, groupID, privyWalletID, solanaAddress)
}

// InsertTreasuryTx persists a treasury wallet row for a group within tx.
func (s *Store) InsertTreasuryTx(ctx context.Context, tx *sql.Tx, groupID string, privyWalletID string, solanaAddress string) (Treasury, error) {
	return insertTreasury(ctx, tx, groupID, privyWalletID, solanaAddress)
}

func insertTreasury(ctx context.Context, q queryRower, groupID string, privyWalletID string, solanaAddress string) (Treasury, error) {
	if groupID == "" {
		return Treasury{}, fmt.Errorf("group_id is required")
	}
	if privyWalletID == "" {
		return Treasury{}, fmt.Errorf("privy_wallet_id is required")
	}
	if solanaAddress == "" {
		return Treasury{}, fmt.Errorf("solana_address is required")
	}

	const insertSQL = `
INSERT INTO treasuries (group_id, privy_wallet_id, solana_address)
VALUES ($1, $2, $3)
RETURNING id, group_id, privy_wallet_id, solana_address, created_at`

	var treasury Treasury
	err := q.QueryRowContext(ctx, insertSQL, groupID, privyWalletID, solanaAddress).Scan(
		&treasury.ID,
		&treasury.GroupID,
		&treasury.PrivyWalletID,
		&treasury.SolanaAddress,
		&treasury.CreatedAt,
	)
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
SELECT id, group_id, privy_wallet_id, solana_address, created_at
FROM treasuries
WHERE group_id = $1`

	var treasury Treasury
	err := s.db.QueryRowContext(ctx, selectSQL, groupID).Scan(
		&treasury.ID,
		&treasury.GroupID,
		&treasury.PrivyWalletID,
		&treasury.SolanaAddress,
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
// dummy treasuries are excluded: they are not Privy wallets and must never be swept or read.
func (s *Store) ListTreasuries(ctx context.Context) ([]Treasury, error) {
	const selectSQL = `
SELECT t.id, t.group_id, t.privy_wallet_id, t.solana_address, t.created_at
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
			&treasury.PrivyWalletID,
			&treasury.SolanaAddress,
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
