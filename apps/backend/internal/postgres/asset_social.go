package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// SymbolProposalRow is one proposal about a single symbol, read across every group
// the viewer belongs to.
//
// The tally and the viewer's own ballot ride on the row rather than being fetched
// per proposal: the asset screen shows one line per vote and would otherwise cost a
// query per line. The group name comes from the same join for the same reason.
type SymbolProposalRow struct {
	ProposalRow
	GroupName string
	Yes       int
	No        int
	// ViewerChoice is "yes", "no", or "" when the viewer has not voted.
	ViewerChoice string
}

// ListProposalsForSymbol returns proposals for one symbol across groupIDs, newest
// first, in a single query.
//
// groupIDs is the caller's own membership list and is the only authorization this
// query performs — it never widens to a group the caller is not in, so a symbol's
// social card can never leak another cabal's vote. An empty list returns nothing
// rather than everything.
func (s *Store) ListProposalsForSymbol(
	ctx context.Context,
	groupIDs []string,
	symbol string,
	viewerID string,
	statuses []domain.ProposalStatus,
	limit int,
) ([]SymbolProposalRow, error) {
	if len(groupIDs) == 0 || strings.TrimSpace(symbol) == "" || len(statuses) == 0 {
		return []SymbolProposalRow{}, nil
	}
	if viewerID == "" {
		return nil, fmt.Errorf("viewer_id is required")
	}
	if limit <= 0 {
		limit = 25
	}

	statusValues := make([]string, 0, len(statuses))
	for _, status := range statuses {
		statusValues = append(statusValues, string(status))
	}

	const selectSQL = `
SELECT p.id, p.group_id, p.proposer_id, p.symbol, p.kind, p.usdc_micros, p.token_amount,
       p.agent_display_name, p.allocation_usdc_micros, p.thesis, p.status, p.expires_at, p.created_at,
       g.name,
       COALESCE(tally.yes, 0), COALESCE(tally.no, 0), COALESCE(mine.choice, '')
FROM proposals p
JOIN groups g ON g.id = p.group_id
LEFT JOIN LATERAL (
  SELECT COUNT(*) FILTER (WHERE v.choice = 'yes') AS yes,
         COUNT(*) FILTER (WHERE v.choice = 'no')  AS no
  FROM votes v
  WHERE v.proposal_id = p.id
) tally ON TRUE
LEFT JOIN votes mine ON mine.proposal_id = p.id AND mine.voter_id = $3
WHERE p.group_id = ANY($1::uuid[])
  AND upper(p.symbol) = upper($2)
  AND p.status = ANY($4::text[])
ORDER BY p.created_at DESC
LIMIT $5`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupIDs, strings.TrimSpace(symbol), viewerID, statusValues, limit)
	if err != nil {
		return nil, fmt.Errorf("list proposals for symbol: %w", err)
	}
	defer rows.Close()

	out := make([]SymbolProposalRow, 0, limit)
	for rows.Next() {
		row, err := scanSymbolProposalRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proposals for symbol: %w", err)
	}
	return out, nil
}

func scanSymbolProposalRow(scanner interface{ Scan(dest ...any) error }) (SymbolProposalRow, error) {
	var row SymbolProposalRow
	var kindRaw, statusRaw string
	var usdc, token, allocation sql.NullInt64
	var agentName, thesis sql.NullString
	if err := scanner.Scan(
		&row.ID,
		&row.GroupID,
		&row.ProposerID,
		&row.Symbol,
		&kindRaw,
		&usdc,
		&token,
		&agentName,
		&allocation,
		&thesis,
		&statusRaw,
		&row.ExpiresAt,
		&row.CreatedAt,
		&row.GroupName,
		&row.Yes,
		&row.No,
		&row.ViewerChoice,
	); err != nil {
		return SymbolProposalRow{}, fmt.Errorf("scan symbol proposal: %w", err)
	}
	kind, err := domain.ParseProposalKind(kindRaw)
	if err != nil {
		return SymbolProposalRow{}, err
	}
	status, err := domain.ParseProposalStatus(statusRaw)
	if err != nil {
		return SymbolProposalRow{}, err
	}
	row.Kind = kind
	row.Status = status
	row.UsdcMicros = usdc.Int64
	row.TokenAmount = token.Int64
	row.AgentDisplayName = agentName.String
	row.AllocationUsdcMicros = allocation.Int64
	row.Thesis = thesis.String
	return row, nil
}

// SymbolVoteRow is one ballot with its voter's identity, so a card can show who
// voted without a profile lookup per proposal.
type SymbolVoteRow struct {
	ProposalID      string
	VoterID         string
	DisplayName     string
	ProfilePhotoURL string
	Choice          domain.VoteChoice
	CastAt          time.Time
}

