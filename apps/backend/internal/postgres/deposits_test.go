package postgres

import (
	"context"
	"testing"

	"github.com/monaco/monaco/packages/domain"
)

func TestFailDeposit_marksPendingDepositFailedWithReason(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)

	user, err := store.UpsertUser(ctx, iso.UniquePrivyID("fail-deposit"), "Fail User")
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
	group, err := store.InsertGroupWithRulesTx(ctx, tx, "Fail Group "+iso.Suffix(), user.ID, rules)
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

	row, err := store.InsertDeposit(ctx, user.ID, group.ID, 1_000_000, "member-wallet")
	if err != nil {
		t.Fatalf("InsertDeposit: %v", err)
	}

	failed, ok, err := store.FailDeposit(ctx, row.ID, "submit_sweep")
	if err != nil {
		t.Fatalf("FailDeposit: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if failed.Status != "failed: submit_sweep" {
		t.Fatalf("status = %q, want failed: submit_sweep", failed.Status)
	}

	_, ok, err = store.FailDeposit(ctx, row.ID, "submit_sweep")
	if err != nil {
		t.Fatalf("FailDeposit second call: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for non-pending deposit")
	}

	pending, err := store.ListPendingDeposits(ctx)
	if err != nil {
		t.Fatalf("ListPendingDeposits: %v", err)
	}
	for _, deposit := range pending {
		if deposit.ID == row.ID {
			t.Fatal("failed deposit should not remain pending")
		}
	}
}
