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

func (c *failingEnsureTreasuryClient) PrepareUSDCPayout(ctx context.Context, req privy.PayUSDCRequest) (privy.PreparedPayout, error) {
	return c.inner.PrepareUSDCPayout(ctx, req)
}

func (c *failingEnsureTreasuryClient) BroadcastUSDCPayout(ctx context.Context, payout privy.PreparedPayout) error {
	return c.inner.BroadcastUSDCPayout(ctx, payout)
}

func (c *failingEnsureTreasuryClient) USDCPayoutStatus(ctx context.Context, payout privy.PreparedPayout) (privy.PayoutStatus, error) {
	return c.inner.USDCPayoutStatus(ctx, payout)
}

func (c *failingEnsureTreasuryClient) TokenBalanceDelta(ctx context.Context, signature, owner, mint string) (int64, error) {
	return c.inner.TokenBalanceDelta(ctx, signature, owner, mint)
}

func TestCreateGroup_privyTreasuryFailure_rollsBackGroupRow(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db, iso := integrationDB(t)
	store := postgres.NewStore(db)

	fake := privy.NewFakeClient()
	token := privy.AccessToken(iso.UniqueToken("create-group"))
	privyUserID := iso.UniquePrivyID("create-group")
	privy.RegisterToken(fake, token, privy.Identity{
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
