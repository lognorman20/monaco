package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AgentDeploymentStatus is persisted on group_agent_deployments.status.
type AgentDeploymentStatus string

const (
	AgentDeploymentPendingTransfer AgentDeploymentStatus = "pending_transfer"
	AgentDeploymentDeployed        AgentDeploymentStatus = "deployed"
	AgentDeploymentRecalling       AgentDeploymentStatus = "recalling"
	AgentDeploymentClosed          AgentDeploymentStatus = "closed"
)

// AgentDeploymentRow is a row in group_agent_deployments.
type AgentDeploymentRow struct {
	ID                 string
	GroupID            string
	AgentWalletAddress string
	DeployedUsdcMicros int64
	ReturnedUsdcMicros int64
	Status             AgentDeploymentStatus
	DeployProposalID   sql.NullString
	RecallProposalID   sql.NullString
	OutboundTransferID sql.NullString
	InboundTransferID  sql.NullString
	OperatorKeyEnc     sql.NullString
	RecallCommandSent  sql.NullTime
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// OutstandingUsdcMicros is USDC the group still owns outside its treasury.
func (r AgentDeploymentRow) OutstandingUsdcMicros() int64 {
	return r.DeployedUsdcMicros - r.ReturnedUsdcMicros
}

// AgentOutboundTransferRow is a row in group_agent_outbound_transfers.
type AgentOutboundTransferRow struct {
	ID                   string
	GroupID              string
	ToAddress            string
	Amount               int64
	Status               string
	TxSignature          sql.NullString
	LastValidBlockHeight sql.NullInt64
	CreatedAt            time.Time
}

// SolanaTreasuryRow is a row in group_solana_treasuries.
type SolanaTreasuryRow struct {
	GroupID       string
	PrivyWalletID string
	SolanaAddress string
}

const agentDeploymentColumns = `id, group_id, agent_wallet_address, deployed_usdc_micros, returned_usdc_micros,
  status, deploy_proposal_id, recall_proposal_id, outbound_transfer_id, inbound_transfer_id,
  operator_key_enc, recall_command_sent_at, created_at, updated_at`

func scanAgentDeployment(scanner interface{ Scan(dest ...any) error }) (AgentDeploymentRow, error) {
	var row AgentDeploymentRow
	var status string
	if err := scanner.Scan(
		&row.ID,
		&row.GroupID,
		&row.AgentWalletAddress,
		&row.DeployedUsdcMicros,
		&row.ReturnedUsdcMicros,
		&status,
		&row.DeployProposalID,
		&row.RecallProposalID,
		&row.OutboundTransferID,
		&row.InboundTransferID,
		&row.OperatorKeyEnc,
		&row.RecallCommandSent,
		&row.CreatedAt,
		&row.UpdatedAt,
	); err != nil {
		return AgentDeploymentRow{}, err
	}
	row.Status = AgentDeploymentStatus(status)
	return row, nil
}

// GetGroupSolanaTreasury returns the group's Privy Solana treasury wallet.
func (s *Store) GetGroupSolanaTreasury(ctx context.Context, groupID string) (SolanaTreasuryRow, bool, error) {
	var row SolanaTreasuryRow
	err := s.db.QueryRowContext(ctx, `
SELECT group_id, privy_wallet_id, solana_address FROM group_solana_treasuries WHERE group_id = $1`, groupID).
		Scan(&row.GroupID, &row.PrivyWalletID, &row.SolanaAddress)
	if errors.Is(err, sql.ErrNoRows) {
		return SolanaTreasuryRow{}, false, nil
	}
	if err != nil {
		return SolanaTreasuryRow{}, false, fmt.Errorf("get group solana treasury: %w", err)
	}
	return row, true, nil
}

// InsertGroupSolanaTreasury records a provisioned Solana treasury. A concurrent insert for the
// same group keeps the first row, which is returned.
func (s *Store) InsertGroupSolanaTreasury(ctx context.Context, row SolanaTreasuryRow) (SolanaTreasuryRow, error) {
	if row.GroupID == "" || row.PrivyWalletID == "" || row.SolanaAddress == "" {
		return SolanaTreasuryRow{}, fmt.Errorf("group_id, privy_wallet_id and solana_address are required")
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO group_solana_treasuries (group_id, privy_wallet_id, solana_address)
VALUES ($1, $2, $3)
ON CONFLICT (group_id) DO NOTHING`, row.GroupID, row.PrivyWalletID, row.SolanaAddress); err != nil {
		return SolanaTreasuryRow{}, fmt.Errorf("insert group solana treasury: %w", err)
	}
	stored, found, err := s.GetGroupSolanaTreasury(ctx, row.GroupID)
	if err != nil {
		return SolanaTreasuryRow{}, err
	}
	if !found {
		return SolanaTreasuryRow{}, fmt.Errorf("group solana treasury missing after insert")
	}
	return stored, nil
}

// HasActiveAgentDeployment reports a pending_transfer, deployed, or recalling row for the
// group and agent wallet.
func (s *Store) HasActiveAgentDeployment(ctx context.Context, groupID, agentWallet string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM group_agent_deployments
  WHERE group_id = $1 AND agent_wallet_address = $2
    AND status IN ('pending_transfer', 'deployed', 'recalling')
)`, groupID, agentWallet).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("has active agent deployment: %w", err)
	}
	return exists, nil
}

