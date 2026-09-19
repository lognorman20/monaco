package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/packages/domain"
)

func seedProposalGroup(t *testing.T, store *Store, iso *TestIsolation) (userID, groupID string) {
	t.Helper()
	ctx := context.Background()
	user, err := store.UpsertUser(ctx, iso.UniquePrivyID("prop-user"), "Prop User")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)
	rules := domain.GroupRules{
		JoinPolicy:        domain.JoinPolicy{Mode: domain.JoinModeOpen},
		VoterSet:          domain.VoterSet{Mode: domain.VoterSetAllMembers},
		Threshold:         domain.ThresholdMajority,
		VoteExpirySeconds: 86_400,
	}
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	group, err := store.InsertGroupWithRulesTx(ctx, tx, "Prop Group "+iso.Suffix(), user.ID, rules)
	if err != nil {
		t.Fatalf("InsertGroupWithRulesTx: %v", err)
	}
	iso.TrackGroup(group.ID)
	if err := store.InsertGroupMemberTx(ctx, tx, group.ID, user.ID); err != nil {
		t.Fatalf("InsertGroupMemberTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return user.ID, group.ID
}

func TestInsertProposalTx_roundTripsBuyAndSellAmounts(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	buy, err := store.InsertProposalTx(ctx, tx, InsertProposalParams{
		GroupID:    groupID,
		ProposerID: userID,
		Symbol:     "AAPLx",
		Kind:       domain.ProposalKindBuy,
		UsdcMicros: 2_000_000,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("insert buy: %v", err)
	}
	sell, err := store.InsertProposalTx(ctx, tx, InsertProposalParams{
		GroupID:     groupID,
		ProposerID:  userID,
		Symbol:      "AAPLx",
		Kind:        domain.ProposalKindSell,
		TokenAmount: 50_000_000,
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("insert sell: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	gotBuy, ok, err := store.GetProposalByID(ctx, buy.ID)
	if err != nil || !ok {
		t.Fatalf("GetProposalByID buy: %v ok=%v", err, ok)
	}
	if gotBuy.Kind != domain.ProposalKindBuy || gotBuy.UsdcMicros != 2_000_000 || gotBuy.TokenAmount != 0 {
		t.Fatalf("buy row = %+v", gotBuy)
	}
	gotSell, ok, err := store.GetProposalByID(ctx, sell.ID)
	if err != nil || !ok {
		t.Fatalf("GetProposalByID sell: %v ok=%v", err, ok)
	}
	if gotSell.Kind != domain.ProposalKindSell || gotSell.TokenAmount != 50_000_000 || gotSell.UsdcMicros != 0 {
		t.Fatalf("sell row = %+v", gotSell)
	}
}

func TestInsertProposalTx_rejectsAmountForWrongKind(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = store.InsertProposalTx(ctx, tx, InsertProposalParams{
		GroupID:     groupID,
		ProposerID:  userID,
		Symbol:      "AAPLx",
		Kind:        domain.ProposalKindSell,
		UsdcMicros:  1,
		TokenAmount: 50_000_000,
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err == nil {
		t.Fatal("expected sell with usdc to be rejected")
	}
}

func TestListPassedProposalsPendingExecute_failedBuyStillListed(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	buy, err := store.InsertProposalTx(ctx, tx, InsertProposalParams{
		GroupID:    groupID,
		ProposerID: userID,
		Symbol:     "AAPLx",
		Kind:       domain.ProposalKindBuy,
		UsdcMicros: 2_000_000,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if ok, err := store.UpdateProposalStatusTx(ctx, tx, buy.ID, domain.ProposalOpen, domain.ProposalPassed); err != nil || !ok {
		t.Fatalf("pass proposal: %v ok=%v", err, ok)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	_, _, err = store.InsertPendingTransaction(ctx, InsertPendingTransactionParams{
		GroupID:          groupID,
		ProposalID:       buy.ID,
		Action:           TransactionActionBuy,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		Amount:           2_000_000,
		ExecuteRequestID: fmt.Sprintf("fail-buy-%s", buy.ID),
	})
	if err != nil {
		t.Fatalf("InsertPendingTransaction: %v", err)
	}
	if _, ok, err := store.FailTransactionByExecuteRequestID(ctx, fmt.Sprintf("fail-buy-%s", buy.ID)); err != nil || !ok {
		t.Fatalf("FailTransaction: %v ok=%v", err, ok)
	}

	listed, err := store.ListPassedProposalsPendingExecute(ctx, 50)
	if err != nil {
		t.Fatalf("ListPassedProposalsPendingExecute: %v", err)
	}
	found := false
	for _, row := range listed {
		if row.ID == buy.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("failed buy should still be pending execute")
	}
}

func TestListPassedProposalsPendingExecute_failedSellNotListed(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	userID, groupID := seedProposalGroup(t, store, iso)

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	sell, err := store.InsertProposalTx(ctx, tx, InsertProposalParams{
		GroupID:     groupID,
		ProposerID:  userID,
		Symbol:      "AAPLx",
		Kind:        domain.ProposalKindSell,
		TokenAmount: 50_000_000,
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if ok, err := store.UpdateProposalStatusTx(ctx, tx, sell.ID, domain.ProposalOpen, domain.ProposalPassed); err != nil || !ok {
		t.Fatalf("pass proposal: %v ok=%v", err, ok)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	reqID := fmt.Sprintf("fail-sell-%s", sell.ID)
	_, _, err = store.InsertPendingTransaction(ctx, InsertPendingTransactionParams{
		GroupID:          groupID,
		ProposalID:       sell.ID,
		Action:           TransactionActionSell,
		InputMint:        jupiter.AAPLxMint,
		OutputMint:       jupiter.USDCMint,
		Amount:           50_000_000,
		ExecuteRequestID: reqID,
	})
	if err != nil {
		t.Fatalf("InsertPendingTransaction: %v", err)
	}
	if _, ok, err := store.FailTransactionByExecuteRequestID(ctx, reqID); err != nil || !ok {
		t.Fatalf("FailTransaction: %v ok=%v", err, ok)
	}

	listed, err := store.ListPassedProposalsPendingExecute(ctx, 50)
	if err != nil {
		t.Fatalf("ListPassedProposalsPendingExecute: %v", err)
	}
	for _, row := range listed {
		if row.ID == sell.ID {
			t.Fatal("failed sell should not be pending automatic execute")
		}
	}
}

func TestGetFillDerivedCostBasis_afterPartialSellReturnsRemainingBasis(t *testing.T) {
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	ctx := context.Background()
	_, groupID := seedProposalGroup(t, store, iso)

	const buyTokens int64 = 100_000_000
	const buyUSDC int64 = 10_000_000
	const sellTokens int64 = 25_000_000
	_, _, err := store.ConfirmBuyTransaction(ctx, ConfirmBuyTransactionParams{
		GroupID:          groupID,
		Amount:           buyUSDC,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      fmt.Sprintf("sig-%s-basis-buy", iso.Suffix()),
		ExecuteRequestID: fmt.Sprintf("req-%s-basis-buy", iso.Suffix()),
		CostBasisPrice:   buyUSDC,
		CostBasisAmount:  buyTokens,
	})
	if err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}
	_, _, err = store.ConfirmSellTransaction(ctx, ConfirmSellTransactionParams{
		GroupID:          groupID,
		Amount:           sellTokens,
		InputMint:        jupiter.AAPLxMint,
		OutputMint:       jupiter.USDCMint,
		TxSignature:      fmt.Sprintf("sig-%s-basis-sell", iso.Suffix()),
		ExecuteRequestID: fmt.Sprintf("req-%s-basis-sell", iso.Suffix()),
		ProceedsUSDC:     2_500_000,
	})
	if err != nil {
		t.Fatalf("ConfirmSellTransaction: %v", err)
	}

	basis, remaining, found, err := store.GetFillDerivedCostBasisByOutputMint(ctx, groupID, jupiter.AAPLxMint)
	if err != nil || !found {
		t.Fatalf("GetFillDerivedCostBasisByOutputMint: found=%v err=%v", found, err)
	}
	if remaining != 75_000_000 {
		t.Fatalf("remaining tokens = %d, want 75000000", remaining)
	}
	if basis != 7_500_000 {
		t.Fatalf("remaining basis = %d, want 7500000", basis)
	}
}
