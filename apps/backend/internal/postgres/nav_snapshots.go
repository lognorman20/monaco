package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/packages/domain"
)

const tokenAtomicScale int64 = 1_000_000

type navSnapshotQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// NavSnapshotReason records why a NAV snapshot was written.
type NavSnapshotReason string

const (
	NavSnapshotReasonDeposit             NavSnapshotReason = "deposit"
	NavSnapshotReasonTransactionConfirm  NavSnapshotReason = "transaction_confirm"
	NavSnapshotReasonWithdrawalPayout    NavSnapshotReason = "withdrawal_payout"
)

// NavSnapshotRow is a row in nav_snapshots.
type NavSnapshotRow struct {
	ID                string
	GroupID           string
	PotNavMicros      int64
	NavPerShareMicros int64
	TotalShares       int64
	Reason            NavSnapshotReason
	CreatedAt         time.Time
}

// NavSnapshotValues is the computed NAV state persisted to nav_snapshots.
type NavSnapshotValues struct {
	PotNavMicros      int64
	NavPerShareMicros int64
	TotalShares       int64
}

// InsertNavSnapshotTx writes a snapshot within an existing transaction.
func (s *Store) InsertNavSnapshotTx(ctx context.Context, tx *sql.Tx, groupID string, reason NavSnapshotReason, vals NavSnapshotValues) (NavSnapshotRow, error) {
	if groupID == "" {
		return NavSnapshotRow{}, fmt.Errorf("group_id is required")
	}
	if vals.PotNavMicros < 0 || vals.NavPerShareMicros < 0 || vals.TotalShares < 0 {
		return NavSnapshotRow{}, fmt.Errorf("nav snapshot values must be non-negative")
	}
	switch reason {
	case NavSnapshotReasonDeposit, NavSnapshotReasonTransactionConfirm, NavSnapshotReasonWithdrawalPayout:
	default:
		return NavSnapshotRow{}, fmt.Errorf("invalid nav snapshot reason %q", reason)
	}

	const insertSQL = `
INSERT INTO nav_snapshots (group_id, pot_nav_micros, nav_per_share_micros, total_shares, reason)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, group_id, pot_nav_micros, nav_per_share_micros, total_shares, reason, created_at`

	var row NavSnapshotRow
	err := tx.QueryRowContext(ctx, insertSQL, groupID, vals.PotNavMicros, vals.NavPerShareMicros, vals.TotalShares, string(reason)).Scan(
		&row.ID,
		&row.GroupID,
		&row.PotNavMicros,
		&row.NavPerShareMicros,
		&row.TotalShares,
		&row.Reason,
		&row.CreatedAt,
	)
	if err != nil {
		return NavSnapshotRow{}, fmt.Errorf("insert nav snapshot: %w", err)
	}
	return row, nil
}

