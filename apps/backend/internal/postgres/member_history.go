package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Kinds of row in a member's money history. They are the member's words for what happened,
// not the table the row came from: a cash out is a redeem job while it runs and a withdrawal
// once it has paid.
const (
	MemberHistoryDeposit    = "deposit"
	MemberHistoryFund       = "fund"
	MemberHistoryCashOut    = "cash_out"
	MemberHistoryWithdrawal = "withdrawal"
	MemberHistoryBuy        = "buy"
	MemberHistorySell       = "sell"
	MemberHistoryBotBuy     = "bot_buy"
	MemberHistoryBotSell    = "bot_sell"
)

// MemberHistoryRow is one thing that moved a member's money, as the ledger recorded it.
//
// Buys and sells are the cabal's whole trade: the member's slice of it is applied by the
// caller, which knows the member's share of the pot. Every other row is the member's own.
type MemberHistoryRow struct {
	ID string
	// Kind is one of the MemberHistory* constants.
	Kind string
	// RawStatus is the source table's own status word ("confirmed", "debited", "failed: sweep").
	RawStatus string
	// GroupID is empty for a withdrawal from the account balance, which belongs to no cabal.
	GroupID   string
	GroupName string
	// UsdcMicros is the dollar amount. A sell that has not confirmed has no proceeds yet,
	// so it is not valid there rather than invented.
	UsdcMicros sql.NullInt64
	// TokenAtomics is the stock amount of a trade in the mint's atomic units. A buy that has
	// not confirmed has no fill yet.
	TokenAtomics  sql.NullInt64
	Mint          string
	TokenDecimals int
	At            time.Time
	// TransactionID is set on buys and sells only.
	TransactionID string
}

// MemberHistoryCursor is the position after which the next page starts: rows strictly older,
// or as old with a smaller id.
type MemberHistoryCursor struct {
	At time.Time
	ID string
}

// MemberHistoryQuery selects a page of one member's history.
type MemberHistoryQuery struct {
	UserID string
	// Kinds keeps only these kinds. Empty keeps every kind.
	Kinds []string
	// After is the last row of the previous page; nil starts at the newest row.
	After *MemberHistoryCursor
	Limit int
}

// ListMemberHistory returns one member's money history, newest first, ordered by (at, id)
// so a page boundary never splits or repeats a row even when two rows share a timestamp.
//
// Sources, one branch each:
//   - fund: the member's deposits into a cabal (member balance to cabal pot).
//   - cash_out: a paid cash out (withdrawals), one still running (a redeem job that has not
//     paid), and one whose transfer failed or expired (a redeem payout that moved no money;
//     its job was rolled back and the share units returned).
//   - withdrawal: USDC sent from the account balance to an outside address.
//   - buy, sell, bot_buy, bot_sell: the trades of every cabal the member holds a slice of,
//     from the member's first confirmed deposit into that cabal onward. A trade from before
//     the member had any stake was not theirs.
//
// Nothing here records USDC arriving in the account balance from outside: that balance is
// read from the chain, so there is no `deposit` row to return yet.
func (s *Store) ListMemberHistory(ctx context.Context, q MemberHistoryQuery) ([]MemberHistoryRow, error) {
	if q.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if q.Limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	kinds := q.Kinds
	if kinds == nil {
		kinds = []string{}
	}
	var afterAt sql.NullTime
	var afterID sql.NullString
	if q.After != nil {
		afterAt = sql.NullTime{Time: q.After.At.UTC(), Valid: true}
		afterID = sql.NullString{String: q.After.ID, Valid: true}
	}

	const selectSQL = `
WITH attributed AS (
  SELECT p.group_id, MIN(d.created_at) AS since
  FROM positions p
  JOIN deposits d ON d.user_id = p.user_id AND d.group_id = p.group_id AND d.status = 'confirmed'
  WHERE p.user_id = $1 AND p.share_units > 0
  GROUP BY p.group_id
),
items AS (
  SELECT d.id, 'fund'::text AS kind, d.status AS raw_status, d.group_id,
         d.amount AS usdc_micros, NULL::bigint AS token_atomics, ''::text AS mint,
         0::smallint AS token_decimals, d.created_at AS at, NULL::uuid AS transaction_id
  FROM deposits d
  WHERE d.user_id = $1

  UNION ALL
  SELECT w.id, 'cash_out', w.status, w.group_id,
         w.amount, NULL, '', 0::smallint, w.created_at, NULL
  FROM withdrawals w
  WHERE w.user_id = $1

  UNION ALL
  SELECT r.id, 'cash_out', r.status, r.group_id,
         r.slice_usdc, NULL, '', 0::smallint, r.created_at, NULL
  FROM redeem_jobs r
  WHERE r.user_id = $1
    AND r.status IN ('debited', 'selling', 'paying')
    AND r.withdrawal_id IS NULL

  UNION ALL
  SELECT rp.id, 'cash_out', rp.status, rp.group_id,
         rp.amount, NULL, '', 0::smallint, rp.created_at, NULL
  FROM redeem_payouts rp
  WHERE rp.user_id = $1 AND rp.status IN ('failed', 'dropped')

  UNION ALL
  SELECT pw.id, 'withdrawal', pw.status, NULL::uuid,
         pw.amount, NULL, '', 0::smallint, pw.created_at, NULL
  FROM platform_withdrawals pw
  WHERE pw.user_id = $1

  UNION ALL
  SELECT t.id,
         CASE WHEN t.initiated_by = 'agent' THEN 'bot_' || t.action ELSE t.action END,
         t.status, t.group_id,
         CASE
           WHEN t.action = 'buy' THEN COALESCE(t.cost_basis_price, t.amount)
           WHEN t.status = 'confirmed' THEN t.cost_basis_amount
         END,
         CASE
           WHEN t.action = 'sell' THEN t.amount
           WHEN t.status = 'confirmed' THEN t.cost_basis_amount
         END,
         CASE WHEN t.action = 'buy' THEN t.output_mint ELSE t.input_mint END,
         t.token_decimals, t.created_at, t.id
  FROM transactions t
  JOIN attributed a ON a.group_id = t.group_id
  WHERE t.created_at >= a.since
)
SELECT i.id::text, i.kind, i.raw_status, COALESCE(i.group_id::text, ''), COALESCE(g.name, ''),
       i.usdc_micros, i.token_atomics, i.mint, i.token_decimals, i.at,
       COALESCE(i.transaction_id::text, '')
FROM items i
LEFT JOIN groups g ON g.id = i.group_id
WHERE (cardinality($2::text[]) = 0 OR i.kind = ANY($2::text[]))
  AND ($3::timestamptz IS NULL OR (i.at, i.id) < ($3::timestamptz, $4::uuid))
ORDER BY i.at DESC, i.id DESC
LIMIT $5`

	rows, err := s.db.QueryContext(ctx, selectSQL, q.UserID, kinds, afterAt, afterID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("list member history: %w", err)
	}
	defer rows.Close()

	out := make([]MemberHistoryRow, 0, q.Limit)
	for rows.Next() {
		var row MemberHistoryRow
		if err := rows.Scan(
			&row.ID,
			&row.Kind,
			&row.RawStatus,
			&row.GroupID,
			&row.GroupName,
			&row.UsdcMicros,
			&row.TokenAtomics,
			&row.Mint,
			&row.TokenDecimals,
			&row.At,
			&row.TransactionID,
		); err != nil {
			return nil, fmt.Errorf("scan member history: %w", err)
		}
		row.At = row.At.UTC()
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate member history: %w", err)
	}
	return out, nil
}
