package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// WalletMaterial is MPC metadata persisted with a wallet row.
type WalletMaterial struct {
	WalletID     string
	Address      string
	UserID       string
	GroupID      string
	Metadata     json.RawMessage
	KeySharesEnc string
}

func (s *Store) GetMemberWalletMaterial(ctx context.Context, userID string) (WalletMaterial, bool, error) {
	const q = `
SELECT user_id, wallet_id, address, COALESCE(wallet_metadata, '{}'::jsonb), COALESCE(key_shares_enc, '')
FROM member_wallets WHERE user_id = $1`
	var m WalletMaterial
	err := s.db.QueryRowContext(ctx, q, userID).Scan(&m.UserID, &m.WalletID, &m.Address, &m.Metadata, &m.KeySharesEnc)
	if errors.Is(err, sql.ErrNoRows) {
		return WalletMaterial{}, false, nil
	}
	if err != nil {
		return WalletMaterial{}, false, fmt.Errorf("get member wallet material: %w", err)
	}
	return m, true, nil
}

func (s *Store) InsertMemberWalletMaterial(ctx context.Context, m WalletMaterial) (WalletMaterial, error) {
	if m.Metadata == nil {
		m.Metadata = json.RawMessage(`{}`)
	}
	const q = `
INSERT INTO member_wallets (user_id, wallet_id, address, wallet_metadata, key_shares_enc)
VALUES ($1, $2, $3, $4, $5)
RETURNING user_id, wallet_id, address, COALESCE(wallet_metadata, '{}'::jsonb), COALESCE(key_shares_enc, '')`
	var out WalletMaterial
	err := s.db.QueryRowContext(ctx, q, m.UserID, m.WalletID, m.Address, m.Metadata, m.KeySharesEnc).
		Scan(&out.UserID, &out.WalletID, &out.Address, &out.Metadata, &out.KeySharesEnc)
	if err != nil {
		return WalletMaterial{}, fmt.Errorf("insert member wallet material: %w", err)
	}
	return out, nil
}

func (s *Store) GetTreasuryMaterial(ctx context.Context, groupID string) (WalletMaterial, bool, error) {
	const q = `
SELECT group_id, wallet_id, address, COALESCE(wallet_metadata, '{}'::jsonb), COALESCE(key_shares_enc, '')
FROM treasuries WHERE group_id = $1`
	var m WalletMaterial
	err := s.db.QueryRowContext(ctx, q, groupID).Scan(&m.GroupID, &m.WalletID, &m.Address, &m.Metadata, &m.KeySharesEnc)
	if errors.Is(err, sql.ErrNoRows) {
		return WalletMaterial{}, false, nil
	}
	if err != nil {
		return WalletMaterial{}, false, fmt.Errorf("get treasury material: %w", err)
	}
	return m, true, nil
}

func (s *Store) InsertTreasuryMaterial(ctx context.Context, m WalletMaterial) (WalletMaterial, error) {
	if m.Metadata == nil {
		m.Metadata = json.RawMessage(`{}`)
	}
	const q = `
INSERT INTO treasuries (group_id, wallet_id, address, wallet_metadata, key_shares_enc)
VALUES ($1, $2, $3, $4, $5)
RETURNING group_id, wallet_id, address, COALESCE(wallet_metadata, '{}'::jsonb), COALESCE(key_shares_enc, '')`
	var out WalletMaterial
	err := s.db.QueryRowContext(ctx, q, m.GroupID, m.WalletID, m.Address, m.Metadata, m.KeySharesEnc).
		Scan(&out.GroupID, &out.WalletID, &out.Address, &out.Metadata, &out.KeySharesEnc)
	if err != nil {
		return WalletMaterial{}, fmt.Errorf("insert treasury material: %w", err)
	}
	return out, nil
}

func (s *Store) SetTreasuryGasToppedUpAt(ctx context.Context, groupID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE treasuries SET gas_topped_up_at = $2 WHERE group_id = $1`, groupID, at)
	if err != nil {
		return fmt.Errorf("set gas_topped_up_at: %w", err)
	}
	return nil
}
