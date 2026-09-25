package postgres

import (
	"context"
	"fmt"

	"github.com/monaco/monaco/packages/domain"
)

// The reads below answer for a whole set of groups at once.
//
// The asset social card is opened by one member about one stock, but it is about
// every cabal that member belongs to. Asking each cabal its own question means the
// card costs more the more clubs you are in — the members with the most at stake
// wait the longest. Each query here takes the membership list and returns one row
// per group, so the card costs the same whether the member is in one cabal or twenty.
//
// groupIDs is always the caller's own membership list, and is the only authorization
// these queries perform. An empty list returns nothing rather than everything.

// GroupSymbolHolding is one cabal's net position in one mint, with the cost basis
// behind the units it still holds.
type GroupSymbolHolding struct {
	GroupID string
	Mint    string
	// Units is the net token amount in atomic units.
	Units int64
	// CostBasisUsdc is what the cabal paid for those remaining units, in USDC micros.
	// It is zero when no confirmed buy accounts for them.
	CostBasisUsdc int64
	// HasCostBasis is false when the ledger cannot say what the units cost. The card
	// still shows the holding, at no cost basis, rather than hiding it.
	HasCostBasis bool
	// TokenDecimals is the mint's scale as the group's confirmed fills recorded it.
	TokenDecimals int
}

// ListGroupNamesByIDs returns group id to name for every group in groupIDs that exists.
func (s *Store) ListGroupNamesByIDs(ctx context.Context, groupIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(groupIDs))
	if len(groupIDs) == 0 {
		return out, nil
	}

	const selectSQL = `SELECT id, name FROM groups WHERE id = ANY($1::uuid[])`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("list group names by ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("scan group name: %w", err)
		}
		out[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group names by ids: %w", err)
	}
	return out, nil
}

// ListTreasuryAddressesByGroupIDs returns group id to treasury Solana address. A group
// with no treasury row is absent from the map: it has no pot yet.
func (s *Store) ListTreasuryAddressesByGroupIDs(ctx context.Context, groupIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(groupIDs))
	if len(groupIDs) == 0 {
		return out, nil
	}

	const selectSQL = `SELECT group_id, solana_address FROM treasuries WHERE group_id = ANY($1::uuid[])`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("list treasury addresses by group ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var groupID, address string
		if err := rows.Scan(&groupID, &address); err != nil {
			return nil, fmt.Errorf("scan treasury address: %w", err)
		}
		out[groupID] = address
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate treasury addresses by group ids: %w", err)
	}
	return out, nil
}

// ListShareBaseForGroups returns every claim on each group's pot: position share units
// plus units already debited by redeem jobs that have not paid out yet. It is the set
// form of SumShareUnitsByGroup + SumActiveRedeemJobShareUnits, which callers always
// add together, and it answers for a group with no rows at all (zero) rather than
// leaving it out — a missing key and a zero base mean different things to a caller
// dividing by it.
func (s *Store) ListShareBaseForGroups(ctx context.Context, groupIDs []string) (map[string]int64, error) {
	out := make(map[string]int64, len(groupIDs))
	if len(groupIDs) == 0 {
		return out, nil
	}

	const selectSQL = `
SELECT want.id,
       COALESCE(pos.share_units, 0) + COALESCE(jobs.share_units, 0)
FROM unnest($1::uuid[]) AS want(id)
LEFT JOIN (
  SELECT p.group_id, SUM(p.share_units) AS share_units
  FROM positions p
  JOIN users u ON u.id = p.user_id
  JOIN groups g ON g.id = p.group_id
  WHERE p.group_id = ANY($1::uuid[]) AND ` + potPositionPredicate + `
  GROUP BY p.group_id
) pos ON pos.group_id = want.id
LEFT JOIN (
  SELECT group_id, SUM(share_units) AS share_units
  FROM redeem_jobs
  WHERE group_id = ANY($1::uuid[])
    AND status IN ('debited', 'selling', 'paying')
    AND withdrawal_id IS NULL
  GROUP BY group_id
) jobs ON jobs.group_id = want.id`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("list share base for groups: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var groupID string
		var shareBase int64
		if err := rows.Scan(&groupID, &shareBase); err != nil {
			return nil, fmt.Errorf("scan share base: %w", err)
		}
		out[groupID] = shareBase
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate share base for groups: %w", err)
	}
	return out, nil
}

