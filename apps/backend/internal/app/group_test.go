package app

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

type failingEnsureTreasuryClient struct {
	inner             wallets.Client
	ensureTreasuryErr error
}

func (c *failingEnsureTreasuryClient) EnsureMemberWallet(ctx context.Context, dynamicUserID string, userID wallets.UserID) (wallets.WalletRef, error) {
	return c.inner.EnsureMemberWallet(ctx, dynamicUserID, userID)
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

func (c *failingEnsureTreasuryClient) SubmitSweep(ctx context.Context, req wallets.SweepRequest) (wallets.SweepResult, error) {
	return c.inner.SubmitSweep(ctx, req)
}

func (c *failingEnsureTreasuryClient) SubmitMemberUSDCTransfer(ctx context.Context, req wallets.TransferRequest) (wallets.TransferResult, error) {
	return c.inner.SubmitMemberUSDCTransfer(ctx, req)
}

func (c *failingEnsureTreasuryClient) PayUSDC(ctx context.Context, req wallets.PayUSDCRequest) (wallets.PayUSDCResult, error) {
	return c.inner.PayUSDC(ctx, req)
}

func (c *failingEnsureTreasuryClient) SendTreasuryTransaction(ctx context.Context, treasury wallets.TreasuryRef, to string, data []byte, valueWei *big.Int) (string, error) {
	return c.inner.SendTreasuryTransaction(ctx, treasury, to, data, valueWei)
}

func TestCreateGroup_privyTreasuryFailure_rollsBackGroupRow(t *testing.T) {
	ctx := context.Background()
	db, iso := integrationDB(t)
	store := postgres.NewStore(db)

	fake := wallets.NewFakeClient()
	verifier := auth.NewFakeVerifier()
	token := auth.AccessToken(iso.UniqueToken("create-group"))
	dynamicUserID := iso.UniqueDynamicID("create-group")
	auth.RegisterToken(verifier, token, auth.Identity{
		DynamicUserID: dynamicUserID,
		DisplayName:   "Alfred",
	})
	user, err := store.UpsertUser(ctx, dynamicUserID, "Alfred")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

	walletClient := &failingEnsureTreasuryClient{
		inner:             fake,
		ensureTreasuryErr: errors.New("treasury wallet unavailable"),
	}
	groups := NewGroupService(store, verifier, walletClient)

	_, err = groups.CreateGroup(ctx, string(token), testGroupName(iso, "alpha"))

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
