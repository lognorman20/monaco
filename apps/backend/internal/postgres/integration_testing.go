package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Fixed advisory lock key so integration tests serialize against shared Docker Postgres.
const integrationTestLockKey int64 = 0x4D6F6E61634F4954

const integrationTruncateSQL = `
TRUNCATE users, member_wallets, groups, treasuries, deposits, positions, withdrawals,
  transactions, group_members, group_voters, proposals, votes, nav_snapshots, payout_proofs,
  redeem_jobs
RESTART IDENTITY CASCADE`

// AcquireIntegrationTestLock waits for the shared integration-test lock and returns a release func.
func AcquireIntegrationTestLock(ctx context.Context, db *sql.DB) (func(), error) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var locked bool
		err := db.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, integrationTestLockKey).Scan(&locked)
		if err != nil {
			return nil, fmt.Errorf("integration test lock: %w", err)
		}
		if locked {
			return func() {
				_, _ = db.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, integrationTestLockKey)
			}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for integration test lock")
}

// ResetIntegrationTables clears integration-test rows, retrying deadlocks from sibling lanes.
func ResetIntegrationTables(ctx context.Context, db *sql.DB) error {
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		_, lastErr = db.ExecContext(ctx, integrationTruncateSQL)
		if lastErr == nil {
			return nil
		}
		if !isDeadlock(lastErr) {
			return fmt.Errorf("reset integration tables: %w", lastErr)
		}
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
	return fmt.Errorf("reset integration tables: %w", lastErr)
}

// PrepareIntegrationDB acquires the shared lock and truncates tables for one integration test.
func PrepareIntegrationDB(t *testing.T, db *sql.DB) {
	t.Helper()

	// Two connections: one may hold an open txn while helpers query the pool.
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	ctx := context.Background()
	release, err := AcquireIntegrationTestLock(ctx, db)
	if err != nil {
		t.Fatalf("AcquireIntegrationTestLock: %v", err)
	}
	t.Cleanup(release)

	if err := ResetIntegrationTables(ctx, db); err != nil {
		t.Fatalf("ResetIntegrationTables: %v", err)
	}
}

func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}
