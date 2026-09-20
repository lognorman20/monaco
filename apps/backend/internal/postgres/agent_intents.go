package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// AgentIntentRow is a row in agent_intents.
type AgentIntentRow struct {
	ID            string
	GroupAgentID  string
	GroupID       string
	Side          domain.AgentIntentSide
	Symbol        string
	UsdcMicros    sql.NullInt64
	TokenAmount   sql.NullInt64
	Status        string
	RejectReason  sql.NullString
	TransactionID sql.NullString
	CreatedAt     time.Time
}

const agentIntentSelectColumns = `id, group_agent_id, group_id, side, symbol, usdc_micros, token_amount, status, reject_reason, transaction_id, created_at`

func scanAgentIntentRow(scanner interface{ Scan(dest ...any) error }) (AgentIntentRow, error) {
	var row AgentIntentRow
	var sideRaw string
	if err := scanner.Scan(
		&row.ID,
		&row.GroupAgentID,
		&row.GroupID,
		&sideRaw,
		&row.Symbol,
		&row.UsdcMicros,
		&row.TokenAmount,
		&row.Status,
		&row.RejectReason,
		&row.TransactionID,
		&row.CreatedAt,
	); err != nil {
		return AgentIntentRow{}, err
	}
	side, err := domain.ParseAgentIntentSide(sideRaw)
	if err != nil {
		return AgentIntentRow{}, err
	}
	row.Side = side
	return row, nil
}

// InsertAgentIntent records an agent intent.
func (s *Store) InsertAgentIntent(ctx context.Context, row AgentIntentRow) (AgentIntentRow, error) {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return AgentIntentRow{}, err
	}
	defer func() { _ = tx.Rollback() }()
	out, err := s.InsertAgentIntentTx(ctx, tx, row)
	if err != nil {
		return AgentIntentRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentIntentRow{}, err
	}
	return out, nil
}

// InsertAgentIntentTx records an agent intent within tx.
func (s *Store) InsertAgentIntentTx(ctx context.Context, tx *sql.Tx, row AgentIntentRow) (AgentIntentRow, error) {
	const insertSQL = `
INSERT INTO agent_intents (group_agent_id, group_id, side, symbol, usdc_micros, token_amount, status, reject_reason, transaction_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING ` + agentIntentSelectColumns

	var usdc any
	if row.UsdcMicros.Valid {
		usdc = row.UsdcMicros.Int64
	}
	var token any
	if row.TokenAmount.Valid {
		token = row.TokenAmount.Int64
	}
	var reject any
	if row.RejectReason.Valid {
		reject = row.RejectReason.String
	}
	var txID any
	if row.TransactionID.Valid {
		txID = row.TransactionID.String
	}

	out, err := scanAgentIntentRow(tx.QueryRowContext(ctx, insertSQL,
		row.GroupAgentID,
		row.GroupID,
		string(row.Side),
		row.Symbol,
		usdc,
		token,
		row.Status,
		reject,
		txID,
	))
	if err != nil {
		return AgentIntentRow{}, fmt.Errorf("insert agent intent: %w", err)
	}
	return out, nil
}

// UpdateAgentIntentStatus updates intent status outside an existing tx.
func (s *Store) UpdateAgentIntentStatus(ctx context.Context, intentID, status, rejectReason, transactionID string) error {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.UpdateAgentIntentStatusTx(ctx, tx, intentID, status, rejectReason, transactionID); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateAgentIntentStatusTx updates intent status and optional transaction link.
func (s *Store) UpdateAgentIntentStatusTx(ctx context.Context, tx *sql.Tx, intentID, status, rejectReason, transactionID string) error {
	const updateSQL = `
UPDATE agent_intents
SET status = $2,
    reject_reason = COALESCE($3, reject_reason),
    transaction_id = COALESCE($4, transaction_id)
WHERE id = $1`
	var reject any
	if rejectReason != "" {
		reject = rejectReason
	}
	var txID any
	if transactionID != "" {
		txID = transactionID
	}
	if _, err := tx.ExecContext(ctx, updateSQL, intentID, status, reject, txID); err != nil {
		return fmt.Errorf("update agent intent status: %w", err)
	}
	return nil
}

// GetAgentIntentByID returns one intent row.
func (s *Store) GetAgentIntentByID(ctx context.Context, intentID string) (AgentIntentRow, bool, error) {
	const selectSQL = `SELECT ` + agentIntentSelectColumns + ` FROM agent_intents WHERE id = $1`
	row, err := scanAgentIntentRow(s.db.QueryRowContext(ctx, selectSQL, intentID))
	if errors.Is(err, sql.ErrNoRows) {
		return AgentIntentRow{}, false, nil
	}
	if err != nil {
		return AgentIntentRow{}, false, fmt.Errorf("get agent intent: %w", err)
	}
	return row, true, nil
}
