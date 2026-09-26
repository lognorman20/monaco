package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// DeletedMemberDisplayName is what a deleted account is called wherever its old rows are
// still shown: a vote, a trade, a chat message, a proposal.
const DeletedMemberDisplayName = "Deleted member"

// ErrUserDeleted means a users row exists for the Privy login but the member deleted the
// account. Session opening refuses it rather than handing the row back.
var ErrUserDeleted = errors.New("user deleted")

// Account is a users row read regardless of deletion, for the routes that must tell a
// deleted account apart from one that never existed.
type Account struct {
	User
	DeletedAt sql.NullTime
}

// GetAccountByPrivyUserID returns the user for privyUserID whether or not it was deleted.
// GetUserByPrivyUserID hides deleted rows; this is for the delete route itself, which is
// idempotent, and for nothing that acts as the member.
func (s *Store) GetAccountByPrivyUserID(ctx context.Context, privyUserID string) (Account, bool, error) {
	if privyUserID == "" {
		return Account{}, false, fmt.Errorf("privy_user_id is required")
	}

	const selectSQL = `
SELECT id, privy_user_id, display_name, profile_photo_url, created_at, deleted_at
FROM users
WHERE privy_user_id = $1`

	var account Account
	err := s.db.QueryRowContext(ctx, selectSQL, privyUserID).Scan(
		&account.ID,
		&account.PrivyUserID,
		&account.DisplayName,
		&account.ProfilePhotoURL,
		&account.CreatedAt,
		&account.DeletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, fmt.Errorf("get account by privy_user_id: %w", err)
	}
	return account, true, nil
}

// GetUserPreferences returns the stored preferences object for a live user. found is false
// for an unknown or deleted user. Keys that were never written are simply absent; the app
// layer fills in defaults.
func (s *Store) GetUserPreferences(ctx context.Context, userID string) ([]byte, bool, error) {
	if userID == "" {
		return nil, false, fmt.Errorf("user_id is required")
	}
	const selectSQL = `SELECT preferences FROM users WHERE id = $1 AND deleted_at IS NULL`
	var raw []byte
	err := s.db.QueryRowContext(ctx, selectSQL, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get user preferences: %w", err)
	}
	return raw, true, nil
}

// MergeUserPreferences applies patch to the stored preferences in one statement and returns
// the stored object. The merge goes two levels deep: a top-level key whose patch value and
// stored value are both objects is merged key by key ({"notifications": {"chat": false}}
// leaves the other notification switches alone); anything else replaces. patch must already
// be validated. found is false for an unknown or deleted user.
func (s *Store) MergeUserPreferences(ctx context.Context, userID string, patch []byte) ([]byte, bool, error) {
	if userID == "" {
		return nil, false, fmt.Errorf("user_id is required")
	}
	if len(patch) == 0 {
		return nil, false, fmt.Errorf("preferences patch is required")
	}

	const updateSQL = `
UPDATE users u
SET preferences = u.preferences || COALESCE((
  SELECT jsonb_object_agg(
    p.key,
    CASE
      WHEN jsonb_typeof(p.value) = 'object' AND jsonb_typeof(u.preferences -> p.key) = 'object'
        THEN (u.preferences -> p.key) || p.value
      ELSE p.value
    END
  )
  FROM jsonb_each($2::jsonb) AS p
), '{}'::jsonb)
WHERE u.id = $1 AND u.deleted_at IS NULL
RETURNING u.preferences`

	var raw []byte
	err := s.db.QueryRowContext(ctx, updateSQL, userID, string(patch)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("merge user preferences: %w", err)
	}
	return raw, true, nil
}

// SliceHolding is a positive share-unit balance in one cabal.
type SliceHolding struct {
	GroupID    string
	GroupName  string
	ShareUnits int64
}

// PendingCashOut is a cash out that has debited share units but not settled.
type PendingCashOut struct {
	GroupID     string
	GroupName   string
	SliceMicros int64
}

// AccountHoldings is what stands between a member and deleting their account, as the
// database sees it. The account balance is on chain and is read by the caller.
type AccountHoldings struct {
	Slices []SliceHolding
	// CashOuts are redeem jobs still debited, selling or paying.
	CashOuts []PendingCashOut
	// PendingMicros is fund-to-cabal intents plus platform withdrawals still in flight:
	// USDC that is leaving the member's balance but has not landed.
	PendingMicros int64
}

// ListAccountHoldingsTx reads the member's slices, unsettled cash outs and in-flight
// transfers inside tx. It locks the member's positions rows, so run it under the member
// funds lock and a slice cannot be credited between the check and the delete.
func (s *Store) ListAccountHoldingsTx(ctx context.Context, tx *sql.Tx, userID string) (AccountHoldings, error) {
	return listAccountHoldings(ctx, tx, userID, true)
}

// ListAccountHoldings is ListAccountHoldingsTx without a transaction, for the read-only check.
func (s *Store) ListAccountHoldings(ctx context.Context, userID string) (AccountHoldings, error) {
	return listAccountHoldings(ctx, s.db, userID, false)
}

type accountQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func listAccountHoldings(ctx context.Context, q accountQuerier, userID string, lock bool) (AccountHoldings, error) {
	if userID == "" {
		return AccountHoldings{}, fmt.Errorf("user_id is required")
	}

	slicesSQL := `
SELECT p.group_id, g.name, p.share_units
FROM positions p
JOIN groups g ON g.id = p.group_id
WHERE p.user_id = $1 AND p.share_units > 0
ORDER BY lower(g.name), p.group_id`
	if lock {
		slicesSQL += `
FOR UPDATE OF p`
	}

	var holdings AccountHoldings
	rows, err := q.QueryContext(ctx, slicesSQL, userID)
	if err != nil {
		return AccountHoldings{}, fmt.Errorf("list account slices: %w", err)
	}
	for rows.Next() {
		var slice SliceHolding
		if err := rows.Scan(&slice.GroupID, &slice.GroupName, &slice.ShareUnits); err != nil {
			rows.Close()
			return AccountHoldings{}, fmt.Errorf("scan account slice: %w", err)
		}
		holdings.Slices = append(holdings.Slices, slice)
	}
	if err := rows.Close(); err != nil {
		return AccountHoldings{}, fmt.Errorf("close account slices: %w", err)
	}
	if err := rows.Err(); err != nil {
		return AccountHoldings{}, fmt.Errorf("iterate account slices: %w", err)
	}

	const cashOutsSQL = `
SELECT r.group_id, g.name, r.slice_usdc
FROM redeem_jobs r
JOIN groups g ON g.id = r.group_id
WHERE r.user_id = $1 AND r.status <> 'settled'
ORDER BY r.created_at, r.id`
	rows, err = q.QueryContext(ctx, cashOutsSQL, userID)
	if err != nil {
		return AccountHoldings{}, fmt.Errorf("list account cash outs: %w", err)
	}
	for rows.Next() {
		var cashOut PendingCashOut
		if err := rows.Scan(&cashOut.GroupID, &cashOut.GroupName, &cashOut.SliceMicros); err != nil {
			rows.Close()
			return AccountHoldings{}, fmt.Errorf("scan account cash out: %w", err)
		}
		holdings.CashOuts = append(holdings.CashOuts, cashOut)
	}
	if err := rows.Close(); err != nil {
		return AccountHoldings{}, fmt.Errorf("close account cash outs: %w", err)
	}
	if err := rows.Err(); err != nil {
		return AccountHoldings{}, fmt.Errorf("iterate account cash outs: %w", err)
	}

	const pendingSQL = `
SELECT
  (SELECT COALESCE(SUM(amount), 0) FROM deposits WHERE user_id = $1 AND status = 'pending') +
  (SELECT COALESCE(SUM(amount), 0) FROM platform_withdrawals WHERE user_id = $1 AND status = 'pending')`
	if err := q.QueryRowContext(ctx, pendingSQL, userID).Scan(&holdings.PendingMicros); err != nil {
		return AccountHoldings{}, fmt.Errorf("sum account pending transfers: %w", err)
	}
	return holdings, nil
}

// AnonymiseDeletedUserTx closes the account inside tx: it stamps deleted_at, renames the
// member "Deleted member", drops their photo and preferences, takes them out of every cabal
// (members and named voters) and withdraws their open join requests. Votes, trades, chat,
// comments, proposals, deposits, positions and the ledger are left exactly as they were, so
// a cabal's history still adds up; they now read as the deleted member's.
//
// It returns the number of cabals the member was taken out of. updated is false when the
// user is unknown or was already deleted, in which case nothing changed.
func (s *Store) AnonymiseDeletedUserTx(ctx context.Context, tx *sql.Tx, userID string, deletedAt time.Time) (cabalsLeft int64, updated bool, err error) {
	if userID == "" {
		return 0, false, fmt.Errorf("user_id is required")
	}

	const updateSQL = `
UPDATE users
SET deleted_at = $2,
    display_name = $3,
    profile_photo_url = NULL,
    preferences = '{}'::jsonb
WHERE id = $1 AND deleted_at IS NULL`
	result, err := tx.ExecContext(ctx, updateSQL, userID, deletedAt.UTC(), DeletedMemberDisplayName)
	if err != nil {
		return 0, false, fmt.Errorf("anonymise user: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("anonymise user rows affected: %w", err)
	}
	if affected == 0 {
		return 0, false, nil
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM group_voters WHERE user_id = $1`, userID); err != nil {
		return 0, false, fmt.Errorf("remove deleted user from voters: %w", err)
	}
	membership, err := tx.ExecContext(ctx, `DELETE FROM group_members WHERE user_id = $1`, userID)
	if err != nil {
		return 0, false, fmt.Errorf("remove deleted user from cabals: %w", err)
	}
	cabalsLeft, err = membership.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("remove deleted user from cabals rows affected: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM group_join_requests WHERE user_id = $1 AND status = 'pending'`, userID); err != nil {
		return 0, false, fmt.Errorf("withdraw deleted user's join requests: %w", err)
	}
	return cabalsLeft, true, nil
}
