package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrCommentParentNotFound means parent_comment_id does not name a comment on the same proposal.
var ErrCommentParentNotFound = errors.New("parent comment not found on proposal")

const (
	foreignKeyViolationSQLState = "23503"
	parentSameProposalFKey      = "proposal_comments_parent_same_proposal_fkey"
)

// ProposalCommentRow is a row in proposal_comments. ParentCommentID is empty for top-level comments.
type ProposalCommentRow struct {
	ID              string
	ProposalID      string
	AuthorID        string
	ParentCommentID string
	Body            string
	CreatedAt       time.Time
}

// InsertProposalComment persists a comment. The composite FK on (parent_comment_id, proposal_id)
// rejects replies to comments on other proposals; that surfaces as ErrCommentParentNotFound.
func (s *Store) InsertProposalComment(ctx context.Context, proposalID, authorID, parentCommentID, body string) (ProposalCommentRow, error) {
	if proposalID == "" || authorID == "" {
		return ProposalCommentRow{}, fmt.Errorf("proposal_id and author_id are required")
	}
	if body == "" {
		return ProposalCommentRow{}, fmt.Errorf("body is required")
	}

	const insertSQL = `
INSERT INTO proposal_comments (proposal_id, author_id, parent_comment_id, body)
VALUES ($1, $2, $3, $4)
RETURNING id, proposal_id, author_id, parent_comment_id, body, created_at`

	var parent sql.NullString
	if parentCommentID != "" {
		parent = sql.NullString{String: parentCommentID, Valid: true}
	}

	var row ProposalCommentRow
	var parentOut sql.NullString
	err := s.db.QueryRowContext(ctx, insertSQL, proposalID, authorID, parent, body).Scan(
		&row.ID,
		&row.ProposalID,
		&row.AuthorID,
		&parentOut,
		&row.Body,
		&row.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolationSQLState && pgErr.ConstraintName == parentSameProposalFKey {
			return ProposalCommentRow{}, ErrCommentParentNotFound
		}
		return ProposalCommentRow{}, fmt.Errorf("insert proposal comment: %w", err)
	}
	row.ParentCommentID = parentOut.String
	return row, nil
}

// ListCommentsByProposal returns up to limit comments for proposalID, oldest first.
func (s *Store) ListCommentsByProposal(ctx context.Context, proposalID string, limit int) ([]ProposalCommentRow, error) {
	if proposalID == "" {
		return nil, fmt.Errorf("proposal_id is required")
	}
	if limit <= 0 {
		limit = 500
	}

	const selectSQL = `
SELECT id, proposal_id, author_id, parent_comment_id, body, created_at
FROM proposal_comments
WHERE proposal_id = $1
ORDER BY created_at ASC, id ASC
LIMIT $2`

	rows, err := s.db.QueryContext(ctx, selectSQL, proposalID, limit)
	if err != nil {
		return nil, fmt.Errorf("list proposal comments: %w", err)
	}
	defer rows.Close()

	out := []ProposalCommentRow{}
	for rows.Next() {
		var row ProposalCommentRow
		var parent sql.NullString
		if err := rows.Scan(&row.ID, &row.ProposalID, &row.AuthorID, &parent, &row.Body, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan proposal comment: %w", err)
		}
		row.ParentCommentID = parent.String
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proposal comments: %w", err)
	}
	return out, nil
}

// CountProposalCommentsByAuthorWithin counts comments authorID wrote during the trailing window,
// measured against the database clock that stamps created_at (write throttle).
func (s *Store) CountProposalCommentsByAuthorWithin(ctx context.Context, authorID string, window time.Duration) (int, error) {
	if authorID == "" {
		return 0, fmt.Errorf("author_id is required")
	}
	if window <= 0 {
		return 0, fmt.Errorf("window must be positive")
	}
	const selectSQL = `
SELECT COUNT(*)
FROM proposal_comments
WHERE author_id = $1 AND created_at >= now() - make_interval(secs => $2)`
	var count int
	if err := s.db.QueryRowContext(ctx, selectSQL, authorID, window.Seconds()).Scan(&count); err != nil {
		return 0, fmt.Errorf("count recent proposal comments: %w", err)
	}
	return count, nil
}
