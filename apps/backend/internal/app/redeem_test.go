package app

import (
	"context"
	"sync"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

type redeemSeed struct {
	GroupID        string
	UserID         string
	Token          string
	TreasuryAddr   string
	PayoutAddress  string
	Proof          privy.PayoutProof
	InitialShares  int64
	InitialDeposit int64
}

func seedRedeemGroup(t *testing.T, h integrationHarness, treasuryUsdc int64) redeemSeed {
	t.Helper()
	ctx := context.Background()

	token := privy.AccessToken("redeem-test-" + t.Name())
	privy.RegisterToken(h.Privy, token, privy.Identity{
		PrivyUserID: "did:privy:redeem-" + t.Name(),
		DisplayName: "Redeem Tester",
	})

	sessions := NewSessionService(h.Store, h.Privy)
	if _, err := sessions.OpenSession(ctx, string(token)); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	user, _, err := h.Store.GetUserByPrivyUserID(ctx, "did:privy:redeem-"+t.Name())
	if err != nil {
		t.Fatalf("GetUserByPrivyUserID: %v", err)
	}

	group, err := h.Groups.CreateGroup(ctx, string(token), "Redeem Group")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	treasury, err := h.Privy.EnsureTreasury(ctx, privy.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	privy.SetTreasuryUSDCBalance(h.Privy, treasury.SolanaAddress, treasuryUsdc)

	const depositAmount = int64(4_000_000)
	deposit, err := h.Deposits.CreateDeposit(ctx, string(token), group.GroupID, depositAmount)
	if err != nil {
		t.Fatalf("CreateDeposit: %v", err)
	}
	sweep := ObservedSweep{
		TxSignature: "SWEEP-redeem-" + t.Name(),
		FromAddress: deposit.Deposit.FromAddress,
		ToAddress:   treasury.SolanaAddress,
		Amount:      depositAmount,
		DepositID:   deposit.Deposit.ID,
		UserID:      user.ID,
		GroupID:     group.GroupID,
	}
	if _, err := h.Deposits.ObserveSweep(ctx, sweep); err != nil {
		t.Fatalf("ObserveSweep: %v", err)
	}

	payoutAddress := "PAYOUT" + user.ID[:8]
	proof := privy.BuildValidPayoutProof(user.ID, payoutAddress)

	return redeemSeed{
		GroupID:        group.GroupID,
		UserID:         user.ID,
		Token:          string(token),
		TreasuryAddr:   treasury.SolanaAddress,
		PayoutAddress:  payoutAddress,
		Proof:          proof,
		InitialShares:  depositAmount,
		InitialDeposit: depositAmount,
	}
}

func TestRedeem_partialByShareAmount_debitsUnitsFirst(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	seed := seedRedeemGroup(t, h, 4_000_000)
	shareDebit := int64(1_000_000)

	// Act
	result, err := h.Redeem.Redeem(context.Background(), RedeemRequest{
		AccessToken:       seed.Token,
		GroupID:           seed.GroupID,
		ShareAmountMicros: &shareDebit,
		PayoutProof:       seed.Proof,
	})

	// Assert
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if result.Status != domain.RedeemJobSettled {
		t.Fatalf("status = %q, want settled", result.Status)
	}
	if result.Position.ShareUnits != seed.InitialShares-shareDebit {
		t.Fatalf("shareUnits = %d, want %d", result.Position.ShareUnits, seed.InitialShares-shareDebit)
	}
	if result.Position.AmountDeposited != seed.InitialDeposit {
		t.Fatalf("amountDeposited changed to %d, want unchanged %d", result.Position.AmountDeposited, seed.InitialDeposit)
	}
}

func TestRedeemJob_crashAfterDebit_resumesWithoutSecondDebit(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	seed := seedRedeemGroup(t, h, 4_000_000)
	shareDebit := int64(1_000_000)
	ctx := context.Background()

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.DebitPositionShareUnitsTx(ctx, tx, seed.UserID, seed.GroupID, shareDebit); err != nil {
		t.Fatalf("DebitPositionShareUnitsTx: %v", err)
	}
	job, err := h.Store.InsertRedeemJobTx(ctx, tx, seed.UserID, seed.GroupID, shareDebit, 1_000_000, seed.PayoutAddress)
	if err != nil {
		t.Fatalf("InsertRedeemJobTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit debit: %v", err)
	}

	posBeforeResume, _, err := h.Store.GetPosition(ctx, seed.UserID, seed.GroupID)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}

	// Act
	resumed, err := h.Redeem.Redeem(ctx, RedeemRequest{ResumeJobID: job.ID, PayoutProof: seed.Proof})

	// Assert
	if err != nil {
		t.Fatalf("resume Redeem: %v", err)
	}
	if resumed.Status != domain.RedeemJobSettled {
		t.Fatalf("status = %q, want settled", resumed.Status)
	}
	if resumed.Position.ShareUnits != posBeforeResume.ShareUnits {
		t.Fatalf("resume changed share units from %d to %d", posBeforeResume.ShareUnits, resumed.Position.ShareUnits)
	}
}

func TestRedeem_withXStockInTreasury_sellsSliceBeforePayout(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	seed := seedRedeemGroup(t, h, 3_000_000)
	const outputMint = jupiter.AAPLxMint
	const requestID = "req-redeem-sell"
	registerHappyBuy(h.Jupiter, h.XStocks, outputMint, 1_000_000, requestID, "buy-before-redeem")
	if _, err := h.Swap.DevExecuteBuy(context.Background(), DevExecuteBuyRequest{
		GroupID:    seed.GroupID,
		UserID:     seed.UserID,
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	}); err != nil {
		t.Fatalf("DevExecuteBuy: %v", err)
	}

	jupiter.RegisterSellQuote(h.Jupiter, outputMint, 125_000, jupiter.SellQuote{
		Routable:    true,
		InputMint:   outputMint,
		OutputMint:  jupiter.USDCMint,
		InAmount:    "125000",
		OutAmount:   "120000",
		RequestID:   "req-redeem-sell-out",
		Transaction: "unsigned-sell-tx",
	})
	jupiter.RegisterExecutePoll(h.Jupiter, "req-redeem-sell-out", []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusPending, Code: -1},
		{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          "sell-redeem-slice",
			OutputAmountResult: "120000",
		},
	})

	shareDebit := int64(1_000_000)

	// Act
	result, err := h.Redeem.Redeem(context.Background(), RedeemRequest{
		AccessToken:       seed.Token,
		GroupID:           seed.GroupID,
		ShareAmountMicros: &shareDebit,
		PayoutProof:       seed.Proof,
	})

	// Assert
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if result.Status != domain.RedeemJobSettled {
		t.Fatalf("status = %q, want settled", result.Status)
	}
	count, err := h.Store.CountConfirmedTransactionsBySignature(context.Background(), "sell-redeem-slice")
	if err != nil {
		t.Fatalf("CountConfirmedTransactionsBySignature: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected sell transaction, got count %d", count)
	}
}

