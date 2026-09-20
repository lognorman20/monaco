package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

// settleSellIntoTreasury credits the fake treasury when a fake Jupiter sell confirms. The Privy
// and Jupiter fakes hold separate state, so without this a sell raises no cash and the treasury
// balance stays where the test seeded it.
func settleSellIntoTreasury(t *testing.T, h integrationHarness, requestID, treasuryAddress string, proceeds int64) {
	t.Helper()
	jupiter.RegisterSettlementHook(h.Jupiter, requestID, func(jupiter.ExecuteResult) {
		current, err := h.Privy.TreasuryUSDCBalance(context.Background(), treasuryAddress)
		if err != nil {
			t.Errorf("settlement hook treasury balance: %v", err)
			return
		}
		privy.SetTreasuryUSDCBalance(h.Privy, treasuryAddress, current+proceeds)
	})
}

// registerSellFill wires a routable sell quote, its poll sequence, and the treasury settlement.
func registerSellFill(t *testing.T, h integrationHarness, label, treasuryAddress string, sellAmount, proceeds int64) {
	t.Helper()
	requestID := testRequestID(h.ISO, label)
	registerHappySell(h.Jupiter, jupiter.AAPLxMint, sellAmount, requestID, testTxSignature(h.ISO, label))
	settleSellIntoTreasury(t, h, requestID, treasuryAddress, proceeds)
}

// seedStakeAndHoldings gives userID shareUnits in the group and puts a confirmed buy of
// stockAtomics AAPLx (marked 1:1) in the treasury.
func seedStakeAndHoldings(t *testing.T, h integrationHarness, userID, groupID, label string, shareUnits, stockAtomics int64) {
	t.Helper()
	ctx := context.Background()

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, userID, groupID, shareUnits, shareUnits); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}

	if stockAtomics <= 0 {
		return
	}
	if _, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          groupID,
		Amount:           stockAtomics,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      testTxSignature(h.ISO, label+"-buy"),
		ExecuteRequestID: testRequestID(h.ISO, label+"-buy"),
		CostBasisPrice:   stockAtomics,
		CostBasisAmount:  stockAtomics,
	}); err != nil {
		t.Fatalf("confirm buy: %v", err)
	}
	registerLiveAAPLxMark(h, groupID, jupiter.XStockAtomicScale)
}

// wedgeRedeemJobInPaying reproduces the production wedge: share units already burnt and a job
// parked in `paying` carrying the slice it was quoted.
func wedgeRedeemJobInPaying(t *testing.T, h integrationHarness, userID, groupID, payoutAddress string, shareUnits, sliceUsdc int64) string {
	t.Helper()
	ctx := context.Background()

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.DebitPositionShareUnitsTx(ctx, tx, userID, groupID, shareUnits); err != nil {
		t.Fatalf("DebitPositionShareUnitsTx: %v", err)
	}
	job, err := h.Store.InsertRedeemJobTx(ctx, tx, userID, groupID, shareUnits, sliceUsdc, payoutAddress)
	if err != nil {
		t.Fatalf("InsertRedeemJobTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit wedged job: %v", err)
	}
	if err := h.Store.UpdateRedeemJobStatus(ctx, job.ID, string(domain.RedeemJobPaying)); err != nil {
		t.Fatalf("UpdateRedeemJobStatus: %v", err)
	}
	return job.ID
}

// A pot that spent its cash on stock must sell before paying, and must never broadcast a
// transfer larger than the USDC the sale actually realised: an SPL transfer for more than the
// token account holds fails simulation with Custom:1 and 500s the cash out.
func TestWithdrawToBalance_potHoldsStock_sellsThenPaysNoMoreThanTreasuryUsdc(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Privy)
	governance.SetRedeemService(h.Redeem)

	token := privy.AccessToken(h.ISO.UniqueToken("cashout-stock"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "cashout-stock", "Stock User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "cashout-stock"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	const totalShares = int64(2_000_000)
	const stockAtomics = int64(2_000_000)
	seedStakeAndHoldings(t, h, session.UserID, group.GroupID, "cashout-stock", totalShares, stockAtomics)
	// Every dollar went into the stock: the treasury really holds no USDC.
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 0)

	// The shortfall sale is sized with a slippage buffer; the fill comes back under the mark.
	// The plain pro-rata size is registered too, so the sale always fills and the test is
	// pinned on the payout rather than on how the sale is sized.
	const proceeds = int64(480_000)
	registerSellFill(t, h, "cashout-stock-sell", group.TreasuryAddress, 505_001, proceeds)
	registerSellFill(t, h, "cashout-stock-sell-prorata", group.TreasuryAddress, 500_000, proceeds)

	redeemShares := int64(500_000)
	job, err := h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken:       string(token),
		GroupID:           group.GroupID,
		ShareAmountMicros: &redeemShares,
	})
	if err != nil {
		t.Fatalf("WithdrawToBalance: %v", err)
	}
	if job.Status != domain.RedeemJobSettled {
		t.Fatalf("status = %q, want settled", job.Status)
	}
	if job.SliceUsdc != proceeds {
		t.Fatalf("slice_usdc = %d, want %d (payout clamped to realised USDC)", job.SliceUsdc, proceeds)
	}

	payout, ok := privy.LastPayUSDCRequest(h.Privy)
	if !ok {
		t.Fatal("expected PayUSDC call")
	}
	if payout.Amount != proceeds {
		t.Fatalf("payout amount = %d, want %d", payout.Amount, proceeds)
	}

	treasuryUsdc, err := h.Privy.TreasuryUSDCBalance(ctx, group.TreasuryAddress)
	if err != nil {
		t.Fatalf("TreasuryUSDCBalance: %v", err)
	}
	if treasuryUsdc < 0 {
		t.Fatalf("treasury usdc = %d, want non-negative", treasuryUsdc)
	}

	position, _, err := h.Store.GetPosition(ctx, session.UserID, group.GroupID)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if position.ShareUnits != totalShares-redeemShares {
		t.Fatalf("share_units = %d, want %d", position.ShareUnits, totalShares-redeemShares)
	}
}

