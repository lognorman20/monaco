package postgres

import (
	"context"
	"testing"

	"github.com/monaco/monaco/packages/domain"
)

func TestUser_inManyGroups_hasDistinctPositionsPerGroup(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	creator, err := store.UpsertUser(ctx, iso.UniqueDynamicID("creator"), "Alex")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(creator.ID)
	rules := domain.GroupRules{
		JoinPolicy:        domain.JoinPolicy{Mode: domain.JoinModeOpen},
		VoterSet:          domain.VoterSet{Mode: domain.VoterSetAllMembers},
		Threshold:         domain.ThresholdMajority,
		VoteExpirySeconds: 86_400,
	}
	txA, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	groupA, err := store.InsertGroupWithRulesTx(ctx, txA, "Alpha", creator.ID, rules)
	if err != nil {
		t.Fatalf("insert alpha: %v", err)
	}
	iso.TrackGroup(groupA.ID)
	_ = store.InsertGroupMemberTx(ctx, txA, groupA.ID, creator.ID)
	_ = txA.Commit()
	txB, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	groupB, err := store.InsertGroupWithRulesTx(ctx, txB, "Beta", creator.ID, rules)
	if err != nil {
		t.Fatalf("insert beta: %v", err)
	}
	iso.TrackGroup(groupB.ID)
	_ = store.InsertGroupMemberTx(ctx, txB, groupB.ID, creator.ID)
	_ = txB.Commit()
	txPos, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, _ = store.IncrementPositionTx(ctx, txPos, creator.ID, groupA.ID, 1_000_000, 1_000_000)
	_, _ = store.IncrementPositionTx(ctx, txPos, creator.ID, groupB.ID, 2_500_000, 2_500_000)
	_ = txPos.Commit()
	posA, foundA, _ := store.GetPosition(ctx, creator.ID, groupA.ID)
	posB, foundB, _ := store.GetPosition(ctx, creator.ID, groupB.ID)
	groupIDs, _ := store.ListUserGroupIDs(ctx, creator.ID)
	if !foundA || !foundB {
		t.Fatal("expected positions in both groups")
	}
	if posA.GroupID == posB.GroupID || posA.ShareUnits == posB.ShareUnits {
		t.Fatal("expected distinct positions per group")
	}
	if len(groupIDs) != 2 {
		t.Fatalf("expected user in 2 groups, got %d", len(groupIDs))
	}
}
