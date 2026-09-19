package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// ProposalRow is a row in proposals.
type ProposalRow struct {
	ID         string
	GroupID    string
	ProposerID string
	Symbol     string
	UsdcMicros int64
	Status     domain.ProposalStatus
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// VoteRow is a row in votes.
type VoteRow struct {
	ProposalID string
	VoterID    string
	Choice     domain.VoteChoice
	CastAt     time.Time
}

// InsertProposalTx persists a new open proposal within tx.
func (s *Store) InsertProposalTx(ctx context.Context, tx *sql.Tx, groupID, proposerID, symbol string, usdcMicros int64, expiresAt time.Time) (ProposalRow, error) {
	if groupID == "" || proposerID == "" {
		return ProposalRow{}, fmt.Errorf("group_id and proposer_id are required")
	}
	if symbol == "" {
		return ProposalRow{}, fmt.Errorf("symbol is required")
	}
	if usdcMicros <= 0 {
		return ProposalRow{}, fmt.Errorf("usdc_micros must be positive")
	}

	const insertSQL = `
INSERT INTO proposals (group_id, proposer_id, symbol, usdc_micros, status, expires_at)
VALUES ($1, $2, $3, $4, 'open', $5)
RETURNING id, group_id, proposer_id, symbol, usdc_micros, status, expires_at, created_at`

	var row ProposalRow
	var status string
	err := tx.QueryRowContext(ctx, insertSQL, groupID, proposerID, symbol, usdcMicros, expiresAt).Scan(
		&row.ID,
		&row.GroupID,
		&row.ProposerID,
		&row.Symbol,
		&row.UsdcMicros,
		&status,
		&row.ExpiresAt,
		&row.CreatedAt,
	)
	if err != nil {
		return ProposalRow{}, fmt.Errorf("insert proposal: %w", err)
	}
	parsedStatus, err := domain.ParseProposalStatus(status)
	if err != nil {
		return ProposalRow{}, err
	}
	row.Status = parsedStatus
	return row, nil
}

// GetProposalByID returns the proposal for id, or false if none exists.
func (s *Store) GetProposalByID(ctx context.Context, proposalID string) (ProposalRow, bool, error) {
	return getProposalByID(ctx, s.db, proposalID)
}

// GetProposalByIDTx returns the proposal for id within tx.
func (s *Store) GetProposalByIDTx(ctx context.Context, tx *sql.Tx, proposalID string) (ProposalRow, bool, error) {
	return getProposalByID(ctx, tx, proposalID)
}

func getProposalByID(ctx context.Context, q queryRower, proposalID string) (ProposalRow, bool, error) {
	if proposalID == "" {
		return ProposalRow{}, false, fmt.Errorf("proposal_id is required")
	}

	const selectSQL = `
SELECT id, group_id, proposer_id, symbol, usdc_micros, status, expires_at, created_at
FROM proposals
WHERE id = $1`

	var row ProposalRow
	var status string
	err := q.QueryRowContext(ctx, selectSQL, proposalID).Scan(
		&row.ID,
		&row.GroupID,
		&row.ProposerID,
		&row.Symbol,
		&row.UsdcMicros,
		&status,
		&row.ExpiresAt,
		&row.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProposalRow{}, false, nil
	}
	if err != nil {
		return ProposalRow{}, false, fmt.Errorf("get proposal by id: %w", err)
	}
	parsedStatus, err := domain.ParseProposalStatus(status)
	if err != nil {
		return ProposalRow{}, false, err
	}
	row.Status = parsedStatus
	return row, true, nil
}

// UpdateProposalStatusTx sets proposals.status within tx when still open.
func (s *Store) UpdateProposalStatusTx(ctx context.Context, tx *sql.Tx, proposalID string, from, to domain.ProposalStatus) (bool, error) {
	const updateSQL = `
UPDATE proposals
SET status = $3
WHERE id = $1 AND status = $2`

	res, err := tx.ExecContext(ctx, updateSQL, proposalID, string(from), string(to))
	if err != nil {
		return false, fmt.Errorf("update proposal status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("update proposal status rows affected: %w", err)
	}
	return n == 1, nil
}

// InsertVoteTx records a ballot within tx. inserted is false when the voter already voted.
func (s *Store) InsertVoteTx(ctx context.Context, tx *sql.Tx, proposalID, voterID string, choice domain.VoteChoice) (VoteRow, bool, error) {
	if proposalID == "" || voterID == "" {
		return VoteRow{}, false, fmt.Errorf("proposal_id and voter_id are required")
	}

	const insertSQL = `
INSERT INTO votes (proposal_id, voter_id, choice)
VALUES ($1, $2, $3)
ON CONFLICT (proposal_id, voter_id) DO NOTHING
RETURNING proposal_id, voter_id, choice, cast_at`

	var row VoteRow
	var choiceRaw string
	err := tx.QueryRowContext(ctx, insertSQL, proposalID, voterID, string(choice)).Scan(
		&row.ProposalID,
		&row.VoterID,
		&choiceRaw,
		&row.CastAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		existing, found, getErr := getVoteByProposalAndVoter(ctx, tx, proposalID, voterID)
		if getErr != nil {
			return VoteRow{}, false, getErr
		}
		if !found {
			return VoteRow{}, false, fmt.Errorf("insert vote conflict without existing row")
		}
		return existing, false, nil
	}
	if err != nil {
		return VoteRow{}, false, fmt.Errorf("insert vote: %w", err)
	}
	parsedChoice, err := domain.ParseVoteChoice(choiceRaw)
	if err != nil {
		return VoteRow{}, false, err
	}
	row.Choice = parsedChoice
	return row, true, nil
}

// GetVoteByProposalAndVoter returns an existing ballot.
func (s *Store) GetVoteByProposalAndVoter(ctx context.Context, proposalID, voterID string) (VoteRow, bool, error) {
	return getVoteByProposalAndVoter(ctx, s.db, proposalID, voterID)
}

// GetVoteByProposalAndVoterTx returns an existing ballot within tx.
func (s *Store) GetVoteByProposalAndVoterTx(ctx context.Context, tx *sql.Tx, proposalID, voterID string) (VoteRow, bool, error) {
	return getVoteByProposalAndVoter(ctx, tx, proposalID, voterID)
}

func getVoteByProposalAndVoter(ctx context.Context, q queryRower, proposalID, voterID string) (VoteRow, bool, error) {
	const selectSQL = `
SELECT proposal_id, voter_id, choice, cast_at
FROM votes
WHERE proposal_id = $1 AND voter_id = $2`

	var row VoteRow
	var choiceRaw string
	err := q.QueryRowContext(ctx, selectSQL, proposalID, voterID).Scan(
		&row.ProposalID,
		&row.VoterID,
		&choiceRaw,
		&row.CastAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return VoteRow{}, false, nil
	}
	if err != nil {
		return VoteRow{}, false, fmt.Errorf("get vote: %w", err)
	}
	parsedChoice, err := domain.ParseVoteChoice(choiceRaw)
	if err != nil {
		return VoteRow{}, false, err
	}
	row.Choice = parsedChoice
	return row, true, nil
}

// ListVotesForProposal returns ballots for proposalID ordered by cast time.
func (s *Store) ListVotesForProposal(ctx context.Context, proposalID string) ([]VoteRow, error) {
	return listVotesForProposal(ctx, s.db, proposalID)
}

// ListVotesForProposalTx returns ballots for proposalID within tx.
func (s *Store) ListVotesForProposalTx(ctx context.Context, tx *sql.Tx, proposalID string) ([]VoteRow, error) {
	return listVotesForProposal(ctx, tx, proposalID)
}

type queryRunner interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func listVotesForProposal(ctx context.Context, q queryRunner, proposalID string) ([]VoteRow, error) {
	if proposalID == "" {
		return nil, fmt.Errorf("proposal_id is required")
	}

	const selectSQL = `
SELECT proposal_id, voter_id, choice, cast_at
FROM votes
WHERE proposal_id = $1
ORDER BY cast_at`

	rows, err := q.QueryContext(ctx, selectSQL, proposalID)
	if err != nil {
		return nil, fmt.Errorf("list votes: %w", err)
	}
	defer rows.Close()

	var out []VoteRow
	for rows.Next() {
		var row VoteRow
		var choiceRaw string
		if err := rows.Scan(&row.ProposalID, &row.VoterID, &choiceRaw, &row.CastAt); err != nil {
			return nil, fmt.Errorf("scan vote: %w", err)
		}
		parsedChoice, err := domain.ParseVoteChoice(choiceRaw)
		if err != nil {
			return nil, err
		}
		row.Choice = parsedChoice
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListProposalsByGroupID returns proposals for groupID filtered by statuses, newest first.
func (s *Store) ListProposalsByGroupID(ctx context.Context, groupID string, statuses []domain.ProposalStatus) ([]ProposalRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}
	if len(statuses) == 0 {
		return []ProposalRow{}, nil
	}

	statusValues := make([]string, 0, len(statuses))
	for _, status := range statuses {
		statusValues = append(statusValues, string(status))
	}

	const selectSQL = `
SELECT id, group_id, proposer_id, symbol, usdc_micros, status, expires_at, created_at
FROM proposals
WHERE group_id = $1 AND status = ANY($2::text[])
ORDER BY created_at DESC
LIMIT 100`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupID, statusValues)
	if err != nil {
		return nil, fmt.Errorf("list proposals by group: %w", err)
	}
	defer rows.Close()

	var out []ProposalRow
	for rows.Next() {
		var row ProposalRow
		var status string
		if err := rows.Scan(
			&row.ID,
			&row.GroupID,
			&row.ProposerID,
			&row.Symbol,
			&row.UsdcMicros,
			&status,
			&row.ExpiresAt,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		parsedStatus, err := domain.ParseProposalStatus(status)
		if err != nil {
			return nil, err
		}
		row.Status = parsedStatus
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListProposalsByGroupIDTx returns proposals for groupID filtered by statuses within tx.
func (s *Store) ListProposalsByGroupIDTx(ctx context.Context, tx *sql.Tx, groupID string, statuses []domain.ProposalStatus) ([]ProposalRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}
	if len(statuses) == 0 {
		return []ProposalRow{}, nil
	}

	statusValues := make([]string, 0, len(statuses))
	for _, status := range statuses {
		statusValues = append(statusValues, string(status))
	}

	const selectSQL = `
SELECT id, group_id, proposer_id, symbol, usdc_micros, status, expires_at, created_at
FROM proposals
WHERE group_id = $1 AND status = ANY($2::text[])
ORDER BY created_at DESC
LIMIT 100`

	rows, err := tx.QueryContext(ctx, selectSQL, groupID, statusValues)
	if err != nil {
		return nil, fmt.Errorf("list proposals by group tx: %w", err)
	}
	defer rows.Close()

	var out []ProposalRow
	for rows.Next() {
		var row ProposalRow
		var status string
		if err := rows.Scan(
			&row.ID,
			&row.GroupID,
			&row.ProposerID,
			&row.Symbol,
			&row.UsdcMicros,
			&status,
			&row.ExpiresAt,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		parsedStatus, err := domain.ParseProposalStatus(status)
		if err != nil {
			return nil, err
		}
		row.Status = parsedStatus
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListPassedProposalsPendingExecute returns passed proposals without a confirmed buy row.
func (s *Store) ListPassedProposalsPendingExecute(ctx context.Context, limit int) ([]ProposalRow, error) {
	if limit <= 0 {
		limit = 20
	}

	const selectSQL = `
SELECT p.id, p.group_id, p.proposer_id, p.symbol, p.usdc_micros, p.status, p.expires_at, p.created_at
FROM proposals p
WHERE p.status = 'passed'
  AND NOT EXISTS (
    SELECT 1
    FROM transactions t
    WHERE t.proposal_id = p.id
      AND t.action = 'buy'
      AND t.status = 'confirmed'
  )
ORDER BY p.created_at ASC
LIMIT $1`

	rows, err := s.db.QueryContext(ctx, selectSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("list passed proposals pending execute: %w", err)
	}
	defer rows.Close()

	var out []ProposalRow
	for rows.Next() {
		var row ProposalRow
		var status string
		if err := rows.Scan(
			&row.ID,
			&row.GroupID,
			&row.ProposerID,
			&row.Symbol,
			&row.UsdcMicros,
			&status,
			&row.ExpiresAt,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan passed proposal pending execute: %w", err)
		}
		parsedStatus, err := domain.ParseProposalStatus(status)
		if err != nil {
			return nil, err
		}
		row.Status = parsedStatus
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListPassedProposalsAwaitingExecuteByGroupID returns passed proposals with no linked swap row yet.
func (s *Store) ListPassedProposalsAwaitingExecuteByGroupID(ctx context.Context, groupID string) ([]ProposalRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

	const selectSQL = `
SELECT p.id, p.group_id, p.proposer_id, p.symbol, p.usdc_micros, p.status, p.expires_at, p.created_at
FROM proposals p
WHERE p.group_id = $1
  AND p.status = 'passed'
  AND NOT EXISTS (
    SELECT 1
    FROM transactions t
    WHERE t.proposal_id = p.id
  )
ORDER BY p.created_at DESC
LIMIT 100`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list passed proposals awaiting execute by group: %w", err)
	}
	defer rows.Close()

	var out []ProposalRow
	for rows.Next() {
		var row ProposalRow
		var status string
		if err := rows.Scan(
			&row.ID,
			&row.GroupID,
			&row.ProposerID,
			&row.Symbol,
			&row.UsdcMicros,
			&status,
			&row.ExpiresAt,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan passed proposal awaiting execute: %w", err)
		}
		parsedStatus, err := domain.ParseProposalStatus(status)
		if err != nil {
			return nil, err
		}
		row.Status = parsedStatus
		out = append(out, row)
	}
	return out, rows.Err()
}

// MissedProposalRow is an open proposal the viewer has not voted on.
type MissedProposalRow struct {
	ProposalID string
	GroupID    string
	GroupName  string
	Symbol     string
	Status     domain.ProposalStatus
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// ListMissedOpenProposalsForUser returns newest open proposals in groupIDs without a vote from userID.
func (s *Store) ListMissedOpenProposalsForUser(ctx context.Context, userID string, groupIDs []string, limit int) ([]MissedProposalRow, error) {
	if userID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if len(groupIDs) == 0 {
		return []MissedProposalRow{}, nil
	}
	if limit <= 0 {
		limit = 20
	}

	const selectSQL = `
SELECT p.id, p.group_id, g.name, p.symbol, p.status, p.created_at, p.expires_at
FROM proposals p
JOIN groups g ON g.id = p.group_id
WHERE p.group_id = ANY($1::uuid[])
  AND p.status = 'open'
  AND p.expires_at > NOW()
  AND NOT EXISTS (
    SELECT 1 FROM votes v WHERE v.proposal_id = p.id AND v.voter_id = $2
  )
ORDER BY p.created_at DESC
LIMIT $3`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupIDs, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list missed open proposals: %w", err)
	}
	defer rows.Close()

	var out []MissedProposalRow
	for rows.Next() {
		var row MissedProposalRow
		var status string
		if err := rows.Scan(
			&row.ProposalID,
			&row.GroupID,
			&row.GroupName,
			&row.Symbol,
			&status,
			&row.CreatedAt,
			&row.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan missed proposal: %w", err)
		}
		parsedStatus, err := domain.ParseProposalStatus(status)
		if err != nil {
			return nil, err
		}
		row.Status = parsedStatus
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missed proposals: %w", err)
	}
	return out, nil
}

// CountTransactionsForProposal returns swap rows linked to proposalID.
func (s *Store) CountTransactionsForProposal(ctx context.Context, proposalID string) (int, error) {
	if proposalID == "" {
		return 0, fmt.Errorf("proposal_id is required")
	}
	const selectSQL = `SELECT COUNT(*) FROM transactions WHERE proposal_id = $1`
	var count int
	if err := s.db.QueryRowContext(ctx, selectSQL, proposalID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count transactions for proposal: %w", err)
	}
	return count, nil
}
