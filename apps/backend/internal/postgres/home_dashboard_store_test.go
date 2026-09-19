package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

func TestGetNavSnapshotAtOrBefore_returnsLatestEligible(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)

	user, err := store.UpsertUser(ctx, iso.UniquePrivyID("snap-at"), "Snap User")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

	groupID := insertOpenGroup(t, ctx, store, iso, user.ID, "Snap Cabal")
	base := time.Now().UTC().Add(-3 * time.Hour)
	insertNavSnapshot(t, ctx, db, groupID, base.Add(-2*time.Hour), 100_000_000)
	insertNavSnapshot(t, ctx, db, groupID, base.Add(-30*time.Minute), 200_000_000)
	insertNavSnapshot(t, ctx, db, groupID, base.Add(10*time.Minute), 300_000_000)

	got, found, err := store.GetNavSnapshotAtOrBefore(ctx, groupID, base)
	if err != nil {
		t.Fatalf("GetNavSnapshotAtOrBefore: %v", err)
	}
	if !found {
		t.Fatal("expected snapshot")
	}
	if got.PotNavMicros != 200_000_000 {
		t.Fatalf("pot nav = %d, want 200000000", got.PotNavMicros)
	}
}

func TestListMissedOpenProposalsForUser_excludesVotedAndExpired(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)

	user, err := store.UpsertUser(ctx, iso.UniquePrivyID("missed"), "Voter")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

	groupID := insertOpenGroup(t, ctx, store, iso, user.ID, "Vote Cabal")

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	openProposal, err := store.InsertProposalTx(ctx, tx, groupID, user.ID, "AAPL", 1_000_000, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("InsertProposalTx open: %v", err)
	}
	votedProposal, err := store.InsertProposalTx(ctx, tx, groupID, user.ID, "TSLA", 1_000_000, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("InsertProposalTx voted: %v", err)
	}
	_, err = store.InsertProposalTx(ctx, tx, groupID, user.ID, "MSFT", 1_000_000, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("InsertProposalTx expired: %v", err)
	}
	if _, _, err := store.InsertVoteTx(ctx, tx, votedProposal.ID, user.ID, domain.VoteYes); err != nil {
		t.Fatalf("InsertVoteTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	missed, err := store.ListMissedOpenProposalsForUser(ctx, user.ID, []string{groupID}, 20)
	if err != nil {
		t.Fatalf("ListMissedOpenProposalsForUser: %v", err)
	}
	if len(missed) != 1 {
		t.Fatalf("missed len = %d, want 1", len(missed))
	}
	if missed[0].ProposalID != openProposal.ID {
		t.Fatalf("proposal id = %s, want %s", missed[0].ProposalID, openProposal.ID)
	}
	if missed[0].GroupName == "" {
		t.Fatal("expected group name")
	}
}

func insertOpenGroup(t *testing.T, ctx context.Context, store *Store, iso *TestIsolation, userID, name string) string {
	t.Helper()
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
	group, err := store.InsertGroupWithRulesTx(ctx, tx, name+" "+iso.Suffix(), userID, rules)
	if err != nil {
		t.Fatalf("InsertGroupWithRulesTx: %v", err)
	}
	iso.TrackGroup(group.ID)
	if err := store.InsertGroupMemberTx(ctx, tx, group.ID, userID); err != nil {
		t.Fatalf("InsertGroupMemberTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return group.ID
}

func insertNavSnapshot(t *testing.T, ctx context.Context, db *sql.DB, groupID string, createdAt time.Time, potNavMicros int64) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
INSERT INTO nav_snapshots (group_id, pot_nav_micros, nav_per_share_micros, total_shares, reason, created_at)
VALUES ($1, $2, $3, $4, 'deposit', $5)`,
		groupID, potNavMicros, 1_000_000, 1_000_000, createdAt.UTC())
	if err != nil {
		t.Fatalf("insert nav snapshot: %v", err)
	}
}
