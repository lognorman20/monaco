package postgres

import (
	"context"
	"fmt"
)

// TryAcquireMemberRedeemLock grabs a session-scoped advisory lock for one member redeem.
// acquired=false when another redeem for the same user and group holds the lock.
func (s *Store) TryAcquireMemberRedeemLock(ctx context.Context, userID, groupID string) (release func(), acquired bool, err error) {
	if userID == "" || groupID == "" {
		return nil, false, fmt.Errorf("user_id and group_id are required")
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("member redeem lock conn: %w", err)
	}

	var locked bool
	err = conn.QueryRowContext(ctx, `
SELECT pg_try_advisory_lock(hashtext($1), hashtext($2))`,
		userID, groupID,
	).Scan(&locked)
	if err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("member redeem lock try: %w", err)
	}
	if !locked {
		_ = conn.Close()
		return nil, false, nil
	}

	release = func() {
		unlockCtx := context.Background()
		_, _ = conn.ExecContext(unlockCtx, `
SELECT pg_advisory_unlock(hashtext($1), hashtext($2))`,
			userID, groupID,
		)
		_ = conn.Close()
	}
	return release, true, nil
}