func TestRedeem_validProof_paysUsdcOnlyToProvenAddress(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	seed := seedRedeemGroup(t, h, 4_000_000)
	shareDebit := int64(2_000_000)

	// Act
	result, err := h.Redeem.Redeem(context.Background(), RedeemRequest{
		AccessToken:       seed.Token,
		GroupID:           seed.GroupID,
		ShareAmountMicros: &shareDebit,
		PayoutProof:       seed.Proof,
	})

	// Assert
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	payout, ok := privy.LastPayUSDCRequest(h.Privy)
	if !ok {
		t.Fatal("expected PayUSDC call")
	}
	if payout.ToAddress != seed.PayoutAddress {
		t.Fatalf("paid to %q, want %q", payout.ToAddress, seed.PayoutAddress)
	}
	if payout.Amount != result.SliceUsdc {
		t.Fatalf("paid %d, want slice %d", payout.Amount, result.SliceUsdc)
	}
}

func TestRedeem_afterPayout_incrementsAmountWithdrawnAndWritesNavSnapshot(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	seed := seedRedeemGroup(t, h, 4_000_000)
	shareDebit := int64(2_000_000)

	// Act
	result, err := h.Redeem.Redeem(context.Background(), RedeemRequest{
		AccessToken:       seed.Token,
		GroupID:           seed.GroupID,
		ShareAmountMicros: &shareDebit,
		PayoutProof:       seed.Proof,
	})

	// Assert
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if result.Position.AmountWithdrawn != result.SliceUsdc {
		t.Fatalf("amountWithdrawn = %d, want %d", result.Position.AmountWithdrawn, result.SliceUsdc)
	}
	count, err := h.Store.CountNavSnapshotsByGroupAndReason(context.Background(), seed.GroupID, postgres.NavSnapshotReasonWithdrawalPayout)
	if err != nil {
		t.Fatalf("CountNavSnapshotsByGroupAndReason: %v", err)
	}
	if count != 1 {
		t.Fatalf("withdrawal snapshot count = %d, want 1", count)
	}

	positions, err := h.Store.ListPositionsByGroup(context.Background(), seed.GroupID)
	if err != nil {
		t.Fatalf("ListPositionsByGroup: %v", err)
	}
	appPositions := make([]Position, 0, len(positions))
	for _, row := range positions {
		appPositions = append(appPositions, positionFromRowPostgres(row))
	}
	board, err := BuildMemberBoardAfterWithdrawal(appPositions, result.Position.ShareUnits, 4_000_000-result.SliceUsdc)
	if err != nil {
		t.Fatalf("BuildMemberBoardAfterWithdrawal: %v", err)
	}
	if len(board) == 0 {
		t.Fatal("expected board row after withdrawal")
	}
}

