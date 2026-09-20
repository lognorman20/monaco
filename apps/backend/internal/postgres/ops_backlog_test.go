package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// backlogTestAge is older than anything another test leaves behind, so "oldest" assertions
// hold while other packages' rows share the database.
const backlogTestAge = 10 * 365 * 24 * time.Hour

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func TestGetOpsBacklog_countsInFlightMoneyRowsWithOldestAge(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)

	// Arrange
	user, err := store.UpsertUser(ctx, iso.UniquePrivyID("ops-backlog"), "Ops User")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)
	rules := domain.GroupRules{
		JoinPolicy:        domain.JoinPolicy{Mode: domain.JoinModeOpen},
		VoterSet:          domain.VoterSet{Mode: domain.VoterSetAllMembers},
		Threshold:         domain.ThresholdMajority,
		VoteExpirySeconds: 86_400,
	}
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	group, err := store.InsertGroupWithRulesTx(ctx, tx, "Ops Group "+iso.Suffix(), user.ID, rules)
	if err != nil {
		t.Fatalf("InsertGroupWithRulesTx: %v", err)
	}
	iso.TrackGroup(group.ID)
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	before, err := store.GetOpsBacklog(ctx)
	if err != nil {
		t.Fatalf("GetOpsBacklog before: %v", err)
	}

	old := time.Now().UTC().Add(-backlogTestAge)
	deposit, err := store.InsertDeposit(ctx, user.ID, group.ID, 1_000_000, "member-wallet-"+iso.Suffix())
	if err != nil {
		t.Fatalf("InsertDeposit: %v", err)
	}
	mustExec(t, db, `UPDATE deposits SET created_at = $2 WHERE id = $1`, deposit.ID, old)
	mustExec(t, db, `
INSERT INTO transactions (group_id, amount, action, input_mint, output_mint, status, created_at)
VALUES ($1, 500000, 'buy', 'usdc-mint', 'stock-mint', 'pending', $2)`, group.ID, old)
	mustExec(t, db, `
INSERT INTO redeem_jobs (group_id, user_id, share_units, slice_usdc, payout_address, status, updated_at)
VALUES ($1, $2, 10, 250000, 'payout-wallet', 'paying', $3)`, group.ID, user.ID, old)

	// Act
	got, err := store.GetOpsBacklog(ctx)

	// Assert
	if err != nil {
		t.Fatalf("GetOpsBacklog: %v", err)
	}
	minAge := int64((backlogTestAge - time.Hour).Seconds())
	if got.PendingDeposits != before.PendingDeposits+1 || got.OldestPendingDepositAgeSec < minAge {
		t.Fatalf("deposits = %d (before %d), oldest age = %ds", got.PendingDeposits, before.PendingDeposits, got.OldestPendingDepositAgeSec)
	}
	if got.PendingSwaps != before.PendingSwaps+1 || got.OldestPendingSwapAgeSec < minAge {
		t.Fatalf("swaps = %d (before %d), oldest age = %ds", got.PendingSwaps, before.PendingSwaps, got.OldestPendingSwapAgeSec)
	}
	if got.RedeemJobs["paying"] != before.RedeemJobs["paying"]+1 || got.OldestRedeemJobAgeSec["paying"] < minAge {
		t.Fatalf("paying jobs = %d (before %d), oldest age = %ds", got.RedeemJobs["paying"], before.RedeemJobs["paying"], got.OldestRedeemJobAgeSec["paying"])
	}

	// A settled or credited row is no longer backlog.
	mustExec(t, db, `UPDATE deposits SET status = 'credited' WHERE id = $1`, deposit.ID)
	mustExec(t, db, `UPDATE transactions SET status = 'confirmed' WHERE group_id = $1`, group.ID)
	mustExec(t, db, `UPDATE redeem_jobs SET status = 'settled' WHERE group_id = $1`, group.ID)
	after, err := store.GetOpsBacklog(ctx)
	if err != nil {
		t.Fatalf("GetOpsBacklog after: %v", err)
	}
	if after.PendingDeposits != before.PendingDeposits || after.PendingSwaps != before.PendingSwaps || after.RedeemJobs["paying"] != before.RedeemJobs["paying"] {
		t.Fatalf("resolved rows still counted: %+v (before %+v)", after, before)
	}
}

func TestGetOpsBacklog_fakerRows_neverPage(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)

	// Arrange
	user, err := store.UpsertUser(ctx, iso.UniquePrivyID("ops-backlog-faker"), "Ghost")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)
	rules := domain.GroupRules{
		JoinPolicy:        domain.JoinPolicy{Mode: domain.JoinModeOpen},
		VoterSet:          domain.VoterSet{Mode: domain.VoterSetAllMembers},
		Threshold:         domain.ThresholdMajority,
		VoteExpirySeconds: 86_400,
	}
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	group, err := store.InsertGroupWithRulesTx(ctx, tx, "Ghost Group "+iso.Suffix(), user.ID, rules)
	if err != nil {
		t.Fatalf("InsertGroupWithRulesTx: %v", err)
	}
	iso.TrackGroup(group.ID)
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	mustExec(t, db, `UPDATE groups SET is_faker = true WHERE id = $1`, group.ID)
	before, err := store.GetOpsBacklog(ctx)
	if err != nil {
		t.Fatalf("GetOpsBacklog before: %v", err)
	}
	if _, err := store.InsertDeposit(ctx, user.ID, group.ID, 1_000_000, "ghost-wallet-"+iso.Suffix()); err != nil {
		t.Fatalf("InsertDeposit: %v", err)
	}

	// Act
	got, err := store.GetOpsBacklog(ctx)

	// Assert
	if err != nil {
		t.Fatalf("GetOpsBacklog: %v", err)
	}
	if got.PendingDeposits != before.PendingDeposits {
		t.Fatalf("faker deposit counted as backlog: %d -> %d; it never sweeps, so the alert would never clear", before.PendingDeposits, got.PendingDeposits)
	}
}

func TestGetOpsBacklog_databaseUnavailable_returnsError(t *testing.T) {
	// Arrange
	db := integrationDB(t)
	store := NewStore(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Act
	_, err := store.GetOpsBacklog(ctx)

	// Assert
	if err == nil {
		t.Fatal("GetOpsBacklog succeeded on a cancelled context; the collector would export a fake empty backlog")
	}
}
