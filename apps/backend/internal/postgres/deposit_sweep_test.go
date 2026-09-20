package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// Claim tests are not parallel: a claim takes the next due deposit in the whole database, so
// they must not run beside tests that leave their own pending deposits around.

func seedSweepDeposits(t *testing.T, store *Store, iso *TestIsolation, label string, count int) []DepositRow {
	t.Helper()
	ctx := context.Background()

	user, err := store.UpsertUser(ctx, iso.UniquePrivyID(label), "Sweep "+label)
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
	group, err := store.InsertGroupWithRulesTx(ctx, tx, "Sweep Group "+label+" "+iso.Suffix(), user.ID, rules)
	if err != nil {
		t.Fatalf("InsertGroupWithRulesTx: %v", err)
	}
	iso.TrackGroup(group.ID)
	if err := store.InsertGroupMemberTx(ctx, tx, group.ID, user.ID); err != nil {
		t.Fatalf("InsertGroupMemberTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows := make([]DepositRow, 0, count)
	for i := 0; i < count; i++ {
		row, err := store.InsertDeposit(ctx, user.ID, group.ID, 1_000_000, "member-wallet-"+label)
		if err != nil {
			t.Fatalf("InsertDeposit: %v", err)
		}
		rows = append(rows, row)
	}
	return rows
}

func TestClaimNextSweepDeposit_concurrentOwnersNeverShareADeposit(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	const depositCount = 24
	seeded := seedSweepDeposits(t, store, iso, "claim-race", depositCount)
	now := time.Unix(1_700_000_000, 0).UTC()

	// Act: four pollers drain the queue at once.
	var mu sync.Mutex
	claimedBy := make(map[string][]string)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		owner := fmt.Sprintf("owner-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				row, found, err := store.ClaimNextSweepDeposit(ctx, owner, now, time.Minute, nil)
				if err != nil {
					t.Errorf("ClaimNextSweepDeposit: %v", err)
					return
				}
				if !found {
					return
				}
				mu.Lock()
				claimedBy[row.ID] = append(claimedBy[row.ID], owner)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Assert
	for _, deposit := range seeded {
		if owners := claimedBy[deposit.ID]; len(owners) != 1 {
			t.Fatalf("deposit %s claimed by %v, want exactly one owner", deposit.ID, owners)
		}
	}
}

func TestClaimNextSweepDeposit_leaseExpiryHandsDepositToAnotherOwner(t *testing.T) {
	// Arrange: owner A claims and then dies without releasing.
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	deposit := seedSweepDeposits(t, store, iso, "lease", 1)[0]
	now := time.Unix(1_700_000_000, 0).UTC()
	lease := 2 * time.Minute

	claimed, found, err := store.ClaimNextSweepDeposit(ctx, "owner-a", now, lease, nil)
	if err != nil || !found || claimed.ID != deposit.ID {
		t.Fatalf("owner-a claim: row=%+v found=%v err=%v", claimed, found, err)
	}

	// Act + Assert: inside the lease nobody else gets it.
	if _, found, err := store.ClaimNextSweepDeposit(ctx, "owner-b", now.Add(time.Minute), lease, nil); err != nil || found {
		t.Fatalf("owner-b claim inside lease: found=%v err=%v, want not found", found, err)
	}

	// After the lease it is reclaimable, and the dead owner can no longer record a signature.
	reclaimed, found, err := store.ClaimNextSweepDeposit(ctx, "owner-b", now.Add(lease+time.Second), lease, nil)
	if err != nil || !found || reclaimed.ID != deposit.ID {
		t.Fatalf("owner-b claim after lease: row=%+v found=%v err=%v", reclaimed, found, err)
	}
	height := sql.NullInt64{Int64: 1_000, Valid: true}
	if recorded, err := store.RecordSweepSignature(ctx, deposit.ID, "owner-a", "sig-a-"+iso.Suffix(), height); err != nil || recorded {
		t.Fatalf("stale owner RecordSweepSignature: recorded=%v err=%v, want false", recorded, err)
	}
	if recorded, err := store.RecordSweepSignature(ctx, deposit.ID, "owner-b", "sig-b-"+iso.Suffix(), height); err != nil || !recorded {
		t.Fatalf("owner-b RecordSweepSignature: recorded=%v err=%v, want true", recorded, err)
	}
	// A second signature while one is outstanding would be a second sweep.
	if recorded, err := store.RecordSweepSignature(ctx, deposit.ID, "owner-b", "sig-b2-"+iso.Suffix(), height); err != nil || recorded {
		t.Fatalf("second RecordSweepSignature: recorded=%v err=%v, want false", recorded, err)
	}

	// Releasing with the wrong owner is a no-op; the right owner frees it for the next tick.
	if err := store.ReleaseSweepDeposit(ctx, deposit.ID, "owner-a"); err != nil {
		t.Fatalf("ReleaseSweepDeposit(owner-a): %v", err)
	}
	if _, found, _ := store.ClaimNextSweepDeposit(ctx, "owner-c", now.Add(lease+2*time.Second), lease, nil); found {
		t.Fatal("stale owner's release freed a live claim")
	}
	if err := store.ReleaseSweepDeposit(ctx, deposit.ID, "owner-b"); err != nil {
		t.Fatalf("ReleaseSweepDeposit(owner-b): %v", err)
	}
	if _, found, err := store.ClaimNextSweepDeposit(ctx, "owner-c", now.Add(lease+2*time.Second), lease, nil); err != nil || !found {
		t.Fatalf("claim after release: found=%v err=%v, want found", found, err)
	}
}

func TestClaimNextSweepDeposit_honoursBackoffAndExclusions(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	deposits := seedSweepDeposits(t, store, iso, "backoff", 2)
	backedOff, fresh := deposits[0], deposits[1]
	now := time.Unix(1_700_000_000, 0).UTC()
	if err := store.RecordSweepAttemptFailure(ctx, backedOff.ID, "rpc 503", now.Add(30*time.Second)); err != nil {
		t.Fatalf("RecordSweepAttemptFailure: %v", err)
	}

	// Act + Assert: the older deposit is skipped while it backs off.
	first, found, err := store.ClaimNextSweepDeposit(ctx, "owner", now, time.Minute, nil)
	if err != nil || !found || first.ID != fresh.ID {
		t.Fatalf("first claim = %+v found=%v err=%v, want the fresh deposit", first, found, err)
	}
	if err := store.ReleaseSweepDeposit(ctx, fresh.ID, "owner"); err != nil {
		t.Fatalf("ReleaseSweepDeposit: %v", err)
	}
	if _, found, err := store.ClaimNextSweepDeposit(ctx, "owner", now, time.Minute, []string{fresh.ID}); err != nil || found {
		t.Fatalf("claim with fresh excluded: found=%v err=%v, want nothing due", found, err)
	}

	// Once due, the backed-off deposit is claimable again and carries its attempt count.
	due, found, err := store.ClaimNextSweepDeposit(ctx, "owner", now.Add(31*time.Second), time.Minute, []string{fresh.ID})
	if err != nil || !found || due.ID != backedOff.ID {
		t.Fatalf("claim after backoff = %+v found=%v err=%v, want the backed-off deposit", due, found, err)
	}
	if due.AttemptCount != 1 {
		t.Fatalf("attempt_count = %d, want 1", due.AttemptCount)
	}

	// A healthy pass resets the streak.
	if err := store.DeferSweepDeposit(ctx, backedOff.ID, sql.NullTime{}); err != nil {
		t.Fatalf("DeferSweepDeposit: %v", err)
	}
	var attempts int
	var lastError sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT attempt_count, last_error FROM deposits WHERE id = $1`, backedOff.ID).Scan(&attempts, &lastError); err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if attempts != 0 || lastError.Valid {
		t.Fatalf("after defer attempts=%d last_error=%v, want reset", attempts, lastError)
	}
}

func TestClearDroppedSweepSignature_onlyClearsTheNamedSignature(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	deposit := seedSweepDeposits(t, store, iso, "dropped", 1)[0]
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, found, err := store.ClaimNextSweepDeposit(ctx, "owner", now, time.Minute, nil); err != nil || !found {
		t.Fatalf("claim: found=%v err=%v", found, err)
	}
	sig := "sig-dropped-" + iso.Suffix()
	if recorded, err := store.RecordSweepSignature(ctx, deposit.ID, "owner", sig, sql.NullInt64{Int64: 10, Valid: true}); err != nil || !recorded {
		t.Fatalf("RecordSweepSignature: recorded=%v err=%v", recorded, err)
	}

	// Act + Assert
	if cleared, err := store.ClearDroppedSweepSignature(ctx, deposit.ID, "some-other-sig"); err != nil || cleared {
		t.Fatalf("clear wrong signature: cleared=%v err=%v, want false", cleared, err)
	}
	if cleared, err := store.ClearDroppedSweepSignature(ctx, deposit.ID, sig); err != nil || !cleared {
		t.Fatalf("clear dropped signature: cleared=%v err=%v, want true", cleared, err)
	}
	row, found, err := store.GetDepositByID(ctx, deposit.ID)
	if err != nil || !found {
		t.Fatalf("GetDepositByID: found=%v err=%v", found, err)
	}
	if row.TxSignature.Valid || row.Status != "pending" {
		t.Fatalf("row = %+v, want pending without signature", row)
	}
}

func TestDepositsSchema_statusCheckAndHotPathIndexes(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	deposit := seedSweepDeposits(t, store, iso, "schema", 1)[0]

	// Act + Assert: only statuses the API writes are accepted.
	if _, err := db.ExecContext(ctx, `UPDATE deposits SET status = 'sweeping' WHERE id = $1`, deposit.ID); err == nil {
		t.Fatal("status 'sweeping' accepted; want deposits_status_check violation")
	}
	for _, status := range []string{"failed: submit_sweep", "failed", "confirmed", "pending"} {
		if _, err := db.ExecContext(ctx, `UPDATE deposits SET status = $2 WHERE id = $1`, deposit.ID, status); err != nil {
			t.Fatalf("status %q rejected: %v", status, err)
		}
	}

	for _, index := range []string{
		"deposits_pending_due_idx",
		"deposits_group_id_created_at_idx",
		"deposits_user_id_idx",
		"deposits_user_pending_idx",
		"deposits_group_pending_idx",
	} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE tablename = 'deposits' AND indexname = $1)`, index).Scan(&exists); err != nil {
			t.Fatalf("pg_indexes %s: %v", index, err)
		}
		if !exists {
			t.Fatalf("index %s is missing", index)
		}
	}
}