// HasOpenAgentDeploymentProposal reports an open proposal of kind for the group and wallet.
func (s *Store) HasOpenAgentDeploymentProposal(ctx context.Context, groupID, agentWallet, kind string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM proposals
  WHERE group_id = $1 AND agent_wallet_address = $2 AND kind = $3 AND status = 'open'
    AND expires_at > now()
)`, groupID, agentWallet, kind).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("has open agent deployment proposal: %w", err)
	}
	return exists, nil
}

// GetAgentDeploymentByStatus returns the group's deployment for agentWallet in status.
func (s *Store) GetAgentDeploymentByStatus(ctx context.Context, groupID, agentWallet string, status AgentDeploymentStatus) (AgentDeploymentRow, bool, error) {
	return getAgentDeploymentByStatus(ctx, s.db, groupID, agentWallet, status, false)
}

func getAgentDeploymentByStatus(ctx context.Context, q queryRower, groupID, agentWallet string, status AgentDeploymentStatus, forUpdate bool) (AgentDeploymentRow, bool, error) {
	selectSQL := `
SELECT ` + agentDeploymentColumns + `
FROM group_agent_deployments
WHERE group_id = $1 AND agent_wallet_address = $2 AND status = $3`
	if forUpdate {
		selectSQL += ` FOR UPDATE`
	}
	row, err := scanAgentDeployment(q.QueryRowContext(ctx, selectSQL, groupID, agentWallet, string(status)))
	if errors.Is(err, sql.ErrNoRows) {
		return AgentDeploymentRow{}, false, nil
	}
	if err != nil {
		return AgentDeploymentRow{}, false, fmt.Errorf("get agent deployment: %w", err)
	}
	return row, true, nil
}

// GetAgentDeploymentByID returns one deployment.
func (s *Store) GetAgentDeploymentByID(ctx context.Context, id string) (AgentDeploymentRow, bool, error) {
	row, err := scanAgentDeployment(s.db.QueryRowContext(ctx, `
SELECT `+agentDeploymentColumns+` FROM group_agent_deployments WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AgentDeploymentRow{}, false, nil
	}
	if err != nil {
		return AgentDeploymentRow{}, false, fmt.Errorf("get agent deployment by id: %w", err)
	}
	return row, true, nil
}

