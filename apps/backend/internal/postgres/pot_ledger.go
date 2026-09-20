package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// PotLedgerUSDC is the treasury USDC the group's own books account for: net USDC
// contributed by pot-backed positions, minus USDC spent on confirmed buys, plus USDC
// received from confirmed sells. Realized gains and losses are part of it; a payout still
// in flight is too, because amount_withdrawn only moves when the withdrawal confirms.
// On-chain USDC above this figure has not been credited to anyone yet.
func (s *Store) PotLedgerUSDC(ctx context.Context, groupID string) (int64, error) {
	return potLedgerUSDCQuery(ctx, s.db, groupID)
}

// PotLedgerUSDCTx is PotLedgerUSDC as visible within tx.
func (s *Store) PotLedgerUSDCTx(ctx context.Context, tx *sql.Tx, groupID string) (int64, error) {
	return potLedgerUSDCQuery(ctx, tx, groupID)
}

func potLedgerUSDCQuery(ctx context.Context, q navSnapshotQuerier, groupID string) (int64, error) {
	if groupID == "" {
		return 0, fmt.Errorf("group_id is required")
	}
	const selectSQL = `
SELECT COALESCE((SELECT SUM(p.amount_deposited - p.amount_withdrawn)
                 FROM positions p
                 JOIN users u ON u.id = p.user_id
                 JOIN groups g ON g.id = p.group_id
                 WHERE p.group_id = $1 AND ` + potPositionPredicate + `), 0)
     - COALESCE((SELECT SUM(COALESCE(t.cost_basis_price, t.amount)) FROM transactions t
                 WHERE t.group_id = $1 AND t.action = 'buy' AND t.status = 'confirmed'), 0)
     + COALESCE((SELECT SUM(COALESCE(t.cost_basis_amount, 0)) FROM transactions t
                 WHERE t.group_id = $1 AND t.action = 'sell' AND t.status = 'confirmed'), 0)`

	var usdc int64
	if err := q.QueryRowContext(ctx, selectSQL, groupID).Scan(&usdc); err != nil {
		return 0, fmt.Errorf("pot ledger usdc: %w", err)
	}
	return usdc, nil
}

// SumActiveRedeemJobShareUnits returns the share units debited by redeem jobs that have not
// paid out yet. Those units are off the positions table but are still a claim on the pot.
func (s *Store) SumActiveRedeemJobShareUnits(ctx context.Context, groupID string) (int64, error) {
	return sumActiveRedeemJobShareUnitsQuery(ctx, s.db, groupID)
}

// SumActiveRedeemJobShareUnitsTx is SumActiveRedeemJobShareUnits as visible within tx.
func (s *Store) SumActiveRedeemJobShareUnitsTx(ctx context.Context, tx *sql.Tx, groupID string) (int64, error) {
	return sumActiveRedeemJobShareUnitsQuery(ctx, tx, groupID)
}

func sumActiveRedeemJobShareUnitsQuery(ctx context.Context, q navSnapshotQuerier, groupID string) (int64, error) {
	if groupID == "" {
		return 0, fmt.Errorf("group_id is required")
	}
	const selectSQL = `
SELECT COALESCE(SUM(share_units), 0)
FROM redeem_jobs
WHERE group_id = $1 AND status IN ('debited', 'selling', 'paying') AND withdrawal_id IS NULL`

	var total int64
	if err := q.QueryRowContext(ctx, selectSQL, groupID).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum active redeem job share units: %w", err)
	}
	return total, nil
}

// HasPendingTransactionsForGroup reports whether a swap is still unresolved for the group.
// A pending swap may already have moved treasury USDC that the ledger does not show yet.
func (s *Store) HasPendingTransactionsForGroup(ctx context.Context, groupID string) (bool, error) {
	if groupID == "" {
		return false, fmt.Errorf("group_id is required")
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM transactions WHERE group_id = $1 AND status = 'pending')`, groupID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("has pending transactions for group: %w", err)
	}
	return exists, nil
}
