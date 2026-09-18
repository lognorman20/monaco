package postgres

import (
	"context"
	"fmt"
)

// ProposalFeedStats is per-proposal vote and comment aggregates for feed cards.
type ProposalFeedStats struct {
	YesCount     int
	NoCount      int
	CommentCount int
	ViewerVoted  bool
}

// ListProposalFeedStats returns vote counts, comment counts, and whether viewerID already voted,
// keyed by proposal id. One round trip for the whole page so feed cards avoid N+1 queries.
func (s *Store) ListProposalFeedStats(ctx context.Context, proposalIDs []string, viewerID string) (map[string]ProposalFeedStats, error) {
	out := make(map[string]ProposalFeedStats, len(proposalIDs))
	if len(proposalIDs) == 0 {
		return out, nil
	}
	if viewerID == "" {
		return nil, fmt.Errorf("viewer_id is required")
	}

	const selectSQL = `
SELECT
  p.id,
  (SELECT COUNT(*) FROM votes v WHERE v.proposal_id = p.id AND v.choice = 'yes'),
  (SELECT COUNT(*) FROM votes v WHERE v.proposal_id = p.id AND v.choice = 'no'),
  (SELECT COUNT(*) FROM proposal_comments c WHERE c.proposal_id = p.id),
  EXISTS (SELECT 1 FROM votes v WHERE v.proposal_id = p.id AND v.voter_id = $2)
FROM proposals p
WHERE p.id = ANY($1::uuid[])`

	rows, err := s.db.QueryContext(ctx, selectSQL, proposalIDs, viewerID)
	if err != nil {
		return nil, fmt.Errorf("list proposal feed stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var stats ProposalFeedStats
		if err := rows.Scan(&id, &stats.YesCount, &stats.NoCount, &stats.CommentCount, &stats.ViewerVoted); err != nil {
			return nil, fmt.Errorf("scan proposal feed stats: %w", err)
		}
		out[id] = stats
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proposal feed stats: %w", err)
	}
	return out, nil
}