// ListAgentDeploymentsByStatus returns the oldest deployments in status first.
func (s *Store) ListAgentDeploymentsByStatus(ctx context.Context, status AgentDeploymentStatus, limit int) ([]AgentDeploymentRow, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+agentDeploymentColumns+`
FROM group_agent_deployments
WHERE status = $1
ORDER BY created_at ASC, id ASC
LIMIT $2`, string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("list agent deployments: %w", err)
	}
	defer rows.Close()
	var out []AgentDeploymentRow
	for rows.Next() {
		row, err := scanAgentDeployment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent deployment: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// InsertAgentDeployOperatorKeyTx seals a proposal's cpk_ operator key until the vote passes.
func (s *Store) InsertAgentDeployOperatorKeyTx(ctx context.Context, tx *sql.Tx, proposalID, operatorKeyEnc string) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO group_agent_deploy_operator_keys (proposal_id, operator_key_enc) VALUES ($1, $2)`,
		proposalID, operatorKeyEnc); err != nil {
		return fmt.Errorf("insert agent deploy operator key: %w", err)
	}
	return nil
}

// TakeAgentDeployOperatorKeyTx removes and returns a proposal's sealed operator key.
func (s *Store) TakeAgentDeployOperatorKeyTx(ctx context.Context, tx *sql.Tx, proposalID string) (string, bool, error) {
	var enc string
	err := tx.QueryRowContext(ctx, `
DELETE FROM group_agent_deploy_operator_keys WHERE proposal_id = $1 RETURNING operator_key_enc`, proposalID).Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("take agent deploy operator key: %w", err)
	}
	return enc, true, nil
}

// InsertAgentDeploymentParams is the deploy-pass insert.
type InsertAgentDeploymentParams struct {
	GroupID            string
	AgentWalletAddress string
	DeployedUsdcMicros int64
	DeployProposalID   string
	OperatorKeyEnc     string
}

// InsertAgentDeploymentTx inserts a pending_transfer deployment within the pass transaction.
func (s *Store) InsertAgentDeploymentTx(ctx context.Context, tx *sql.Tx, p InsertAgentDeploymentParams) (AgentDeploymentRow, error) {
	if p.GroupID == "" || p.AgentWalletAddress == "" || p.DeployedUsdcMicros <= 0 || p.DeployProposalID == "" {
		return AgentDeploymentRow{}, fmt.Errorf("group, agent wallet, positive usdc and deploy proposal are required")
	}
	var operatorKey any
	if p.OperatorKeyEnc != "" {
		operatorKey = p.OperatorKeyEnc
	}
	row, err := scanAgentDeployment(tx.QueryRowContext(ctx, `
INSERT INTO group_agent_deployments (
  group_id, agent_wallet_address, deployed_usdc_micros, returned_usdc_micros, status,
  deploy_proposal_id, operator_key_enc
) VALUES ($1, $2, $3, 0, 'pending_transfer', $4, $5)
RETURNING `+agentDeploymentColumns,
		p.GroupID, p.AgentWalletAddress, p.DeployedUsdcMicros, p.DeployProposalID, operatorKey))
	if err != nil {
		return AgentDeploymentRow{}, fmt.Errorf("insert agent deployment: %w", err)
	}
	return row, nil
}

// MarkAgentDeploymentRecallingTx moves the group's deployed row for agentWallet to recalling.
func (s *Store) MarkAgentDeploymentRecallingTx(ctx context.Context, tx *sql.Tx, groupID, agentWallet, recallProposalID string) (bool, error) {
	res, err := tx.ExecContext(ctx, `
UPDATE group_agent_deployments
SET status = 'recalling', recall_proposal_id = $3, updated_at = now()
WHERE group_id = $1 AND agent_wallet_address = $2 AND status = 'deployed'`,
		groupID, agentWallet, recallProposalID)
	if err != nil {
		return false, fmt.Errorf("mark agent deployment recalling: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// MarkAgentRecallCommandSent records that the operated agent was told to send USDC back.
func (s *Store) MarkAgentRecallCommandSent(ctx context.Context, deploymentID string) error {
	if _, err := s.db.ExecContext(ctx, `
UPDATE group_agent_deployments
SET recall_command_sent_at = now(), updated_at = now()
WHERE id = $1 AND status = 'recalling' AND recall_command_sent_at IS NULL`, deploymentID); err != nil {
		return fmt.Errorf("mark agent recall command sent: %w", err)
	}
	return nil
}

// GetAgentOutboundTransfer returns one outbound transfer.
func (s *Store) GetAgentOutboundTransfer(ctx context.Context, id string) (AgentOutboundTransferRow, bool, error) {
	var row AgentOutboundTransferRow
	err := s.db.QueryRowContext(ctx, `
SELECT id, group_id, to_address, amount, status, tx_signature, last_valid_block_height, created_at
FROM group_agent_outbound_transfers WHERE id = $1`, id).Scan(
		&row.ID, &row.GroupID, &row.ToAddress, &row.Amount, &row.Status, &row.TxSignature, &row.LastValidBlockHeight, &row.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentOutboundTransferRow{}, false, nil
	}
	if err != nil {
		return AgentOutboundTransferRow{}, false, fmt.Errorf("get agent outbound transfer: %w", err)
	}
	return row, true, nil
}

// AttachAgentOutboundTransfer records a signed (not yet broadcast) outbound transfer and links
// it to the deployment. It fails when the deployment already has one, so a transfer is never
// signed twice for the same attempt.
func (s *Store) AttachAgentOutboundTransfer(ctx context.Context, deploymentID string, transfer AgentOutboundTransferRow) (AgentOutboundTransferRow, error) {
	if !transfer.TxSignature.Valid || transfer.TxSignature.String == "" {
		return AgentOutboundTransferRow{}, fmt.Errorf("outbound transfer signature is required")
	}
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return AgentOutboundTransferRow{}, err
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, `
INSERT INTO group_agent_outbound_transfers (group_id, to_address, amount, status, tx_signature, last_valid_block_height)
VALUES ($1, $2, $3, 'pending', $4, $5)
RETURNING id, status, created_at`,
		transfer.GroupID, transfer.ToAddress, transfer.Amount, transfer.TxSignature, transfer.LastValidBlockHeight).
		Scan(&transfer.ID, &transfer.Status, &transfer.CreatedAt)
	if err != nil {
		return AgentOutboundTransferRow{}, fmt.Errorf("insert agent outbound transfer: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
UPDATE group_agent_deployments
SET outbound_transfer_id = $2, updated_at = now()
WHERE id = $1 AND status = 'pending_transfer' AND outbound_transfer_id IS NULL`, deploymentID, transfer.ID)
	if err != nil {
		return AgentOutboundTransferRow{}, fmt.Errorf("link agent outbound transfer: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return AgentOutboundTransferRow{}, fmt.Errorf("agent deployment %s is not awaiting a transfer", deploymentID)
	}
	if err := tx.Commit(); err != nil {
		return AgentOutboundTransferRow{}, fmt.Errorf("commit agent outbound transfer: %w", err)
	}
	return transfer, nil
}

