package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

func TestDomainNavInputFromPyth_scalesTokenAtomicsToDecimalUnits(t *testing.T) {
	// Arrange
	input := pyth.NavInput{
		TreasuryUsdc: 0,
		Holdings: []pyth.MarkedHolding{{
			Symbol:    "AAPLx",
			Units:     500_000,
			MarkUsdc:  600_000_000,
			CostBasis: 50_000_000,
		}},
	}

	// Act
	navInput, err := domainNavInputFromPyth(input, domain.ShareUnits("100"))

	// Assert
	if err != nil {
		t.Fatalf("domainNavInputFromPyth: %v", err)
	}
	if len(navInput.Holdings) != 1 {
		t.Fatalf("holdings len = %d, want 1", len(navInput.Holdings))
	}
	if navInput.Holdings[0].Units != "0.5" {
		t.Fatalf("units = %q, want %q", navInput.Holdings[0].Units, "0.5")
	}
}

func TestCostBasisForGroup_usesNetHoldingAtomicsNotLatestFillOnly(t *testing.T) {
	// Arrange
	h := integrationApp(t)
	ctx := context.Background()
	groupID, _, _ := seedSwapGroup(t, h)

	if _, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          groupID,
		Amount:           100_000_000,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      "BUY-cost-basis-first",
		ExecuteRequestID: "req-cost-basis-first",
		CostBasisPrice:   100_000_000,
		CostBasisAmount:  800_000,
	}); err != nil {
		t.Fatalf("first ConfirmBuyTransaction: %v", err)
	}
	if _, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          groupID,
		Amount:           50_000_000,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      "BUY-cost-basis-second",
		ExecuteRequestID: "req-cost-basis-second",
		CostBasisPrice:   50_000_000,
		CostBasisAmount:  300_000,
	}); err != nil {
		t.Fatalf("second ConfirmBuyTransaction: %v", err)
	}

	holdings, err := h.Store.ListNetTokenHoldingsByGroup(ctx, groupID)
	if err != nil {
		t.Fatalf("ListNetTokenHoldingsByGroup: %v", err)
	}
	if len(holdings) != 1 || holdings[0].Amount != 1_100_000 {
		t.Fatalf("net holding amount = %+v, want 1100000 atomics", holdings)
	}

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Act
	costBasis, err := h.Deposits.costBasisForGroup(ctx, tx, groupID, holdings)

	// Assert
	if err != nil {
		t.Fatalf("costBasisForGroup: %v", err)
	}
	if len(costBasis) != 1 {
		t.Fatalf("cost basis len = %d, want 1", len(costBasis))
	}
	if costBasis[0].Units != 1_100_000 {
		t.Fatalf("cost basis units = %d, want 1100000 (net), not latest fill 300000", costBasis[0].Units)
	}
}

func TestShareCreditForSweep_excludesInboundSweepFromTreasuryNav(t *testing.T) {
	// Arrange — post-sweep treasury includes Blair USDC; stock marks to $110 on 1 share.
	h := integrationApp(t)
	ctx := context.Background()
	groupID, userID, treasuryAddress := seedSwapGroup(t, h)
	const (
		alexDeposit  = int64(100_000_000)
		blairDeposit = int64(110_000_000)
	)

	if _, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          groupID,
		Amount:           alexDeposit,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      "BUY-nav-test",
		ExecuteRequestID: "req-nav-test",
		CostBasisPrice:   alexDeposit,
		CostBasisAmount:  1_000_000,
	}); err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, userID, groupID, 100_000_000, alexDeposit); err != nil {
		t.Fatalf("seed alex position: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	privy.SetTreasuryUSDCBalance(h.Privy, treasuryAddress, blairDeposit)

	pyth.RegisterMarkedPot(h.Pyth, pyth.TreasuryRef{GroupID: groupID, Address: treasuryAddress}, pyth.NavInput{
		Holdings: []pyth.MarkedHolding{{
			Symbol:    "AAPLx",
			Units:     1_000_000,
			MarkUsdc:  110_000_000,
			CostBasis: alexDeposit,
		}},
	})

	creditTx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx credit: %v", err)
	}
	defer func() { _ = creditTx.Rollback() }()

	// Act
	shareUnits, err := h.Deposits.shareCreditForSweep(ctx, creditTx, groupID, treasuryAddress, blairDeposit)

	// Assert
	if err != nil {
		t.Fatalf("shareCreditForSweep: %v", err)
	}
	if shareUnits != 100_000_000 {
		t.Fatalf("shareUnits = %d, want 100000000 (fair mint, not double-counted treasury)", shareUnits)
	}
}
