package postgres

import (
	"context"
	"fmt"
)

// Faker (#153) read helpers. Seeded demo rows are flagged with users.is_faker and
// groups.is_faker. These helpers let product code keep faker rows away from every
// Privy / Solana RPC / Jupiter path while still showing them on read surfaces.

// ListRealGroupIDs returns ids of non-faker groups (the only groups with chain-backed treasuries).
func (s *Store) ListRealGroupIDs(ctx context.Context) ([]string, error) {
	return s.listGroupIDsWhere(ctx, `NOT is_faker`, "list real group ids")
}

// ListFakerGroupIDs returns ids of wholly fake scale clubs, oldest first.
func (s *Store) ListFakerGroupIDs(ctx context.Context) ([]string, error) {
	return s.listGroupIDsWhere(ctx, `is_faker`, "list faker group ids")
}

func (s *Store) listGroupIDsWhere(ctx context.Context, where, op string) ([]string, error) {
	selectSQL := `SELECT id FROM groups WHERE ` + where + ` ORDER BY created_at ASC, id ASC`
	rows, err := s.db.QueryContext(ctx, selectSQL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("%s scan: %w", op, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s iterate: %w", op, err)
	}
	return ids, nil
}

// IsFakerUser reports whether userID is a seeded faker user. Missing users report false.
func (s *Store) IsFakerUser(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, fmt.Errorf("user_id is required")
	}
	var isFaker bool
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT is_faker FROM users WHERE id = $1), false)`, userID).Scan(&isFaker)
	if err != nil {
		return false, fmt.Errorf("is faker user: %w", err)
	}
	return isFaker, nil
}

// IsFakerGroup reports whether groupID is a faker scale club. Missing groups report false.
func (s *Store) IsFakerGroup(ctx context.Context, groupID string) (bool, error) {
	if groupID == "" {
		return false, fmt.Errorf("group_id is required")
	}
	var isFaker bool
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT is_faker FROM groups WHERE id = $1), false)`, groupID).Scan(&isFaker)
	if err != nil {
		return false, fmt.Errorf("is faker group: %w", err)
	}
	return isFaker, nil
}

// CanReadGroup reports whether userID may read groupID: members always, and any
// authenticated user for faker scale clubs (spectator reads). Mutations still require
// membership and reject faker groups separately.
func (s *Store) CanReadGroup(ctx context.Context, groupID, userID string) (bool, error) {
	if groupID == "" || userID == "" {
		return false, fmt.Errorf("group_id and user_id are required")
	}
	const selectSQL = `
SELECT EXISTS (SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2)
    OR EXISTS (SELECT 1 FROM groups WHERE id = $1 AND is_faker)`
	var ok bool
	if err := s.db.QueryRowContext(ctx, selectSQL, groupID, userID).Scan(&ok); err != nil {
		return false, fmt.Errorf("can read group: %w", err)
	}
	return ok, nil
}

// ListLiveGroupMemberIDs returns the member ids that count for live governance (voter set,
// majority). In a real group, faker ghost members are excluded so they can never block or
// decide a real proposal. In a faker group every member is returned (display only).
func (s *Store) ListLiveGroupMemberIDs(ctx context.Context, groupID string) ([]string, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}
	const selectSQL = `
SELECT gm.user_id
FROM group_members gm
JOIN users u ON u.id = gm.user_id
JOIN groups g ON g.id = gm.group_id
WHERE gm.group_id = $1 AND (g.is_faker OR NOT u.is_faker)
ORDER BY gm.joined_at`
	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list live group members: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan live group member: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FakerLedgerUSDC derives a faker club's display treasury USDC from its own ledger:
// net deposited − USDC spent on confirmed buys + USDC received from confirmed sells.
// Faker treasuries are dummy rows, so this replaces the Privy/RPC balance read.
// Returns an error for non-faker groups so real treasuries can never be mis-valued.
func (s *Store) FakerLedgerUSDC(ctx context.Context, groupID string) (int64, error) {
	if groupID == "" {
		return 0, fmt.Errorf("group_id is required")
	}
	const selectSQL = `
SELECT g.is_faker,
       COALESCE((SELECT SUM(p.amount_deposited - p.amount_withdrawn) FROM positions p WHERE p.group_id = g.id), 0)
     - COALESCE((SELECT SUM(COALESCE(t.cost_basis_price, t.amount)) FROM transactions t
                 WHERE t.group_id = g.id AND t.action = 'buy' AND t.status = 'confirmed'), 0)
     + COALESCE((SELECT SUM(COALESCE(t.cost_basis_amount, 0)) FROM transactions t
                 WHERE t.group_id = g.id AND t.action = 'sell' AND t.status = 'confirmed'), 0)
FROM groups g
WHERE g.id = $1`
	var isFaker bool
	var usdc int64
	if err := s.db.QueryRowContext(ctx, selectSQL, groupID).Scan(&isFaker, &usdc); err != nil {
		return 0, fmt.Errorf("faker ledger usdc: %w", err)
	}
	if !isFaker {
		return 0, fmt.Errorf("faker ledger usdc: group %s is not a faker group", groupID)
	}
	if usdc < 0 {
		usdc = 0
	}
	return usdc, nil
}