// ListPositionsForUserInGroups returns one user's position in each of groupIDs. A group
// the user holds no position in is absent from the map.
func (s *Store) ListPositionsForUserInGroups(ctx context.Context, userID string, groupIDs []string) (map[string]PositionRow, error) {
	out := make(map[string]PositionRow, len(groupIDs))
	if len(groupIDs) == 0 {
		return out, nil
	}
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}

	const selectSQL = `
SELECT user_id, group_id, share_units, amount_deposited, amount_withdrawn
FROM positions
WHERE user_id = $1 AND group_id = ANY($2::uuid[])`

	rows, err := s.db.QueryContext(ctx, selectSQL, userID, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("list positions for user in groups: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row PositionRow
		if err := rows.Scan(&row.UserID, &row.GroupID, &row.ShareUnits, &row.AmountDeposited, &row.AmountWithdrawn); err != nil {
			return nil, fmt.Errorf("scan position for user in groups: %w", err)
		}
		out[row.GroupID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate positions for user in groups: %w", err)
	}
	return out, nil
}

// ListGroupHoldings returns every group's remaining units per mint, with the
// fill-derived cost basis behind them, in one query.
//
// It replaces, for the whole set of groups, what used to cost one
// ListNetTokenHoldingsByGroup plus one GetFillDerivedCostBasisByOutputMint per
// holding per group. The arithmetic is the same one those two queries did:
// remaining = confirmed buy tokens − confirmed sell tokens, and the cost basis is
// pro-rated to what is left, so a cabal that sold half its position carries half the
// cost it paid.
//
// It returns every mint rather than taking a symbol, because a ticker is not a
// database fact: only the mint catalog can say which mint is "AAPLx", and the caller
// holds that resolver. Filtering here would mean either a second round trip to
// resolve the symbol or a second, divergent idea of what a ticker means.
func (s *Store) ListGroupHoldings(ctx context.Context, groupIDs []string) ([]GroupSymbolHolding, error) {
	if len(groupIDs) == 0 {
		return []GroupSymbolHolding{}, nil
	}

	const selectSQL = `
WITH buys AS (
  SELECT group_id, output_mint AS mint,
         COALESCE(SUM(cost_basis_price), 0)  AS usdc,
         COALESCE(SUM(cost_basis_amount), 0) AS tokens,
         MAX(token_decimals)                 AS token_decimals
  FROM transactions
  WHERE group_id = ANY($1::uuid[]) AND action = 'buy' AND status = 'confirmed'
  GROUP BY group_id, output_mint
),
sells AS (
  SELECT group_id, input_mint AS mint, COALESCE(SUM(amount), 0) AS tokens,
         MAX(token_decimals) AS token_decimals
  FROM transactions
  WHERE group_id = ANY($1::uuid[]) AND action = 'sell' AND status = 'confirmed'
  GROUP BY group_id, input_mint
)
SELECT buys.group_id, buys.mint, buys.usdc, buys.tokens, COALESCE(sells.tokens, 0),
       GREATEST(buys.token_decimals, COALESCE(sells.token_decimals, 0))
FROM buys
LEFT JOIN sells ON sells.group_id = buys.group_id AND sells.mint = buys.mint
WHERE buys.tokens - COALESCE(sells.tokens, 0) > 0
ORDER BY buys.group_id, buys.mint`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("list group holdings: %w", err)
	}
	defer rows.Close()

	out := make([]GroupSymbolHolding, 0, len(groupIDs))
	for rows.Next() {
		var groupID, mint string
		var buyUSDC, buyTokens, sellTokens int64
		var tokenDecimals int
		if err := rows.Scan(&groupID, &mint, &buyUSDC, &buyTokens, &sellTokens, &tokenDecimals); err != nil {
			return nil, fmt.Errorf("scan group holding: %w", err)
		}
		remaining := buyTokens - sellTokens
		if buyTokens <= 0 || remaining <= 0 {
			continue
		}
		holding := GroupSymbolHolding{GroupID: groupID, Mint: mint, Units: remaining, TokenDecimals: tokenDecimals}
		remainingBasis, err := domain.MulDivFloor(buyUSDC, remaining, buyTokens)
		if err != nil {
			return nil, fmt.Errorf("remaining cost basis for group %s mint %s: %w", groupID, mint, err)
		}
		if remainingBasis > 0 {
			// A basis we cannot pro-rate is reported as absent, not as zero dollars
			// paid: the card then shows the holding with no return rather than a
			// fabricated 100% gain.
			holding.CostBasisUsdc = remainingBasis
			holding.HasCostBasis = true
		}
		out = append(out, holding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group holdings: %w", err)
	}
	return out, nil
}