// A job wedged in `paying` carries the slice it was quoted against a stale treasury read. On the
// next attempt it must be re-priced against the real pot, raise the cash, and settle.
func TestWithdrawToBalance_recoversJobWedgedInPaying_repricesInflatedSlice(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Privy)
	governance.SetRedeemService(h.Redeem)

	token := privy.AccessToken(h.ISO.UniqueToken("cashout-wedged"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "cashout-wedged", "Wedged User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "cashout-wedged"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	const totalShares = int64(2_000_000)
	seedStakeAndHoldings(t, h, session.UserID, group.GroupID, "cashout-wedged", totalShares, 2_000_000)
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 0)

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}

	// The wedged slice double-counted the USDC the pot had already spent on stock.
	const redeemShares = int64(500_000)
	const inflatedSlice = int64(999_500)
	jobID := wedgeRedeemJobInPaying(t, h, session.UserID, group.GroupID, wallet.SolanaAddress, redeemShares, inflatedSlice)

	const proceeds = int64(495_000)
	registerSellFill(t, h, "cashout-wedged-sell", group.TreasuryAddress, 505_001, proceeds)

	job, err := h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken: string(token),
		GroupID:     group.GroupID,
	})
	if err != nil {
		t.Fatalf("WithdrawToBalance: %v", err)
	}
	if job.ID != jobID {
		t.Fatalf("job id = %q, want the wedged job %q", job.ID, jobID)
	}
	if job.Status != domain.RedeemJobSettled {
		t.Fatalf("status = %q, want settled", job.Status)
	}
	if job.SliceUsdc >= inflatedSlice {
		t.Fatalf("slice_usdc = %d, want it re-priced below the inflated %d", job.SliceUsdc, inflatedSlice)
	}
	if job.SliceUsdc != proceeds {
		t.Fatalf("slice_usdc = %d, want %d", job.SliceUsdc, proceeds)
	}

	payout, ok := privy.LastPayUSDCRequest(h.Privy)
	if !ok {
		t.Fatal("expected PayUSDC call")
	}
	if payout.Amount != proceeds {
		t.Fatalf("payout amount = %d, want %d", payout.Amount, proceeds)
	}

	// The wedge burnt the shares once; recovery must not burn them again.
	position, _, err := h.Store.GetPosition(ctx, session.UserID, group.GroupID)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if position.ShareUnits != totalShares-redeemShares {
		t.Fatalf("share_units = %d, want %d", position.ShareUnits, totalShares-redeemShares)
	}

	stored, found, err := h.Store.GetRedeemJobByID(ctx, jobID)
	if err != nil || !found {
		t.Fatalf("GetRedeemJobByID: found=%v err=%v", found, err)
	}
	if stored.SliceUsdc != proceeds {
		t.Fatalf("persisted slice_usdc = %d, want %d", stored.SliceUsdc, proceeds)
	}
}

// When the pot cannot raise the cash at all, the member must get a clear refusal and their
// shares back rather than a 500 and a job wedged forever.
func TestWithdrawToBalance_potCannotRaiseCash_rollsBackBurntShares(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	governance := NewGovernanceService(h.Store, h.Privy)
	governance.SetRedeemService(h.Redeem)

	token := privy.AccessToken(h.ISO.UniqueToken("cashout-illiquid"))
	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "cashout-illiquid", "Illiquid User")
	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "cashout-illiquid"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	const totalShares = int64(1_000_000)
	seedStakeAndHoldings(t, h, session.UserID, group.GroupID, "cashout-illiquid", totalShares, 0)
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 0)

	wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
	if err != nil || !found {
		t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
	}

	const redeemShares = int64(500_000)
	jobID := wedgeRedeemJobInPaying(t, h, session.UserID, group.GroupID, wallet.SolanaAddress, redeemShares, 500_000)

	_, err = h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
		AccessToken: string(token),
		GroupID:     group.GroupID,
	})
	if !errors.Is(err, ErrRedeemPotIlliquid) {
		t.Fatalf("WithdrawToBalance err = %v, want ErrRedeemPotIlliquid", err)
	}

	if _, ok := privy.LastPayUSDCRequest(h.Privy); ok {
		t.Fatal("expected no payout broadcast when the pot cannot cover it")
	}

	position, _, err := h.Store.GetPosition(ctx, session.UserID, group.GroupID)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if position.ShareUnits != totalShares {
		t.Fatalf("share_units = %d, want %d (burnt shares restored)", position.ShareUnits, totalShares)
	}

	if _, found, err := h.Store.GetRedeemJobByID(ctx, jobID); err != nil {
		t.Fatalf("GetRedeemJobByID: %v", err)
	} else if found {
		t.Fatal("expected the unpayable redeem job to be cleared")
	}

	active, err := h.Store.HasActiveRedeemJobForUser(ctx, session.UserID, group.GroupID)
	if err != nil {
		t.Fatalf("HasActiveRedeemJobForUser: %v", err)
	}
	if active {
		t.Fatal("expected no active redeem job after rollback")
	}
}
