package postgres

import (
	"context"
	"fmt"
)

// OpsBacklog counts the money-path rows that are still in flight, for the ops metrics.
// Ages are whole seconds and 0 when there is no such row.
type OpsBacklog struct {
	PendingDeposits            int64
	OldestPendingDepositAgeSec int64
	PendingSwaps               int64
	OldestPendingSwapAgeSec    int64
	// RedeemJobs and OldestRedeemJobAgeSec are keyed by unsettled status; the age counts
	// from updated_at, i.e. how long a job has sat in that status.
	RedeemJobs            map[string]int64
	OldestRedeemJobAgeSec map[string]int64
}

// GetOpsBacklog reads the in-flight counts. Faker users and groups (#153) are excluded for
// the reason ListPendingDeposits excludes them: their rows never move, and would page forever.
func (s *Store) GetOpsBacklog(ctx context.Context) (OpsBacklog, error) {
	backlog := OpsBacklog{
		RedeemJobs:            map[string]int64{},
		OldestRedeemJobAgeSec: map[string]int64{},
	}

	const depositsSQL = `
SELECT COUNT(*), COALESCE(EXTRACT(EPOCH FROM now() - MIN(d.created_at)), 0)::bigint
FROM deposits d
JOIN users u ON u.id = d.user_id
JOIN groups g ON g.id = d.group_id
WHERE d.status = 'pending' AND NOT u.is_faker AND NOT g.is_faker`
	if err := s.db.QueryRowContext(ctx, depositsSQL).Scan(&backlog.PendingDeposits, &backlog.OldestPendingDepositAgeSec); err != nil {
		return OpsBacklog{}, fmt.Errorf("pending deposits backlog: %w", err)
	}

	const swapsSQL = `
SELECT COUNT(*), COALESCE(EXTRACT(EPOCH FROM now() - MIN(t.created_at)), 0)::bigint
FROM transactions t
JOIN groups g ON g.id = t.group_id
WHERE t.status = 'pending' AND NOT g.is_faker`
	if err := s.db.QueryRowContext(ctx, swapsSQL).Scan(&backlog.PendingSwaps, &backlog.OldestPendingSwapAgeSec); err != nil {
		return OpsBacklog{}, fmt.Errorf("pending swaps backlog: %w", err)
	}

	const redeemSQL = `
SELECT r.status, COUNT(*), COALESCE(EXTRACT(EPOCH FROM now() - MIN(r.updated_at)), 0)::bigint
FROM redeem_jobs r
JOIN groups g ON g.id = r.group_id
WHERE r.status IN ('debited', 'selling', 'paying') AND NOT g.is_faker
GROUP BY r.status`
	rows, err := s.db.QueryContext(ctx, redeemSQL)
	if err != nil {
		return OpsBacklog{}, fmt.Errorf("redeem jobs backlog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count, ageSec int64
		if err := rows.Scan(&status, &count, &ageSec); err != nil {
			return OpsBacklog{}, fmt.Errorf("redeem jobs backlog: %w", err)
		}
		backlog.RedeemJobs[status] = count
		backlog.OldestRedeemJobAgeSec[status] = ageSec
	}
	if err := rows.Err(); err != nil {
		return OpsBacklog{}, fmt.Errorf("redeem jobs backlog: %w", err)
	}
	return backlog, nil
}