// ListVotesForProposals returns every ballot on proposalIDs, oldest first, in one
// query joined to the voters' profiles.
//
// The caller passes proposal ids it has already authorized — this does not re-check
// membership, exactly like ListVotesForProposal.
func (s *Store) ListVotesForProposals(ctx context.Context, proposalIDs []string) ([]SymbolVoteRow, error) {
	if len(proposalIDs) == 0 {
		return []SymbolVoteRow{}, nil
	}

	const selectSQL = `
SELECT v.proposal_id, v.voter_id, COALESCE(u.display_name, ''), COALESCE(u.profile_photo_url, ''), v.choice, v.cast_at
FROM votes v
LEFT JOIN users u ON u.id = v.voter_id
WHERE v.proposal_id = ANY($1::uuid[])
ORDER BY v.cast_at`

	rows, err := s.db.QueryContext(ctx, selectSQL, proposalIDs)
	if err != nil {
		return nil, fmt.Errorf("list votes for proposals: %w", err)
	}
	defer rows.Close()

	out := make([]SymbolVoteRow, 0, len(proposalIDs))
	for rows.Next() {
		var row SymbolVoteRow
		var choiceRaw string
		if err := rows.Scan(&row.ProposalID, &row.VoterID, &row.DisplayName, &row.ProfilePhotoURL, &choiceRaw, &row.CastAt); err != nil {
			return nil, fmt.Errorf("scan symbol vote: %w", err)
		}
		choice, err := domain.ParseVoteChoice(choiceRaw)
		if err != nil {
			return nil, err
		}
		row.Choice = choice
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate votes for proposals: %w", err)
	}
	return out, nil
}

// SymbolFillRow is a treasury swap that moved one symbol, with the proposal it came
// from and who kicked it off.
//
// TokenAmount is the proposal's, not the transaction's: a transaction stores its
// fill sizes under cost-basis columns whose meaning differs between a buy and a
// sell, and the asset screen only ever says "the cabal voted to sell 12 shares".
//
// Amount is the swap's raw input amount — USDC micros for a buy, token atomics
// for a sell — so it is not a dollar figure on its own. Call SwapUsdcMicros
// with Action, Status, Amount and CostBasisAmount to get one.
type SymbolFillRow struct {
	ID              string
	GroupID         string
	GroupName       string
	ProposalID      string
	Action          string
	Status          string
	Amount          int64
	CostBasisAmount sql.NullInt64
	TokenAmount     int64
	TxHash          string
	ActorName       string
	CreatedAt       time.Time
}

// ListFillsForSymbol returns confirmed and in-flight swaps for one symbol across
// groupIDs in one query.
//
// Transactions carry token addresses, not tickers, so the symbol is matched through the
// proposal the swap executes. A swap with no proposal behind it (an agent intent
// that never became a vote) is out of scope for the asset screen, which is about
// what the cabal decided.
func (s *Store) ListFillsForSymbol(ctx context.Context, groupIDs []string, symbol string, limit int) ([]SymbolFillRow, error) {
	if len(groupIDs) == 0 || strings.TrimSpace(symbol) == "" {
		return []SymbolFillRow{}, nil
	}
	if limit <= 0 {
		limit = 25
	}

	const selectSQL = `
SELECT t.id, t.group_id, g.name, t.proposal_id, t.action, t.status,
       COALESCE(t.amount, 0), t.cost_basis_amount, COALESCE(p.token_amount, 0),
       COALESCE(t.tx_hash, ''), COALESCE(actor.display_name, ''), t.created_at
FROM transactions t
JOIN proposals p ON p.id = t.proposal_id
JOIN groups g ON g.id = t.group_id
-- initiated_by is text, not a uuid column: it also carries non-user actors, so the
-- join casts rather than assuming every value names a member.
LEFT JOIN users actor ON actor.id::text = NULLIF(t.initiated_by, '')
WHERE t.group_id = ANY($1::uuid[])
  AND upper(p.symbol) = upper($2)
ORDER BY t.created_at DESC
LIMIT $3`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupIDs, strings.TrimSpace(symbol), limit)
	if err != nil {
		return nil, fmt.Errorf("list fills for symbol: %w", err)
	}
	defer rows.Close()

	out := make([]SymbolFillRow, 0, limit)
	for rows.Next() {
		var row SymbolFillRow
		var proposalID sql.NullString
		if err := rows.Scan(
			&row.ID,
			&row.GroupID,
			&row.GroupName,
			&proposalID,
			&row.Action,
			&row.Status,
			&row.Amount,
			&row.CostBasisAmount,
			&row.TokenAmount,
			&row.TxHash,
			&row.ActorName,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan symbol fill: %w", err)
		}
		row.ProposalID = proposalID.String
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fills for symbol: %w", err)
	}
	return out, nil
}
