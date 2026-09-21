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
	ID        string
	UserID    string
	WalletID  string
	Address   string
	CreatedAt time.Time
}

// GetMemberWalletByUserID returns the member wallet for userID, or false if none exists.
func (s *Store) GetMemberWalletByUserID(ctx context.Context, userID string) (MemberWallet, bool, error) {
	if userID == "" {
		return MemberWallet{}, false, fmt.Errorf("user_id is required")
	}

	const selectSQL = `
SELECT id, user_id, wallet_id, address, created_at
FROM member_wallets
WHERE user_id = $1`

	var wallet MemberWallet
	err := s.db.QueryRowContext(ctx, selectSQL, userID).Scan(
		&wallet.ID,
		&wallet.UserID,
		&wallet.WalletID,
		&wallet.Address,
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

// ListMemberWallets returns member wallet rows for real users. Faker users (#153) are
// excluded (and a DB trigger rejects faker wallets) so the sweep scan never queries them.
func (s *Store) ListMemberWallets(ctx context.Context) ([]MemberWallet, error) {
	const selectSQL = `
SELECT w.id, w.user_id, w.wallet_id, w.address, w.created_at
FROM member_wallets w
JOIN users u ON u.id = w.user_id
WHERE NOT u.is_faker
ORDER BY w.created_at ASC`

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
			&wallet.WalletID,
			&wallet.Address,
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
func (s *Store) InsertMemberWallet(ctx context.Context, userID string, walletID string, address string) (MemberWallet, error) {
	if userID == "" {
		return MemberWallet{}, fmt.Errorf("user_id is required")
	}
	if walletID == "" {
		return MemberWallet{}, fmt.Errorf("wallet_id is required")
	}
	if address == "" {
		return MemberWallet{}, fmt.Errorf("address is required")
	}

	const insertSQL = `
INSERT INTO member_wallets (user_id, wallet_id, address)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO NOTHING
RETURNING id, user_id, wallet_id, address, created_at`

	var wallet MemberWallet
	err := s.db.QueryRowContext(ctx, insertSQL, userID, walletID, address).Scan(
		&wallet.ID,
		&wallet.UserID,
		&wallet.WalletID,
		&wallet.Address,
		&wallet.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		existing, ok, getErr := s.GetMemberWalletByUserID(ctx, userID)
		if getErr != nil {
			return MemberWallet{}, getErr
		}
		if !ok {
			return MemberWallet{}, fmt.Errorf("insert member wallet: conflict with no row")
		}
		return existing, nil
	}
	if err != nil {
		return MemberWallet{}, fmt.Errorf("insert member wallet: %w", err)
	}

	return wallet, nil
}
