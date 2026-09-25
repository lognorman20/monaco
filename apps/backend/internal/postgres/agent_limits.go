package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// LockGroupAgentTx locks one group_agents row for the rest of tx and returns it. Every intent
// of an agent takes this lock before it reads the agent's budget or position, so two intents
// can never both fit the same remaining allocation. Pause and revoke votes update the same
// row, so the status read here is the one the intent is accepted under.
func (s *Store) LockGroupAgentTx(ctx context.Context, tx *sql.Tx, agentID string) (GroupAgentRow, bool, error) {
	const selectSQL = `
SELECT ` + groupAgentSelectColumns + `
FROM group_agents
WHERE id = $1
FOR UPDATE`
	row, err := scanGroupAgentRow(tx.QueryRowContext(ctx, selectSQL, agentID))
	if errors.Is(err, sql.ErrNoRows) {
		return GroupAgentRow{}, false, nil
	}
	if err != nil {
		return GroupAgentRow{}, false, fmt.Errorf("lock group agent: %w", err)
	}
	return row, true, nil
}

// GetAgentIntentByIdempotencyKeyTx returns the intent an agent already submitted under key.
func (s *Store) GetAgentIntentByIdempotencyKeyTx(ctx context.Context, tx *sql.Tx, agentID, key string) (AgentIntentRow, bool, error) {
	const selectSQL = `
SELECT ` + agentIntentSelectColumns + `
FROM agent_intents
WHERE group_agent_id = $1 AND idempotency_key = $2`
	row, err := scanAgentIntentRow(tx.QueryRowContext(ctx, selectSQL, agentID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return AgentIntentRow{}, false, nil
	}
	if err != nil {
		return AgentIntentRow{}, false, fmt.Errorf("get agent intent by idempotency key: %w", err)
	}
	return row, true, nil
}

// ReconcileAgentIntentsTx brings an agent's intent rows in line with the ledger. The status
// write after a swap can fail (database blip, process killed), which would leave the audit
// trail saying "accepted" after a fill. The transactions row is the source of truth, so:
// an intent whose swap confirmed is executed, an accepted intent whose swap failed is failed,
// and an accepted intent older than abandonedAfter that never reached the ledger is failed
// so it stops reserving budget.
func (s *Store) ReconcileAgentIntentsTx(ctx context.Context, tx *sql.Tx, agentID string, abandonedAfter time.Duration) error {
	const linkSQL = `
UPDATE agent_intents ai
SET transaction_id = t.id,
    status = CASE
      WHEN t.status = 'confirmed' THEN 'executed'
      WHEN t.status = 'failed' AND ai.status = 'accepted' THEN 'failed'
      ELSE ai.status
    END,
    reject_reason = CASE
      WHEN t.status = 'confirmed' THEN NULL
      WHEN t.status = 'failed' AND ai.status = 'accepted' THEN COALESCE(ai.reject_reason, 'swap failed')
      ELSE ai.reject_reason
    END
FROM transactions t
WHERE t.agent_intent_id = ai.id
  AND ai.group_agent_id = $1
  AND ai.status IN ('accepted', 'failed')
  AND (
    ai.transaction_id IS NULL
    OR (t.status = 'confirmed')
    OR (t.status = 'failed' AND ai.status = 'accepted')
  )`
	if _, err := tx.ExecContext(ctx, linkSQL, agentID); err != nil {
		return fmt.Errorf("reconcile agent intents with ledger: %w", err)
	}

	const abandonSQL = `
UPDATE agent_intents ai
SET status = 'failed',
    reject_reason = 'abandoned before execution'
WHERE ai.group_agent_id = $1
  AND ai.status = 'accepted'
  AND ai.created_at < now() - make_interval(secs => $2)
  AND NOT EXISTS (SELECT 1 FROM transactions t WHERE t.agent_intent_id = ai.id)`
	if _, err := tx.ExecContext(ctx, abandonSQL, agentID, abandonedAfter.Seconds()); err != nil {
		return fmt.Errorf("fail abandoned agent intents: %w", err)
	}
	return nil
}

// SumAgentBuyUSDCTx is the USDC an agent's buys hold against its allocation. spent is every
// buy confirmed on the ledger. reserved is every buy not settled yet: accepted and still in
// flight, or pending on the ledger. Reading it under LockGroupAgentTx and inserting the
// accepted intent in the same tx is what reserves budget before the swap is submitted. Sell
// proceeds come back through SumAgentSellProceedsUSDCTx.
func (s *Store) SumAgentBuyUSDCTx(ctx context.Context, tx *sql.Tx, agentID string) (spent, reserved int64, err error) {
	const selectSQL = `
SELECT
  COALESCE(SUM(b.usdc_micros) FILTER (WHERE b.confirmed), 0),
  COALESCE(SUM(b.usdc_micros) FILTER (WHERE NOT b.confirmed AND (b.pending OR b.status = 'accepted')), 0)
FROM (
  SELECT ai.usdc_micros, ai.status,
    EXISTS (
      SELECT 1 FROM transactions t
      WHERE t.agent_intent_id = ai.id AND t.action = 'buy' AND t.initiated_by = 'agent' AND t.status = 'confirmed'
    ) AS confirmed,
    EXISTS (
      SELECT 1 FROM transactions t
      WHERE t.agent_intent_id = ai.id AND t.action = 'buy' AND t.initiated_by = 'agent' AND t.status = 'pending'
    ) AS pending
  FROM agent_intents ai
  WHERE ai.group_agent_id = $1 AND ai.side = 'buy'
) b`
	if err := tx.QueryRowContext(ctx, selectSQL, agentID).Scan(&spent, &reserved); err != nil {
		return 0, 0, fmt.Errorf("sum agent buy usdc: %w", err)
	}
	return spent, reserved, nil
}

// SumAgentSellProceedsUSDCTx is the USDC the agent's confirmed sells returned to the treasury.
// A confirmed sell row stores its proceeds in cost_basis_amount.
func (s *Store) SumAgentSellProceedsUSDCTx(ctx context.Context, tx *sql.Tx, agentID string) (int64, error) {
	const selectSQL = `
SELECT COALESCE(SUM(t.cost_basis_amount), 0)
FROM transactions t
JOIN agent_intents ai ON ai.id = t.agent_intent_id
WHERE ai.group_agent_id = $1
  AND t.initiated_by = 'agent'
  AND t.action = 'sell'
  AND t.status = 'confirmed'`
	var proceeds int64
	if err := tx.QueryRowContext(ctx, selectSQL, agentID).Scan(&proceeds); err != nil {
		return 0, fmt.Errorf("sum agent sell proceeds: %w", err)
	}
	return proceeds, nil
}

// AgentSellableTokenAmountTx is how much of mint the agent may still sell: what its own
// confirmed buys returned, less its own pending or confirmed sells, less its accepted sells
// that have not reached the ledger yet. Positions bought by member vote never count.
func (s *Store) AgentSellableTokenAmountTx(ctx context.Context, tx *sql.Tx, agentID, mint string) (int64, error) {
	const selectSQL = `
SELECT
  COALESCE((
    SELECT SUM(t.cost_basis_amount)
    FROM transactions t
    JOIN agent_intents ai ON ai.id = t.agent_intent_id
    WHERE ai.group_agent_id = $1
      AND t.initiated_by = 'agent'
      AND t.action = 'buy'
      AND t.status = 'confirmed'
      AND t.output_mint = $2
  ), 0)
  - COALESCE((
    SELECT SUM(t.amount)
    FROM transactions t
    JOIN agent_intents ai ON ai.id = t.agent_intent_id
    WHERE ai.group_agent_id = $1
      AND t.initiated_by = 'agent'
      AND t.action = 'sell'
      AND t.status IN ('pending', 'confirmed')
      AND t.input_mint = $2
  ), 0)
  - COALESCE((
    SELECT SUM(ai.token_amount)
    FROM agent_intents ai
    WHERE ai.group_agent_id = $1
      AND ai.side = 'sell'
      AND ai.status = 'accepted'
      AND ai.mint = $2
      AND NOT EXISTS (SELECT 1 FROM transactions t WHERE t.agent_intent_id = ai.id)
  ), 0)`
	var amount int64
	if err := tx.QueryRowContext(ctx, selectSQL, agentID, mint).Scan(&amount); err != nil {
		return 0, fmt.Errorf("agent sellable token amount: %w", err)
	}
	if amount < 0 {
		return 0, nil
	}
	return amount, nil
}

// NetTokenHoldingByGroupAndMintTx is the treasury's ledger holding of one mint: confirmed buy
// output minus confirmed sell input, as ListNetTokenHoldingsByGroup computes per mint.
func (s *Store) NetTokenHoldingByGroupAndMintTx(ctx context.Context, tx *sql.Tx, groupID, mint string) (int64, error) {
	const selectSQL = `
SELECT
  COALESCE((
    SELECT SUM(cost_basis_amount) FROM transactions
    WHERE group_id = $1 AND action = 'buy' AND status = 'confirmed' AND output_mint = $2
  ), 0)
  - COALESCE((
    SELECT SUM(amount) FROM transactions
    WHERE group_id = $1 AND action = 'sell' AND status = 'confirmed' AND input_mint = $2
  ), 0)`
	var amount int64
	if err := tx.QueryRowContext(ctx, selectSQL, groupID, mint).Scan(&amount); err != nil {
		return 0, fmt.Errorf("net token holding by mint: %w", err)
	}
	if amount < 0 {
		return 0, nil
	}
	return amount, nil
}