// WriteNavSnapshot persists a snapshot outside an caller-managed transaction.
func (s *Store) WriteNavSnapshot(ctx context.Context, groupID string, reason NavSnapshotReason, vals NavSnapshotValues) (NavSnapshotRow, error) {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return NavSnapshotRow{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	row, err := s.InsertNavSnapshotTx(ctx, tx, groupID, reason, vals)
	if err != nil {
		return NavSnapshotRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return NavSnapshotRow{}, fmt.Errorf("commit nav snapshot: %w", err)
	}
	committed = true
	return row, nil
}

// WriteNavSnapshotOnDepositConfirmTx records pot NAV after a confirmed deposit credit.
func (s *Store) WriteNavSnapshotOnDepositConfirmTx(ctx context.Context, tx *sql.Tx, groupID string, treasuryUSDC int64) error {
	vals, err := s.computeNavSnapshotValues(ctx, tx, groupID, treasuryUSDC)
	if err != nil {
		return err
	}
	_, err = s.InsertNavSnapshotTx(ctx, tx, groupID, NavSnapshotReasonDeposit, vals)
	return err
}

// WriteNavSnapshotOnTransactionConfirm records pot NAV after a newly confirmed transaction.
func (s *Store) WriteNavSnapshotOnTransactionConfirm(ctx context.Context, groupID string, treasuryUSDC int64) error {
	vals, err := s.ComputeNavSnapshotValues(ctx, groupID, treasuryUSDC)
	if err != nil {
		return err
	}
	_, err = s.WriteNavSnapshot(ctx, groupID, NavSnapshotReasonTransactionConfirm, vals)
	return err
}

// RecordTreasuryHoldingsAndNavSnapshotOnConfirm verifies confirmed buy holdings and writes pot NAV.
func (s *Store) RecordTreasuryHoldingsAndNavSnapshotOnConfirm(
	ctx context.Context,
	groupID string,
	tx TransactionRow,
	treasuryUSDC int64,
) error {
	if groupID == "" {
		return fmt.Errorf("group_id is required")
	}
	if tx.Status != TransactionStatusConfirmed || tx.Action != TransactionActionBuy {
		return fmt.Errorf("transaction must be a confirmed buy")
	}
	if tx.OutputMint == "" {
		return fmt.Errorf("output mint is required")
	}

	holdings, err := s.ListNetTokenHoldingsByGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if !holdingIncludesMint(holdings, tx.OutputMint) {
		return fmt.Errorf("treasury holdings missing output mint %s", tx.OutputMint)
	}

	return s.WriteNavSnapshotOnTransactionConfirm(ctx, groupID, treasuryUSDC)
}

func holdingIncludesMint(holdings []TokenHoldingRow, mint string) bool {
	for _, holding := range holdings {
		if holding.Mint == mint && holding.Amount > 0 {
			return true
		}
	}
	return false
}

// WriteNavSnapshotOnWithdrawalPayoutTx records pot NAV after a confirmed withdrawal payout.
func (s *Store) WriteNavSnapshotOnWithdrawalPayoutTx(ctx context.Context, tx *sql.Tx, groupID string, treasuryUSDC int64) error {
	vals, err := s.computeNavSnapshotValues(ctx, tx, groupID, treasuryUSDC)
	if err != nil {
		return err
	}
	_, err = s.InsertNavSnapshotTx(ctx, tx, groupID, NavSnapshotReasonWithdrawalPayout, vals)
	return err
}

// ComputeNavSnapshotValues derives pot NAV from treasury USDC, outstanding shares, and confirmed fills.
func (s *Store) ComputeNavSnapshotValues(ctx context.Context, groupID string, treasuryUSDC int64) (NavSnapshotValues, error) {
	return s.computeNavSnapshotValues(ctx, s.db, groupID, treasuryUSDC)
}

// SumShareUnitsByGroupTx returns total share_units within tx.
func (s *Store) SumShareUnitsByGroupTx(ctx context.Context, tx *sql.Tx, groupID string) (int64, error) {
	return sumShareUnitsByGroupQuery(ctx, tx, groupID)
}

// ListNetTokenHoldingsByGroupTx aggregates holdings visible within tx.
func (s *Store) ListNetTokenHoldingsByGroupTx(ctx context.Context, tx *sql.Tx, groupID string) ([]TokenHoldingRow, error) {
	return listNetTokenHoldingsByGroupQuery(ctx, tx, groupID)
}

// GetFillDerivedCostBasisByOutputMintTx returns fill cost basis visible within tx.
func (s *Store) GetFillDerivedCostBasisByOutputMintTx(ctx context.Context, tx *sql.Tx, groupID, outputMint string) (int64, int64, bool, error) {
	return fillDerivedCostBasisByOutputMintQuery(ctx, tx, groupID, outputMint)
}

func (s *Store) computeNavSnapshotValues(ctx context.Context, q navSnapshotQuerier, groupID string, treasuryUSDC int64) (NavSnapshotValues, error) {
	if groupID == "" {
		return NavSnapshotValues{}, fmt.Errorf("group_id is required")
	}
	if treasuryUSDC < 0 {
		return NavSnapshotValues{}, fmt.Errorf("treasury usdc must be non-negative")
	}

	totalSharesMicro, err := sumShareUnitsByGroupQuery(ctx, q, groupID)
	if err != nil {
		return NavSnapshotValues{}, err
	}

	holdings, err := listNetTokenHoldingsByGroupQuery(ctx, q, groupID)
	if err != nil {
		return NavSnapshotValues{}, err
	}

	totalShares, err := domain.ShareUnitsMicrosToDomain(totalSharesMicro)
	if err != nil {
		return NavSnapshotValues{}, err
	}

	if len(holdings) == 0 {
		potNav := treasuryUSDC
		if potNav == 0 && totalSharesMicro > 0 {
			potNav = totalSharesMicro
		}
		return NavSnapshotValues{
			PotNavMicros:      potNav,
			NavPerShareMicros: domain.BootstrapSharePriceMicros,
			TotalShares:       totalSharesMicro,
		}, nil
	}

	markedHoldings := make([]domain.MarkedHolding, 0, len(holdings))
	for _, holding := range holdings {
		price, fillAmount, found, err := fillDerivedCostBasisByOutputMintQuery(ctx, q, groupID, holding.Mint)
		if err != nil {
			return NavSnapshotValues{}, err
		}
		if !found {
			return NavSnapshotValues{}, fmt.Errorf("cost basis not found for mint %s", holding.Mint)
		}
		if fillAmount <= 0 {
			return NavSnapshotValues{}, fmt.Errorf("cost basis amount must be positive for mint %s", holding.Mint)
		}
		markPerUnit, err := costBasisMarkPerUnitMicros(price, fillAmount)
		if err != nil {
			return NavSnapshotValues{}, err
		}
		units, err := tokenAtomicsToShareUnits(holding.Amount)
		if err != nil {
			return NavSnapshotValues{}, err
		}
		markedHoldings = append(markedHoldings, domain.MarkedHolding{
			Symbol:    symbolForMint(holding.Mint),
			Units:     string(units),
			MarkUsdc:  domain.USDCMicros(markPerUnit),
			CostBasis: domain.USDCMicros(markPerUnit),
		})
	}

	nav, err := domain.ComputePotNAV(domain.NavInput{
		Mode:         domain.NavMarked,
		TreasuryUsdc: domain.USDCMicros(treasuryUSDC),
		TotalShares:  totalShares,
		Holdings:     markedHoldings,
	})
	if err != nil {
		return NavSnapshotValues{}, fmt.Errorf("compute pot nav: %w", err)
	}

	return NavSnapshotValuesFromPotNAV(nav, totalSharesMicro), nil
}

func sumShareUnitsByGroupQuery(ctx context.Context, q navSnapshotQuerier, groupID string) (int64, error) {
	const selectSQL = `SELECT COALESCE(SUM(share_units), 0) FROM positions WHERE group_id = $1`
	var total int64
	if err := q.QueryRowContext(ctx, selectSQL, groupID).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum share units: %w", err)
	}
	return total, nil
}

