package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// InviteCode is a row in invite_codes. RevokedAt is null while the code is live.
type InviteCode struct {
	Code      string
	GroupID   string
	CreatedBy string
	CreatedAt time.Time
	RevokedAt sql.NullTime
}

// Live reports whether the code still opens its cabal.
func (c InviteCode) Live() bool {
	return !c.RevokedAt.Valid
}

// ErrInviteCodeTaken means the generated code already exists (live or revoked). The caller
// draws another one; the transaction is still usable because the insert does not raise.
var ErrInviteCodeTaken = errors.New("invite code taken")

const inviteCodeColumns = `code, group_id, created_by, created_at, revoked_at`

func scanInviteCode(row interface{ Scan(dest ...any) error }) (InviteCode, error) {
	var code InviteCode
	if err := row.Scan(&code.Code, &code.GroupID, &code.CreatedBy, &code.CreatedAt, &code.RevokedAt); err != nil {
		return InviteCode{}, err
	}
	code.CreatedAt = code.CreatedAt.UTC()
	if code.RevokedAt.Valid {
		code.RevokedAt.Time = code.RevokedAt.Time.UTC()
	}
	return code, nil
}

// GetInviteCode returns the row for code, live or revoked.
func (s *Store) GetInviteCode(ctx context.Context, code string) (InviteCode, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+inviteCodeColumns+` FROM invite_codes WHERE code = $1`, code)
	invite, err := scanInviteCode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return InviteCode{}, false, nil
	}
	if err != nil {
		return InviteCode{}, false, fmt.Errorf("get invite code: %w", err)
	}
	return invite, true, nil
}

// LockGroupForInviteTx takes the group row lock that serializes invite writes for one
// cabal, so two members asking for a code at once end up with the same one. It reports
// false when the group does not exist.
func (s *Store) LockGroupForInviteTx(ctx context.Context, tx *sql.Tx, groupID string) (bool, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM groups WHERE id = $1 FOR UPDATE`, groupID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock group for invite: %w", err)
	}
	return true, nil
}

// GetLiveInviteCodeTx returns the group's live code within tx.
func (s *Store) GetLiveInviteCodeTx(ctx context.Context, tx *sql.Tx, groupID string) (InviteCode, bool, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+inviteCodeColumns+` FROM invite_codes WHERE group_id = $1 AND revoked_at IS NULL`, groupID)
	invite, err := scanInviteCode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return InviteCode{}, false, nil
	}
	if err != nil {
		return InviteCode{}, false, fmt.Errorf("get live invite code: %w", err)
	}
	return invite, true, nil
}

// RevokeLiveInviteCodesTx revokes the group's live code within tx and returns how many
// rows it touched (0 or 1).
func (s *Store) RevokeLiveInviteCodesTx(ctx context.Context, tx *sql.Tx, groupID string) (int64, error) {
	result, err := tx.ExecContext(ctx, `UPDATE invite_codes SET revoked_at = now() WHERE group_id = $1 AND revoked_at IS NULL`, groupID)
	if err != nil {
		return 0, fmt.Errorf("revoke invite codes: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("revoke invite codes rows: %w", err)
	}
	return n, nil
}

// InsertInviteCodeTx stores a new live code within tx. A code that already exists returns
// ErrInviteCodeTaken without aborting tx (ON CONFLICT DO NOTHING), so the caller can retry
// with another code in the same transaction. Revoke the group's live code first: a second
// live code for one group violates invite_codes_one_live_per_group and aborts tx.
func (s *Store) InsertInviteCodeTx(ctx context.Context, tx *sql.Tx, code, groupID, createdBy string) (InviteCode, error) {
	row := tx.QueryRowContext(ctx, `
INSERT INTO invite_codes (code, group_id, created_by)
VALUES ($1, $2, $3)
ON CONFLICT (code) DO NOTHING
RETURNING `+inviteCodeColumns, code, groupID, createdBy)
	invite, err := scanInviteCode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return InviteCode{}, ErrInviteCodeTaken
	}
	if err != nil {
		return InviteCode{}, fmt.Errorf("insert invite code: %w", err)
	}
	return invite, nil
}
