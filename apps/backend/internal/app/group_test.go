package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

type failingEnsureTreasuryClient struct {
	inner           privy.Client
	ensureTreasuryErr error
}

func (c *failingEnsureTreasuryClient) VerifySession(ctx context.Context, token privy.AccessToken) (privy.Identity, error) {
	return c.inner.VerifySession(ctx, token)
}

func (c *failingEnsureTreasuryClient) EnsureMemberWallet(ctx context.Context, privyUserID string, userID privy.UserID) (privy.WalletRef, error) {
	return c.inner.EnsureMemberWallet(ctx, privyUserID, userID)
}

func (c *failingEnsureTreasuryClient) EnsureTreasury(ctx context.Context, groupID privy.GroupID) (privy.TreasuryRef, error) {
	return privy.TreasuryRef{}, c.ensureTreasuryErr
}

func TestCreateGroup_privyTreasuryFailure_rollsBackGroupRow(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	resetTables(t, db)
	store := postgres.NewStore(db)

	fake := privy.NewFakeClient()
	token := privy.AccessToken("test-create-group-privy-fail")
	privy.RegisterToken(fake, token, privy.Identity{
		PrivyUserID: "did:privy:create-group-privy-fail",
		DisplayName: "Alfred",
	})
	user, err := store.UpsertUser(ctx, "did:privy:create-group-privy-fail", "Alfred")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	privyClient := &failingEnsureTreasuryClient{
		inner:             fake,
		ensureTreasuryErr: errors.New("privy unavailable"),
	}
	groups := NewGroupService(store, privyClient)

	// Act
	_, err = groups.CreateGroup(ctx, string(token), "Alpha Fund")

	// Assert
	if err == nil {
		t.Fatal("expected CreateGroup to fail when EnsureTreasury fails")
	}

	var groupCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM groups WHERE creator_user_id = $1", user.ID).Scan(&groupCount); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 0 {
		t.Fatalf("expected 0 groups rows after rollback, got %d", groupCount)
	}

	var treasuryCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM treasuries").Scan(&treasuryCount); err != nil {
		t.Fatalf("count treasuries: %v", err)
	}
	if treasuryCount != 0 {
		t.Fatalf("expected 0 treasuries rows after rollback, got %d", treasuryCount)
	}
}