func listNetTokenHoldingsByGroupQuery(ctx context.Context, q navSnapshotQuerier, groupID string) ([]TokenHoldingRow, error) {
	const selectSQL = `
WITH buys AS (
  SELECT output_mint AS mint, COALESCE(SUM(cost_basis_amount), 0) AS amount
  FROM transactions
  WHERE group_id = $1 AND action = 'buy' AND status = 'confirmed'
  GROUP BY output_mint
),
sells AS (
  SELECT input_mint AS mint, COALESCE(SUM(amount), 0) AS amount
  FROM transactions
  WHERE group_id = $1 AND action = 'sell' AND status = 'confirmed'
  GROUP BY input_mint
)
SELECT COALESCE(buys.mint, sells.mint) AS mint,
       COALESCE(buys.amount, 0) - COALESCE(sells.amount, 0) AS amount
FROM buys
FULL OUTER JOIN sells ON buys.mint = sells.mint
WHERE COALESCE(buys.amount, 0) - COALESCE(sells.amount, 0) > 0
ORDER BY mint`

	rows, err := q.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list net token holdings: %w", err)
	}
	defer rows.Close()

	var holdings []TokenHoldingRow
	for rows.Next() {
		var row TokenHoldingRow
		if err := rows.Scan(&row.Mint, &row.Amount); err != nil {
			return nil, fmt.Errorf("scan token holding: %w", err)
		}
		holdings = append(holdings, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate token holdings: %w", err)
	}
	return holdings, nil
}

