package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

type failingEnsureTreasuryClient struct {
	inner           wallets.Client
	ensureTreasuryErr error
}

func (c *failingEnsureTreasuryClient) VerifySession(ctx context.Context, token auth.AccessToken) (auth.Identity, error) {
	return c.inner.VerifySession(ctx, token)
}

func (c *failingEnsureTreasuryClient) EnsureMemberWallet(ctx context.Context, privyUserID string, userID wallets.UserID) (wallets.WalletRef, error) {
	return c.inner.EnsureMemberWallet(ctx, privyUserID, userID)
}

func (c *failingEnsureTreasuryClient) EnsureTreasury(ctx context.Context, groupID wallets.GroupID) (wallets.TreasuryRef, error) {
	return wallets.TreasuryRef{}, c.ensureTreasuryErr
}

func (c *failingEnsureTreasuryClient) MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error) {
	return c.inner.MemberUSDCBalance(ctx, memberAddress)
}

func (c *failingEnsureTreasuryClient) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	return c.inner.TreasuryUSDCBalance(ctx, treasuryAddress)
}

func (c *failingEnsureTreasuryClient) SubmitSweep(ctx context.Context, req privy.SweepRequest) (privy.SweepResult, error) {
	return c.inner.SubmitSweep(ctx, req)
}

func (c *failingEnsureTreasuryClient) SubmitMemberUSDCTransfer(ctx context.Context, req privy.TransferRequest) (privy.TransferResult, error) {
	return c.inner.SubmitMemberUSDCTransfer(ctx, req)
}

func (c *failingEnsureTreasuryClient) VerifyPayoutProof(ctx context.Context, userID string, proof privy.PayoutProof) error {
	return c.inner.VerifyPayoutProof(ctx, userID, proof)
}

func (c *failingEnsureTreasuryClient) PayUSDC(ctx context.Context, req privy.PayUSDCRequest) (privy.PayUSDCResult, error) {
	return c.inner.PayUSDC(ctx, req)
}

func TestCreateGroup_privyTreasuryFailure_rollsBackGroupRow(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db, iso := integrationDB(t)
	store := postgres.NewStore(db)

	fake := wallets.NewFakeClient()
	token := auth.AccessToken(iso.UniqueToken("create-group"))
	privyUserID := iso.UniqueDynamicID("create-group")
	auth.RegisterToken(fake, token, auth.Identity{
		PrivyUserID: privyUserID,
		DisplayName: "Alfred",
	})
	user, err := store.UpsertUser(ctx, privyUserID, "Alfred")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

	privyClient := &failingEnsureTreasuryClient{
		inner:             fake,
		ensureTreasuryErr: errors.New("privy unavailable"),
	}
	groups := NewGroupService(store, privyClient)

	// Act
	_, err = groups.CreateGroup(ctx, string(token), testGroupName(iso, "alpha"))

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
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM treasuries t
		INNER JOIN groups g ON g.id = t.group_id
		WHERE g.creator_user_id = $1`, user.ID).Scan(&treasuryCount); err != nil {
		t.Fatalf("count treasuries: %v", err)
	}
	if treasuryCount != 0 {
		t.Fatalf("expected 0 treasuries rows after rollback, got %d", treasuryCount)
	}
}
