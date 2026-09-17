package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

func TestGetGroupView_afterUSDCtoAAPLxSwap_potTotalUnchanged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "swap-nav", "Swap NAV")
	token := string(privy.AccessToken(h.ISO.UniqueToken("swap-nav")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "swap-nav"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	if err := insertGroupMember(t, ctx, h.Store, group.GroupID, session.UserID); err != nil {
		t.Fatalf("insertGroupMember: %v", err)
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	const depositMicros = int64(2_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, depositMicros, depositMicros); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := h.Store.WriteNavSnapshotOnDepositConfirmTx(ctx, tx, group.GroupID, depositMicros); err != nil {
		t.Fatalf("WriteNavSnapshotOnDepositConfirmTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	const swappedUSDC = int64(1_000_000)
	const aaplAtomics = int64(500_000)
	_, _, err = h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           swappedUSDC,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      testTxSignature(h.ISO, "buy-aapl"),
		ExecuteRequestID: testRequestID(h.ISO, "buy-aapl"),
		CostBasisPrice:   swappedUSDC,
		CostBasisAmount:  aaplAtomics,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	remainingUSDC := depositMicros - swappedUSDC
	privy.SetTreasuryUSDCBalance(h.Privy, treasury.SolanaAddress, remainingUSDC)

	pyth.RegisterMarkedPot(h.Pyth, pyth.TreasuryRef{
		GroupID: group.GroupID,
		Address: treasury.SolanaAddress,
	}, pyth.NavInput{
		TreasuryUsdc: remainingUSDC,
		Holdings: []pyth.MarkedHolding{{
			Symbol:    "AAPLx",
			Mint:      jupiter.AAPLxMint,
			Units:     aaplAtomics,
			MarkUsdc:  2_000_000,
			CostBasis: swappedUSDC,
		}},
	})

	view, err := home.GetGroupView(ctx, token, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupView: %v", err)
	}

	if view.PotTotalUsd != "2.00" {
		t.Fatalf("potTotalUsd = %q, want 2.00", view.PotTotalUsd)
	}
	if len(view.Pot) != 2 {
		t.Fatalf("pot rows = %d, want 2", len(view.Pot))
	}
	if view.Pot[0].Symbol != "USDC" || view.Pot[0].ValueUsd != "1.00" {
		t.Fatalf("USDC row = %+v, want $1.00", view.Pot[0])
	}
	if view.Pot[0].DollarPnL != "+0.00" {
		t.Fatalf("USDC dollarPnl = %q, want +0.00", view.Pot[0].DollarPnL)
	}
	if view.Pot[1].Symbol != "AAPLx" || view.Pot[1].ValueUsd != "1.00" {
		t.Fatalf("AAPLx row = %+v, want $1.00", view.Pot[1])
	}
	if view.Pot[1].DollarPnL != "+0.00" {
		t.Fatalf("AAPLx dollarPnl = %q, want +0.00 at flat mark", view.Pot[1].DollarPnL)
	}
	if view.You.EquityUsd != "2.00" {
		t.Fatalf("equityUsd = %q, want 2.00", view.You.EquityUsd)
	}
}

func TestComputeGroupPotView_costBasisFallback_withoutPyth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "cb-nav", "CB NAV")
	token := string(privy.AccessToken(h.ISO.UniqueToken("cb-nav")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "cb-nav"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	if err := insertGroupMember(t, ctx, h.Store, group.GroupID, session.UserID); err != nil {
		t.Fatalf("insertGroupMember: %v", err)
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	const depositMicros = int64(2_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, depositMicros, depositMicros); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	const swappedUSDC = int64(1_000_000)
	const aaplAtomics = int64(500_000)
	_, _, err = h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           swappedUSDC,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      testTxSignature(h.ISO, "cb-buy"),
		ExecuteRequestID: testRequestID(h.ISO, "cb-buy"),
		CostBasisPrice:   swappedUSDC,
		CostBasisAmount:  aaplAtomics,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	remainingUSDC := depositMicros - swappedUSDC
	potView, err := computeGroupPotView(ctx, h.Store, nil, h.Symbols, group.GroupID, treasury.SolanaAddress, remainingUSDC)
	if err != nil {
		t.Fatalf("computeGroupPotView: %v", err)
	}
	if potView.PotNavMicros != depositMicros {
		t.Fatalf("potNavMicros = %d, want %d", potView.PotNavMicros, depositMicros)
	}
	if len(potView.Rows) != 2 {
		t.Fatalf("pot rows = %d, want 2", len(potView.Rows))
	}
}

func TestGetHome_pythError_stillSucceeds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "pyth-err", "Pyth Err")
	token := string(privy.AccessToken(h.ISO.UniqueToken("pyth-err")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "pyth-err"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	if err := insertGroupMember(t, ctx, h.Store, group.GroupID, session.UserID); err != nil {
		t.Fatalf("insertGroupMember: %v", err)
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	const depositMicros = int64(2_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, depositMicros, depositMicros); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	const swappedUSDC = int64(1_000_000)
	const aaplAtomics = int64(500_000)
	_, _, err = h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           swappedUSDC,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      testTxSignature(h.ISO, "pyth-err-buy"),
		ExecuteRequestID: testRequestID(h.ISO, "pyth-err-buy"),
		CostBasisPrice:   swappedUSDC,
		CostBasisAmount:  aaplAtomics,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	remainingUSDC := depositMicros - swappedUSDC
	privy.SetTreasuryUSDCBalance(h.Privy, treasury.SolanaAddress, remainingUSDC)

	pyth.RegisterMarkedPotError(h.Pyth, pyth.TreasuryRef{
		GroupID: group.GroupID,
		Address: treasury.SolanaAddress,
	}, fmt.Errorf("pyth latest price: status 403"))

	result, err := home.GetHome(ctx, token)
	if err != nil {
		t.Fatalf("GetHome: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("groups len = %d, want 1", len(result.Groups))
	}
	if result.Groups[0].PotValueUsd != "2.00" {
		t.Fatalf("potValueUsd = %q, want 2.00 (cost basis fallback)", result.Groups[0].PotValueUsd)
	}
}

func TestGetGroupView_pythError_stillSucceeds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "view-pyth", "View Pyth")
	token := string(privy.AccessToken(h.ISO.UniqueToken("view-pyth")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "view-pyth"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	if err := insertGroupMember(t, ctx, h.Store, group.GroupID, session.UserID); err != nil {
		t.Fatalf("insertGroupMember: %v", err)
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	const depositMicros = int64(2_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, depositMicros, depositMicros); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	const swappedUSDC = int64(1_000_000)
	const aaplAtomics = int64(500_000)
	_, _, err = h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           swappedUSDC,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      testTxSignature(h.ISO, "view-pyth-buy"),
		ExecuteRequestID: testRequestID(h.ISO, "view-pyth-buy"),
		CostBasisPrice:   swappedUSDC,
		CostBasisAmount:  aaplAtomics,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	remainingUSDC := depositMicros - swappedUSDC
	privy.SetTreasuryUSDCBalance(h.Privy, treasury.SolanaAddress, remainingUSDC)

	pyth.RegisterMarkedPotError(h.Pyth, pyth.TreasuryRef{
		GroupID: group.GroupID,
		Address: treasury.SolanaAddress,
	}, fmt.Errorf("pyth latest price: status 403"))

	view, err := home.GetGroupView(ctx, token, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupView: %v", err)
	}
	if view.PotTotalUsd != "2.00" {
		t.Fatalf("potTotalUsd = %q, want 2.00 (cost basis fallback)", view.PotTotalUsd)
	}
}

func TestGetGroupView_perAssetDollarPnL_gainAndLoss(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "pot-pnl", "Pot PnL")
	token := string(privy.AccessToken(h.ISO.UniqueToken("pot-pnl")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "pot-pnl"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	if err := insertGroupMember(t, ctx, h.Store, group.GroupID, session.UserID); err != nil {
		t.Fatalf("insertGroupMember: %v", err)
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	const depositMicros = int64(2_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, depositMicros, depositMicros); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	const swappedUSDC = int64(1_000_000)
	const aaplAtomics = int64(500_000)
	_, _, err = h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           swappedUSDC,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      testTxSignature(h.ISO, "pot-pnl-buy"),
		ExecuteRequestID: testRequestID(h.ISO, "pot-pnl-buy"),
		CostBasisPrice:   swappedUSDC,
		CostBasisAmount:  aaplAtomics,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	remainingUSDC := depositMicros - swappedUSDC
	privy.SetTreasuryUSDCBalance(h.Privy, treasury.SolanaAddress, remainingUSDC)

	pyth.RegisterMarkedPot(h.Pyth, pyth.TreasuryRef{
		GroupID: group.GroupID,
		Address: treasury.SolanaAddress,
	}, pyth.NavInput{
		TreasuryUsdc: remainingUSDC,
		Holdings: []pyth.MarkedHolding{{
			Symbol:    "AAPLx",
			Mint:      jupiter.AAPLxMint,
			Units:     aaplAtomics,
			MarkUsdc:  2_400_000,
			CostBasis: swappedUSDC,
		}},
	})

	view, err := home.GetGroupView(ctx, token, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupView: %v", err)
	}
	if view.Pot[0].DollarPnL != "+0.00" {
		t.Fatalf("USDC dollarPnl = %q, want +0.00", view.Pot[0].DollarPnL)
	}
	if view.Pot[1].DollarPnL != "+0.20" {
		t.Fatalf("AAPLx dollarPnl = %q, want +0.20", view.Pot[1].DollarPnL)
	}
}

func TestGetGroupView_TSLAxBuy_potRowShowsTickerNotMint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := integrationApp(t)
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "tsla-pot", "TSLA Pot")
	token := string(privy.AccessToken(h.ISO.UniqueToken("tsla-pot")))
	group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, "tsla-pot"))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	if err := insertGroupMember(t, ctx, h.Store, group.GroupID, session.UserID); err != nil {
		t.Fatalf("insertGroupMember: %v", err)
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}

	const depositMicros = int64(2_000_000)
	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, group.GroupID, depositMicros, depositMicros); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	const swappedUSDC = int64(150_000)
	const tslaAtomics = int64(41_287)
	_, _, err = h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           swappedUSDC,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.TSLAxMint,
		TxSignature:      testTxSignature(h.ISO, "buy-tsla"),
		ExecuteRequestID: testRequestID(h.ISO, "buy-tsla"),
		CostBasisPrice:   swappedUSDC,
		CostBasisAmount:  tslaAtomics,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}

	remainingUSDC := depositMicros - swappedUSDC
	privy.SetTreasuryUSDCBalance(h.Privy, treasury.SolanaAddress, remainingUSDC)

	pyth.RegisterMarkedPot(h.Pyth, pyth.TreasuryRef{
		GroupID: group.GroupID,
		Address: treasury.SolanaAddress,
	}, pyth.NavInput{
		TreasuryUsdc: remainingUSDC,
		Holdings: []pyth.MarkedHolding{{
			Symbol:    "TSLAx",
			Mint:      jupiter.TSLAxMint,
			Units:     tslaAtomics,
			MarkUsdc:  3_630_000,
			CostBasis: swappedUSDC,
		}},
	})

	view, err := home.GetGroupView(ctx, token, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupView: %v", err)
	}
	if len(view.Pot) != 2 {
		t.Fatalf("pot rows = %d, want 2", len(view.Pot))
	}
	if view.Pot[1].Symbol != "TSLAx" {
		t.Fatalf("holding symbol = %q, want TSLAx (not raw mint)", view.Pot[1].Symbol)
	}
	if view.Pot[1].Symbol == jupiter.TSLAxMint {
		t.Fatalf("pot row leaked raw mint as symbol")
	}
}
