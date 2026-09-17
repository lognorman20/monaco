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
