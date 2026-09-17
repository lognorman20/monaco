package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// MemberWallet is a row in member_wallets.
type MemberWallet struct {
	ID            string
	UserID        string
	PrivyWalletID string
	SolanaAddress string
	CreatedAt     time.Time
}

// GetMemberWalletByUserID returns the member wallet for userID, or false if none exists.
func (s *Store) GetMemberWalletByUserID(ctx context.Context, userID string) (MemberWallet, bool, error) {
	if userID == "" {
		return MemberWallet{}, false, fmt.Errorf("user_id is required")
	}

	const selectSQL = `
SELECT id, user_id, privy_wallet_id, solana_address, created_at
FROM member_wallets
WHERE user_id = $1`

	var wallet MemberWallet
	err := s.db.QueryRowContext(ctx, selectSQL, userID).Scan(
		&wallet.ID,
		&wallet.UserID,
		&wallet.PrivyWalletID,
		&wallet.SolanaAddress,
		&wallet.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MemberWallet{}, false, nil
	}
	if err != nil {
		return MemberWallet{}, false, fmt.Errorf("get member wallet: %w", err)
	}

	return wallet, true, nil
}

// ListMemberWallets returns all member wallet rows.
func (s *Store) ListMemberWallets(ctx context.Context) ([]MemberWallet, error) {
	const selectSQL = `
SELECT id, user_id, privy_wallet_id, solana_address, created_at
FROM member_wallets
ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, selectSQL)
	if err != nil {
		return nil, fmt.Errorf("list member wallets: %w", err)
	}
	defer rows.Close()

	var wallets []MemberWallet
	for rows.Next() {
		var wallet MemberWallet
		if err := rows.Scan(
			&wallet.ID,
			&wallet.UserID,
			&wallet.PrivyWalletID,
			&wallet.SolanaAddress,
			&wallet.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan member wallet: %w", err)
		}
		wallets = append(wallets, wallet)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate member wallets: %w", err)
	}
	return wallets, nil
}

// InsertMemberWallet persists a new member wallet row for userID.
func (s *Store) InsertMemberWallet(ctx context.Context, userID string, privyWalletID string, solanaAddress string) (MemberWallet, error) {
	if userID == "" {
		return MemberWallet{}, fmt.Errorf("user_id is required")
	}
	if privyWalletID == "" {
		return MemberWallet{}, fmt.Errorf("privy_wallet_id is required")
	}
	if solanaAddress == "" {
		return MemberWallet{}, fmt.Errorf("solana_address is required")
	}

	const insertSQL = `
INSERT INTO member_wallets (user_id, privy_wallet_id, solana_address)
VALUES ($1, $2, $3)
RETURNING id, user_id, privy_wallet_id, solana_address, created_at`

	var wallet MemberWallet
	err := s.db.QueryRowContext(ctx, insertSQL, userID, privyWalletID, solanaAddress).Scan(
		&wallet.ID,
		&wallet.UserID,
		&wallet.PrivyWalletID,
		&wallet.SolanaAddress,
		&wallet.CreatedAt,
	)
	if err != nil {
		return MemberWallet{}, fmt.Errorf("insert member wallet: %w", err)
	}

	return wallet, nil
}