// FailAgentOutboundTransfer marks an outbound transfer that can never land as failed and
// unlinks it, so the next tick signs a fresh one. No USDC moved.
func (s *Store) FailAgentOutboundTransfer(ctx context.Context, deploymentID, transferID string) error {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
UPDATE group_agent_outbound_transfers SET status = 'failed' WHERE id = $1 AND status = 'pending'`, transferID); err != nil {
		return fmt.Errorf("fail agent outbound transfer: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE group_agent_deployments
SET outbound_transfer_id = NULL, updated_at = now()
WHERE id = $1 AND status = 'pending_transfer' AND outbound_transfer_id = $2`, deploymentID, transferID); err != nil {
		return fmt.Errorf("unlink agent outbound transfer: %w", err)
	}
	return tx.Commit()
}

// ConfirmAgentDeploymentOutboundTx marks the outbound transfer confirmed and the deployment
// deployed. It reports false when the deployment already left pending_transfer.
func (s *Store) ConfirmAgentDeploymentOutboundTx(ctx context.Context, tx *sql.Tx, deploymentID, transferID string) (bool, error) {
	if _, err := tx.ExecContext(ctx, `
UPDATE group_agent_outbound_transfers SET status = 'confirmed' WHERE id = $1`, transferID); err != nil {
		return false, fmt.Errorf("confirm agent outbound transfer: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
UPDATE group_agent_deployments
SET status = 'deployed', updated_at = now()
WHERE id = $1 AND status = 'pending_transfer' AND outbound_transfer_id = $2`, deploymentID, transferID)
	if err != nil {
		return false, fmt.Errorf("mark agent deployment deployed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// AgentInboundCredit is what one inbound transfer did to a deployment.
type AgentInboundCredit struct {
	// Recorded is false when the signature was already stored; nothing changed.
	Recorded bool
	// CreditedMicros is the part of the transfer counted as returned (capped at outstanding).
	CreditedMicros int64
	Deployment     AgentDeploymentRow
}

// CreditAgentInboundTransferTx records a confirmed inbound SPL USDC transfer and credits it to
// a recalling deployment, capped at the outstanding amount. A signature already stored is a
// no-op. The deployment closes when returned reaches deployed.
func (s *Store) CreditAgentInboundTransferTx(ctx context.Context, tx *sql.Tx, deploymentID, fromAddress, txSignature string, amount int64) (AgentInboundCredit, error) {
	if txSignature == "" || amount <= 0 {
		return AgentInboundCredit{}, fmt.Errorf("inbound transfer signature and positive amount are required")
	}
	deployment, err := scanAgentDeployment(tx.QueryRowContext(ctx, `
SELECT `+agentDeploymentColumns+` FROM group_agent_deployments WHERE id = $1 FOR UPDATE`, deploymentID))
	if err != nil {
		return AgentInboundCredit{}, fmt.Errorf("lock agent deployment: %w", err)
	}
	if deployment.Status != AgentDeploymentRecalling {
		return AgentInboundCredit{Deployment: deployment}, nil
	}
	if fromAddress != deployment.AgentWalletAddress {
		return AgentInboundCredit{Deployment: deployment}, nil
	}

	var inboundID string
	err = tx.QueryRowContext(ctx, `
INSERT INTO group_agent_inbound_transfers (group_id, from_address, amount, status, tx_signature)
VALUES ($1, $2, $3, 'confirmed', $4)
ON CONFLICT (tx_signature) DO NOTHING
RETURNING id`, deployment.GroupID, fromAddress, amount, txSignature).Scan(&inboundID)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentInboundCredit{Deployment: deployment}, nil
	}
	if err != nil {
		return AgentInboundCredit{}, fmt.Errorf("insert agent inbound transfer: %w", err)
	}

	credit := amount
	if outstanding := deployment.OutstandingUsdcMicros(); credit > outstanding {
		credit = outstanding
	}
	updated, err := scanAgentDeployment(tx.QueryRowContext(ctx, `
UPDATE group_agent_deployments
SET returned_usdc_micros = returned_usdc_micros + $2,
    inbound_transfer_id = $3,
    status = CASE WHEN returned_usdc_micros + $2 >= deployed_usdc_micros THEN 'closed' ELSE status END,
    updated_at = now()
WHERE id = $1
RETURNING `+agentDeploymentColumns, deploymentID, credit, inboundID))
	if err != nil {
		return AgentInboundCredit{}, fmt.Errorf("credit agent deployment: %w", err)
	}
	return AgentInboundCredit{Recorded: true, CreditedMicros: credit, Deployment: updated}, nil
}

// SumOutstandingAgentDeploymentsByGroup is deployed minus returned USDC across the group's
// deployed and recalling rows: cash that left the treasury but still belongs to the pot.
func (s *Store) SumOutstandingAgentDeploymentsByGroup(ctx context.Context, groupID string) (int64, error) {
	return sumOutstandingAgentDeploymentsQuery(ctx, s.db, groupID)
}

func sumOutstandingAgentDeploymentsQuery(ctx context.Context, q queryRower, groupID string) (int64, error) {
	var total int64
	err := q.QueryRowContext(ctx, `
SELECT COALESCE(SUM(deployed_usdc_micros - returned_usdc_micros), 0)
FROM group_agent_deployments
WHERE group_id = $1 AND status IN ('deployed', 'recalling')`, groupID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum outstanding agent deployments: %w", err)
	}
	return total, nil
}
