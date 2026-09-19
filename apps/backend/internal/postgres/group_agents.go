package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// GroupAgentRow is a row in group_agents.
type GroupAgentRow struct {
	ID                   string
	GroupID              string
	Status               domain.AgentStatus
	InstallProposalID    sql.NullString
	PauseProposalID      sql.NullString
	ResumeProposalID     sql.NullString
	RevokeProposalID     sql.NullString
	AgentDisplayName     string
	AllocationUsdcMicros int64
	APIKeyHash           sql.NullString
	APIKeyPrefix         sql.NullString
	APIKey               sql.NullString
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

const groupAgentSelectColumns = `id, group_id, status, install_proposal_id, pause_proposal_id, resume_proposal_id,
  revoke_proposal_id, agent_display_name, allocation_usdc_micros, api_key_hash, api_key_prefix, api_key, created_at, updated_at`

func scanGroupAgentRow(scanner interface{ Scan(dest ...any) error }) (GroupAgentRow, error) {
	var row GroupAgentRow
	var statusRaw string
	if err := scanner.Scan(
		&row.ID,
		&row.GroupID,
		&statusRaw,
		&row.InstallProposalID,
		&row.PauseProposalID,
		&row.ResumeProposalID,
		&row.RevokeProposalID,
		&row.AgentDisplayName,
		&row.AllocationUsdcMicros,
		&row.APIKeyHash,
		&row.APIKeyPrefix,
		&row.APIKey,
		&row.CreatedAt,
		&row.UpdatedAt,
	); err != nil {
		return GroupAgentRow{}, err
	}
	status, err := domain.ParseAgentStatus(statusRaw)
	if err != nil {
		return GroupAgentRow{}, err
	}
	row.Status = status
	return row, nil
}

// InsertGroupAgentTx creates a group agent row within tx.
func (s *Store) InsertGroupAgentTx(ctx context.Context, tx *sql.Tx, row GroupAgentRow) (GroupAgentRow, error) {
	const insertSQL = `
INSERT INTO group_agents (
  group_id, status, install_proposal_id, agent_display_name, allocation_usdc_micros,
  api_key_hash, api_key_prefix, api_key
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING ` + groupAgentSelectColumns

	var installID any
	if row.InstallProposalID.Valid {
		installID = row.InstallProposalID.String
	}
	var keyHash any
	if row.APIKeyHash.Valid {
		keyHash = row.APIKeyHash.String
	}
	var keyPrefix any
	if row.APIKeyPrefix.Valid {
		keyPrefix = row.APIKeyPrefix.String
	}
	var apiKey any
	if row.APIKey.Valid {
		apiKey = row.APIKey.String
	}

	out, err := scanGroupAgentRow(tx.QueryRowContext(ctx, insertSQL,
		row.GroupID,
		string(row.Status),
		installID,
		row.AgentDisplayName,
		row.AllocationUsdcMicros,
		keyHash,
		keyPrefix,
		apiKey,
	))
	if err != nil {
		return GroupAgentRow{}, fmt.Errorf("insert group agent: %w", err)
	}
	return out, nil
}

// GetActiveOrPausedGroupAgentByGroupID returns the live agent for a group, if any.
func (s *Store) GetActiveOrPausedGroupAgentByGroupID(ctx context.Context, groupID string) (GroupAgentRow, bool, error) {
	const selectSQL = `
SELECT ` + groupAgentSelectColumns + `
FROM group_agents
WHERE group_id = $1 AND status IN ('active', 'paused')
ORDER BY created_at DESC
LIMIT 1`
	row, err := scanGroupAgentRow(s.db.QueryRowContext(ctx, selectSQL, groupID))
	if errors.Is(err, sql.ErrNoRows) {
		return GroupAgentRow{}, false, nil
	}
	if err != nil {
		return GroupAgentRow{}, false, fmt.Errorf("get active group agent: %w", err)
	}
	return row, true, nil
}

// GetGroupAgentByAPIKeyHash returns the agent for a hashed API key.
func (s *Store) GetGroupAgentByAPIKeyHash(ctx context.Context, keyHash string) (GroupAgentRow, bool, error) {
	const selectSQL = `
SELECT ` + groupAgentSelectColumns + `
FROM group_agents
WHERE api_key_hash = $1 AND status <> 'revoked'`
	row, err := scanGroupAgentRow(s.db.QueryRowContext(ctx, selectSQL, keyHash))
	if errors.Is(err, sql.ErrNoRows) {
		return GroupAgentRow{}, false, nil
	}
	if err != nil {
		return GroupAgentRow{}, false, fmt.Errorf("get group agent by key hash: %w", err)
	}
	return row, true, nil
}

// UpdateGroupAgentStatusTx updates agent status and optional proposal link columns.
func (s *Store) UpdateGroupAgentStatusTx(ctx context.Context, tx *sql.Tx, agentID string, status domain.AgentStatus, pauseProposalID, resumeProposalID, revokeProposalID string) error {
	const updateSQL = `
UPDATE group_agents
SET status = $2,
    pause_proposal_id = COALESCE($3, pause_proposal_id),
    resume_proposal_id = COALESCE($4, resume_proposal_id),
    revoke_proposal_id = COALESCE($5, revoke_proposal_id),
    updated_at = now()
WHERE id = $1`
	var pauseID any
	if pauseProposalID != "" {
		pauseID = pauseProposalID
	}
	var resumeID any
	if resumeProposalID != "" {
		resumeID = resumeProposalID
	}
	var revokeID any
	if revokeProposalID != "" {
		revokeID = revokeProposalID
	}
	if _, err := tx.ExecContext(ctx, updateSQL, agentID, string(status), pauseID, resumeID, revokeID); err != nil {
		return fmt.Errorf("update group agent status: %w", err)
	}
	return nil
}

// RevokeGroupAgentKeyTx clears api_key_hash when an agent is revoked.
func (s *Store) RevokeGroupAgentKeyTx(ctx context.Context, tx *sql.Tx, agentID string) error {
	const updateSQL = `
UPDATE group_agents
SET api_key_hash = NULL, api_key_prefix = NULL, api_key = NULL, status = 'revoked', updated_at = now()
WHERE id = $1`
	if _, err := tx.ExecContext(ctx, updateSQL, agentID); err != nil {
		return fmt.Errorf("revoke group agent key: %w", err)
	}
	return nil
}

// InsertAgentKeyRevealTx stores a one-time plaintext key for the proposer.
func (s *Store) InsertAgentKeyRevealTx(ctx context.Context, tx *sql.Tx, proposalID, plaintextKey string) error {
	const insertSQL = `
INSERT INTO group_agent_key_reveals (proposal_id, plaintext_key)
VALUES ($1, $2)
ON CONFLICT (proposal_id) DO NOTHING`
	if _, err := tx.ExecContext(ctx, insertSQL, proposalID, plaintextKey); err != nil {
		return fmt.Errorf("insert agent key reveal: %w", err)
	}
	return nil
}

// ConsumeAgentKeyReveal returns and deletes the one-time key for proposalID.
func (s *Store) ConsumeAgentKeyReveal(ctx context.Context, proposalID string) (string, bool, error) {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback() }()

	const selectSQL = `
DELETE FROM group_agent_key_reveals
WHERE proposal_id = $1
RETURNING plaintext_key`
	var key string
	err = tx.QueryRowContext(ctx, selectSQL, proposalID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("consume agent key reveal: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", false, fmt.Errorf("commit consume agent key reveal: %w", err)
	}
	return key, true, nil
}

// SumAgentExecutedBuyUSDCByAgentID sums confirmed agent-initiated buy USDC for allocation tracking.
func (s *Store) SumAgentExecutedBuyUSDCByAgentID(ctx context.Context, agentID string) (int64, error) {
	const selectSQL = `
SELECT COALESCE(SUM(t.amount), 0)
FROM transactions t
WHERE t.agent_intent_id IN (SELECT id FROM agent_intents WHERE group_agent_id = $1)
  AND t.action = 'buy'
  AND t.status = 'confirmed'
  AND t.initiated_by = 'agent'`
	var sum int64
	if err := s.db.QueryRowContext(ctx, selectSQL, agentID).Scan(&sum); err != nil {
		return 0, fmt.Errorf("sum agent executed buy usdc: %w", err)
	}
	return sum, nil
}

// SumPendingAgentBuyUSDCByAgentID sums pending agent buy USDC reserved against allocation.
func (s *Store) SumPendingAgentBuyUSDCByAgentID(ctx context.Context, agentID string) (int64, error) {
	const selectSQL = `
SELECT COALESCE(SUM(t.amount), 0)
FROM transactions t
JOIN agent_intents ai ON ai.id = t.agent_intent_id
WHERE ai.group_agent_id = $1
  AND t.action = 'buy'
  AND t.status = 'pending'
  AND t.initiated_by = 'agent'`
	var sum int64
	if err := s.db.QueryRowContext(ctx, selectSQL, agentID).Scan(&sum); err != nil {
		return 0, fmt.Errorf("sum pending agent buy usdc: %w", err)
	}
	return sum, nil
}
