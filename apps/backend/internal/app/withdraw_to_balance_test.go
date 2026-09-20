package app

import (
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/packages/domain"
)

func TestWithdrawToBalance_fullStake_paysMemberWallet(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Auth, h.Wallets)
	governance.SetRedeemService(h.Redeem)

	token := auth.AccessToken(h.ISO.UniqueToken("withdraw-full"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Auth, h.Wallets), h.Auth, "withdraw-full", "Withdraw User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "withdraw-full"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}

	const shareUnits = int64(1_000_000)
	const treasuryUSDC = shareUnits
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, treasuryUSDC)

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, shareUnits, shareUnits); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}

	job, err := h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken: string(token),
		GroupID:     group.GroupID,
	})
	if err != nil {
		t.Fatalf("WithdrawToBalance: %v", err)
	}
	if job.Status != domain.RedeemJobSettled {
		t.Fatalf("status = %q, want settled", job.Status)
	}
	if job.Position.ShareUnits != 0 {
		t.Fatalf("share_units = %d, want 0", job.Position.ShareUnits)
	}
	if job.SliceUsdc != treasuryUSDC {
		t.Fatalf("slice_usdc = %d, want %d", job.SliceUsdc, treasuryUSDC)
	}

	payout, ok := wallets.LastPayUSDCRequest(h.Wallets)
	if !ok {
		t.Fatal("expected PayUSDC call")
	}
	if payout.ToAddress != wallet.Address {
		t.Fatalf("payout to = %q, want member wallet %q", payout.ToAddress, wallet.Address)
	}
	if payout.Amount != treasuryUSDC {
		t.Fatalf("payout amount = %d, want %d", payout.Amount, treasuryUSDC)
	}

	balance, err := h.Privy.MemberUSDCBalance(ctx, wallet.Address)
	if err != nil {
		t.Fatalf("MemberUSDCBalance: %v", err)
	}
	if balance != treasuryUSDC {
		t.Fatalf("member balance = %d, want %d", balance, treasuryUSDC)
	}
}

func TestWithdrawToBalance_rejectsOverShare(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Auth, h.Wallets)
	governance.SetRedeemService(h.Redeem)

	token := auth.AccessToken(h.ISO.UniqueToken("withdraw-over"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Auth, h.Wallets), h.Auth, "withdraw-over", "Over User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "withdraw-over"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 1_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, 500_000, 500_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}

	over := int64(750_000)
	_, err = h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken:       string(token),
		GroupID:           group.GroupID,
		ShareAmountMicros: &over,
	})
	if !errors.Is(err, ErrInvalidRedeemRequest) {
		t.Fatalf("WithdrawToBalance err = %v, want ErrInvalidRedeemRequest", err)
	}
}

func TestWithdrawToBalance_halfNAV_twoMemberPot(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Auth, h.Wallets)

	token := auth.AccessToken(h.ISO.UniqueToken("withdraw-half-usdc"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Auth, h.Wallets), h.Auth, "withdraw-half-usdc", "Half USDC User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "withdraw-half-usdc"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	const userShares = int64(500_000)
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 1_000_000)

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, userShares, userShares); err != nil {
		t.Fatalf("IncrementPositionTx user: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit user position: %v", err)
	}

	otherUser, err := h.Store.UpsertUser(ctx, h.ISO.UniqueDynamicID("withdraw-half-usdc-other"), "Other")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	h.ISO.TrackUser(otherUser.ID)
	tx2, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx other: %v", err)
	}
	if err := h.Store.InsertGroupMemberTx(ctx, tx2, group.GroupID, otherUser.ID); err != nil {
		t.Fatalf("InsertGroupMemberTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx2, otherUser.ID, group.GroupID, userShares, userShares); err != nil {
		t.Fatalf("IncrementPositionTx other: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit other position: %v", err)
	}

	job, err := h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken: string(token),
		GroupID:     group.GroupID,
	})
	if err != nil {
		t.Fatalf("WithdrawToBalance: %v", err)
	}
	if job.SliceUsdc != 500_000 {
		t.Fatalf("slice_usdc = %d, want 500000 (~50%% pot)", job.SliceUsdc)
	}
}