func TestRedeem_invalidPayoutProof_rejected(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	seed := seedRedeemGroup(t, h, 4_000_000)
	shareDebit := int64(1_000_000)
	badProof := seed.Proof
	badProof.Signature = "invalid-proof"

	// Act
	_, err := h.Redeem.Redeem(context.Background(), RedeemRequest{
		AccessToken:       seed.Token,
		GroupID:           seed.GroupID,
		ShareAmountMicros: &shareDebit,
		PayoutProof:       badProof,
	})

	// Assert
	if err == nil {
		t.Fatal("expected invalid payout proof error")
	}
	if err != ErrInvalidPayoutProof {
		t.Fatalf("err = %v, want ErrInvalidPayoutProof", err)
	}
}

func TestRedeem_payoutEqualsRedeemedSliceNotDepositRefund(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	seed := seedRedeemGroup(t, h, 4_500_000)
	shareDebit := int64(1_000_000)
	depositRefund := shareDebit

	// Act
	result, err := h.Redeem.Redeem(context.Background(), RedeemRequest{
		AccessToken:       seed.Token,
		GroupID:           seed.GroupID,
		ShareAmountMicros: &shareDebit,
		PayoutProof:       seed.Proof,
	})

	// Assert
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if result.SliceUsdc == depositRefund {
		t.Fatalf("payout %d equals naive deposit refund; want marked-pot slice", result.SliceUsdc)
	}
	if result.SliceUsdc <= 0 {
		t.Fatalf("sliceUsdc must be positive, got %d", result.SliceUsdc)
	}
	const potNavMicros = int64(4_500_000)
	expectedSlice, err := domain.ComputeRedeemSlice(domain.RedeemSliceInput{
		SharesRedeemedMicros: shareDebit,
		TotalSharesMicros:    seed.InitialShares,
		PotNav:               domain.USDCMicros(potNavMicros),
	})
	if err != nil {
		t.Fatalf("ComputeRedeemSlice: %v", err)
	}
	if result.SliceUsdc != int64(expectedSlice.UsdcOwed) {
		t.Fatalf("sliceUsdc = %d, want domain redeem slice %d", result.SliceUsdc, expectedSlice.UsdcOwed)
	}
}

func TestRedeem_concurrentDoubleRedeem_debitsOnce(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	seed := seedRedeemGroup(t, h, 4_000_000)
	shareDebit := int64(1_000_000)
	req := RedeemRequest{
		AccessToken:       seed.Token,
		GroupID:           seed.GroupID,
		ShareAmountMicros: &shareDebit,
		PayoutProof:       seed.Proof,
	}

	// Act
	var wg sync.WaitGroup
	results := make([]RedeemJobView, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = h.Redeem.Redeem(context.Background(), req)
		}(i)
	}
	wg.Wait()

	// Assert
	successes := 0
	for i, err := range errs {
		if err == nil && results[i].Status == domain.RedeemJobSettled {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful redeems = %d, want 1", successes)
	}
	pos, found, err := h.Store.GetPosition(context.Background(), seed.UserID, seed.GroupID)
	if err != nil || !found {
		t.Fatalf("GetPosition: found=%v err=%v", found, err)
	}
	if pos.ShareUnits != seed.InitialShares-shareDebit {
		t.Fatalf("shareUnits = %d, want single debit to %d", pos.ShareUnits, seed.InitialShares-shareDebit)
	}
}