func fillDerivedCostBasisByOutputMintQuery(ctx context.Context, q navSnapshotQuerier, groupID, outputMint string) (int64, int64, bool, error) {
	const selectSQL = `
SELECT cost_basis_price, cost_basis_amount
FROM transactions
WHERE group_id = $1 AND output_mint = $2 AND action = 'buy' AND status = 'confirmed'
ORDER BY confirmed_at DESC NULLS LAST, created_at DESC
LIMIT 1`

	var price, amount sql.NullInt64
	err := q.QueryRowContext(ctx, selectSQL, groupID, outputMint).Scan(&price, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("get fill-derived cost basis: %w", err)
	}
	if !price.Valid || !amount.Valid {
		return 0, 0, false, nil
	}
	return price.Int64, amount.Int64, true, nil
}

// NavSnapshotValuesFromPotNAV converts domain pot NAV into persisted micro values.
func NavSnapshotValuesFromPotNAV(nav domain.PotNAV, totalSharesMicro int64) NavSnapshotValues {
	return NavSnapshotValues{
		PotNavMicros:      int64(nav.TotalUsdc),
		NavPerShareMicros: int64(nav.PerShareUsdc),
		TotalShares:       totalSharesMicro,
	}
}

// ListNavSnapshotsByGroup returns snapshots newest first.
func (s *Store) ListNavSnapshotsByGroup(ctx context.Context, groupID string) ([]NavSnapshotRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

	const selectSQL = `
SELECT id, group_id, pot_nav_micros, nav_per_share_micros, total_shares, reason, created_at
FROM nav_snapshots
WHERE group_id = $1
ORDER BY created_at DESC, id DESC`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list nav snapshots: %w", err)
	}
	defer rows.Close()

	var out []NavSnapshotRow
	for rows.Next() {
		var row NavSnapshotRow
		var reason string
		if err := rows.Scan(
			&row.ID,
			&row.GroupID,
			&row.PotNavMicros,
			&row.NavPerShareMicros,
			&row.TotalShares,
			&reason,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan nav snapshot: %w", err)
		}
		row.Reason = NavSnapshotReason(reason)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nav snapshots: %w", err)
	}
	return out, nil
}

// CountNavSnapshotsByGroupAndReason counts persisted snapshots for assertions.
func (s *Store) CountNavSnapshotsByGroupAndReason(ctx context.Context, groupID string, reason NavSnapshotReason) (int, error) {
	if groupID == "" {
		return 0, fmt.Errorf("group_id is required")
	}

	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM nav_snapshots WHERE group_id = $1 AND reason = $2`, groupID, string(reason)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count nav snapshots: %w", err)
	}
	return count, nil
}

func tokenAtomicsToShareUnits(atomics int64) (domain.ShareUnits, error) {
	if atomics < 0 {
		return "", fmt.Errorf("token atomics must be non-negative")
	}
	if atomics == 0 {
		return domain.ShareUnits("0"), nil
	}
	r := new(big.Rat).SetFrac(big.NewInt(atomics), big.NewInt(tokenAtomicScale))
	s := strings.TrimRight(r.FloatString(6), "0")
	s = strings.TrimRight(s, ".")
	return domain.ShareUnits(s), nil
}

func symbolForMint(mint string) string {
	switch mint {
	case jupiter.AAPLxMint:
		return "AAPLx"
	default:
		return mint
	}
}

func costBasisMarkPerUnitMicros(totalUSDCMicros, tokenAtomics int64) (int64, error) {
	if totalUSDCMicros < 0 {
		return 0, fmt.Errorf("cost basis usdc must be non-negative")
	}
	if tokenAtomics <= 0 {
		return 0, fmt.Errorf("cost basis token amount must be positive")
	}
	mark := (totalUSDCMicros * tokenAtomicScale) / tokenAtomics
	if mark <= 0 {
		return 0, fmt.Errorf("derived mark per unit must be positive")
	}
	return mark, nil
}