func TestWithdrawToBalance_halfNAV_withStockHoldings(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Auth, h.Wallets)
	governance.SetRedeemService(h.Redeem)

	token := auth.AccessToken(h.ISO.UniqueToken("withdraw-half"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Auth, h.Wallets), h.Auth, "withdraw-half", "Half User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "withdraw-half"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	const userShares = int64(500_000)
	const otherShares = int64(500_000)

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, userShares, userShares); err != nil {
		t.Fatalf("IncrementPositionTx user: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit user position: %v", err)
	}

	otherUser, err := h.Store.UpsertUser(ctx, h.ISO.UniqueDynamicID("withdraw-half-other"), "Other")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	h.ISO.TrackUser(otherUser.ID)
	tx2, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx other: %v", err)
	}
	if err := h.Store.InsertGroupMemberTx(ctx, tx2, group.GroupID, otherUser.ID); err != nil {
		t.Fatalf("InsertGroupMemberTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx2, otherUser.ID, group.GroupID, otherShares, otherShares); err != nil {
		t.Fatalf("IncrementPositionTx other: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit other position: %v", err)
	}

	if _, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           500_000,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      testTxHash(h.ISO, "withdraw-half-buy"),
		ExecuteRequestID: testRequestID(h.ISO, "withdraw-half-buy"),
		CostBasisPrice:   500_000,
		CostBasisAmount:  500_000,
	}); err != nil {
		t.Fatalf("confirm buy: %v", err)
	}
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 500_000)

	const sellAmount = int64(250_000)
	sellRequestID := testRequestID(h.ISO, "withdraw-half-sell")
	registerHappySell(t, h.Jupiter, "0xb200000000000000000000c2e324d24d7eecd1fb", sellAmount)
	_ = sellRequestID

	job, err := h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken: string(token),
		GroupID:     group.GroupID,
	})
	if err != nil {
		t.Fatalf("WithdrawToBalance: %v", err)
	}
	if job.Status != domain.RedeemJobSettled {
		t.Fatalf("status = %q, want settled", job.Status)
	}
	if job.SliceUsdc != 500_000 {
		t.Fatalf("slice_usdc = %d, want 500000 (~50%% pot)", job.SliceUsdc)
	}
}

func TestLeaveGroup_withWithdrawStake_zeroSharesThenLeaves(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Auth, h.Wallets)
	governance.SetRedeemService(h.Redeem)

	token := auth.AccessToken(h.ISO.UniqueToken("leave-withdraw"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Auth, h.Wallets), h.Auth, "leave-withdraw", "Leave Withdraw")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "leave-withdraw"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 1_000_000)

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, 1_000_000, 1_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}

	if err := governance.LeaveGroup(ctx, LeaveGroupRequest{
		AccessToken:   string(token),
		GroupID:       group.GroupID,
		WithdrawStake: true,
	}); err != nil {
		t.Fatalf("LeaveGroup: %v", err)
	}

	position, hasPosition, err := h.Store.GetPosition(ctx, session.UserID, group.GroupID)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if !hasPosition {
		t.Fatal("expected position row kept for history")
	}
	if position.ShareUnits != 0 {
		t.Fatalf("share_units = %d, want 0", position.ShareUnits)
	}

	member, err := h.Store.IsGroupMember(ctx, group.GroupID, session.UserID)
	if err != nil {
		t.Fatalf("IsGroupMember: %v", err)
	}
	if member {
		t.Fatal("expected user removed from group_members")
	}
}

func TestWithdrawToBalance_abortsStuckDebitedJob_allowsRetry(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Auth, h.Wallets)
	governance.SetRedeemService(h.Redeem)

	token := auth.AccessToken(h.ISO.UniqueToken("withdraw-stuck"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Auth, h.Wallets), h.Auth, "withdraw-stuck", "Stuck User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "withdraw-stuck"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	const userShares = int64(1_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, userShares, userShares); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}

	if _, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           500_000,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      testTxHash(h.ISO, "withdraw-stuck-buy"),
		ExecuteRequestID: testRequestID(h.ISO, "withdraw-stuck-buy"),
		CostBasisPrice:   500_000,
		CostBasisAmount:  500_000,
	}); err != nil {
		t.Fatalf("confirm buy: %v", err)
	}
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 100_000)

	partial := int64(200_000)
	_, err = h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken:       string(token),
		GroupID:           group.GroupID,
		ShareAmountMicros: &partial,
	})
	if !errors.Is(err, ErrQuoteNotRoutable) {
		t.Fatalf("WithdrawToBalance err = %v, want ErrQuoteNotRoutable", err)
	}

	positionAfterFail, _, err := h.Store.GetPosition(ctx, session.UserID, group.GroupID)
	if err != nil {
		t.Fatalf("GetPosition after fail: %v", err)
	}
	if positionAfterFail.ShareUnits != userShares {
		t.Fatalf("share_units after fail = %d, want %d (shares restored after abort)", positionAfterFail.ShareUnits, userShares)
	}

	active, err := h.Store.HasActiveRedeemJobForUser(ctx, session.UserID, group.GroupID)
	if err != nil {
		t.Fatalf("HasActiveRedeemJobForUser: %v", err)
	}
	if active {
		t.Fatal("expected stuck redeem job cleared after abort-on-failure")
	}

	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 500_000)

	job, err := h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken:       string(token),
		GroupID:           group.GroupID,
		ShareAmountMicros: &partial,
	})
	if err != nil {
		t.Fatalf("WithdrawToBalance retry: %v", err)
	}
	if job.Status != domain.RedeemJobSettled {
		t.Fatalf("retry status = %q, want settled", job.Status)
	}
}

func TestWithdrawToBalance_partialUsdcOnly_skipsStockSell(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Auth, h.Wallets)
	governance.SetRedeemService(h.Redeem)

	token := auth.AccessToken(h.ISO.UniqueToken("withdraw-usdc-only"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Auth, h.Wallets), h.Auth, "withdraw-usdc-only", "USDC Only User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "withdraw-usdc-only"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	const userShares = int64(500_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, userShares, userShares); err != nil {
		t.Fatalf("IncrementPositionTx user: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit user position: %v", err)
	}

	otherUser, err := h.Store.UpsertUser(ctx, h.ISO.UniqueDynamicID("withdraw-usdc-only-other"), "Other")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	h.ISO.TrackUser(otherUser.ID)
	tx2, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx other: %v", err)
	}
	if err := h.Store.InsertGroupMemberTx(ctx, tx2, group.GroupID, otherUser.ID); err != nil {
		t.Fatalf("InsertGroupMemberTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx2, otherUser.ID, group.GroupID, userShares, userShares); err != nil {
		t.Fatalf("IncrementPositionTx other: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit other position: %v", err)
	}

	if _, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           500_000,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      testTxHash(h.ISO, "withdraw-usdc-only-buy"),
		ExecuteRequestID: testRequestID(h.ISO, "withdraw-usdc-only-buy"),
		CostBasisPrice:   500_000,
		CostBasisAmount:  500_000,
	}); err != nil {
		t.Fatalf("confirm buy: %v", err)
	}
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 1_000_000)

	partial := int64(100_000)
	job, err := h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken:       string(token),
		GroupID:           group.GroupID,
		ShareAmountMicros: &partial,
	})
	if err != nil {
		t.Fatalf("WithdrawToBalance: %v", err)
	}
	if job.Status != domain.RedeemJobSettled {
		t.Fatalf("status = %q, want settled", job.Status)
	}
	if job.SliceUsdc < 100_000 {
		t.Fatalf("slice_usdc = %d, want at least dust minimum", job.SliceUsdc)
	}
}

func TestWithdrawToBalance_clearsDebitedJobBeforeNewWithdraw(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Auth, h.Wallets)
	governance.SetRedeemService(h.Redeem)

	token := auth.AccessToken(h.ISO.UniqueToken("withdraw-lock"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Auth, h.Wallets), h.Auth, "withdraw-lock", "Lock User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "withdraw-lock"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, 1_000_000, 1_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}
	lockTx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx lock job: %v", err)
	}
	if _, err := h.Store.InsertRedeemJobTx(ctx, lockTx, session.UserID, group.GroupID, 100_000, 100_000, wallet.Address); err != nil {
		t.Fatalf("InsertRedeemJobTx: %v", err)
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatalf("commit lock job: %v", err)
	}

	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 1_000_000)

	job, err := h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken: string(token),
		GroupID:     group.GroupID,
	})
	if err != nil {
		t.Fatalf("WithdrawToBalance err = %v, want success after clearing debited job", err)
	}
	if job.Status != domain.RedeemJobSettled {
		t.Fatalf("status = %q, want settled", job.Status)
	}
}
