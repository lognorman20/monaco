package app

import (
	"context"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/packages/domain"
)

func TestListGroupActivity_includesDepositsBuysSellsAndMixedStatuses(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)
	governance := NewGovernanceService(h.Store, h.Privy)
	governance.SetBuyService(NewBuyService(h.Jupiter, h.XStocks))

	session := openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "activity-user", "Activity User")
	token := auth.AccessToken(h.ISO.UniqueToken("activity-user"))
	auth.RegisterToken(h.Privy, token, auth.Identity{
		PrivyUserID: h.ISO.UniqueDynamicID("activity-user"),
		DisplayName: "Activity User",
	})

	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "activity"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, group.TreasuryAddress, 20_000_000)
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "AAPLx", 6_000_000)

	if _, err := h.Store.InsertDeposit(ctx, session.UserID, group.GroupID, 1_000_000, "from-wallet"); err != nil {
		t.Fatalf("insert pending deposit: %v", err)
	}
	confirmedDepositID := mustInsertDeposit(t, h, session.UserID, group.GroupID, 2_000_000)
	dbTx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, _, err := h.Store.ConfirmDepositTx(ctx, dbTx, confirmedDepositID, "dep-sig"); err != nil {
		_ = dbTx.Rollback()
		t.Fatalf("confirm deposit: %v", err)
	}
	if err := dbTx.Commit(); err != nil {
		t.Fatalf("commit deposit confirm: %v", err)
	}

	confirmedBuy, _, err := h.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          group.GroupID,
		Amount:           3_000_000,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:      testTxHash(h.ISO, "buy-confirmed"),
		ExecuteRequestID: testRequestID(h.ISO, "buy-confirmed"),
		CostBasisPrice:   3_000_000,
		CostBasisAmount:  1_500_000,
	})
	if err != nil {
		t.Fatalf("confirm buy: %v", err)
	}
	_ = confirmedBuy

	if _, _, err := h.Store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          group.GroupID,
		Action:           postgres.TransactionActionBuy,
		InputToken:        evm.USDCAddress,
		OutputToken:       "0xb200000000000000000000c2e324d24d7eecd1fb",
		Amount:           4_000_000,
		ExecuteRequestID: testRequestID(h.ISO, "buy-pending"),
	}); err != nil {
		t.Fatalf("insert pending buy: %v", err)
	}

	if _, err := h.Store.InsertFailedTransaction(ctx, group.GroupID, postgres.TransactionActionBuy, evm.USDCAddress, "0xb200000000000000000000c2e324d24d7eecd1fb", 5_000_000, testRequestID(h.ISO, "buy-failed")); err != nil {
		t.Fatalf("insert failed buy: %v", err)
	}

	confirmedSell, _, err := h.Store.ConfirmSellTransaction(ctx, postgres.ConfirmSellTransactionParams{
		GroupID:          group.GroupID,
		Amount:           1_000_000,
		InputToken:        "0xb200000000000000000000c2e324d24d7eecd1fb",
		OutputToken:       evm.USDCAddress,
		TxHash:      testTxHash(h.ISO, "sell-confirmed"),
		ExecuteRequestID: testRequestID(h.ISO, "sell-confirmed"),
		ProceedsUSDC:     900_000,
	})
	if err != nil {
		t.Fatalf("confirm sell: %v", err)
	}
	_ = confirmedSell

	proposal, err := governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:    group.GroupID,
		ProposerID: session.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: 6_000_000,
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	passed, err := governance.CastVote(ctx, CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    session.UserID,
		Choice:     domain.VoteYes,
	})
	if err != nil {
		t.Fatalf("cast vote: %v", err)
	}
	if passed.Status != ProposalPassed {
		t.Fatalf("proposal status = %q, want passed", passed.Status)
	}

	items, err := home.ListGroupActivity(ctx, string(token), group.GroupID)
	if err != nil {
		t.Fatalf("ListGroupActivity: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected activity items")
	}

	statuses := map[string]int{}
	kinds := map[string]int{}
	for _, item := range items {
		statuses[item.Status]++
		kinds[item.Kind]++
	}

	for _, wantStatus := range []string{
		postgres.TransactionStatusPending,
		postgres.TransactionStatusConfirmed,
		postgres.TransactionStatusFailed,
	} {
		if statuses[wantStatus] == 0 {
			t.Fatalf("missing status %q in activity: %+v", wantStatus, statuses)
		}
	}
	if kinds["deposit"] == 0 {
		t.Fatalf("missing deposits in activity: %+v", kinds)
	}
	if kinds["buy"] == 0 {
		t.Fatalf("missing buys in activity: %+v", kinds)
	}
	if kinds["sell"] == 0 {
		t.Fatalf("missing sells in activity: %+v", kinds)
	}

	foundAwaitingProposal := false
	for _, item := range items {
		if item.ID == proposal.ID && item.Kind == "buy" && item.Status == postgres.TransactionStatusPending {
			foundAwaitingProposal = true
			break
		}
	}
	if !foundAwaitingProposal {
		t.Fatal("expected passed proposal awaiting execute as pending buy")
	}
}

func mustInsertDeposit(t *testing.T, h integrationHarness, userID, groupID string, amount int64) string {
	t.Helper()
	row, err := h.Store.InsertDeposit(context.Background(), userID, groupID, amount, "from-wallet-2")
	if err != nil {
		t.Fatalf("insert deposit: %v", err)
	}
	return row.ID
}

func TestListGroupActivity_emptyWhenNoRows(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)
	governance := NewGovernanceService(h.Store, h.Privy)

	token := auth.AccessToken(h.ISO.UniqueToken("activity-empty"))
	auth.RegisterToken(h.Privy, token, auth.Identity{
		PrivyUserID: h.ISO.UniqueDynamicID("activity-empty"),
		DisplayName: "Empty User",
	})
	openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "activity-empty", "Empty User")

	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "empty"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	items, err := home.ListGroupActivity(ctx, string(token), group.GroupID)
	if err != nil {
		t.Fatalf("ListGroupActivity: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %d, want 0", len(items))
	}
}

func TestListGroupActivity_sortsNewestFirst(t *testing.T) {
	t.Parallel()

	h := integrationApp(t)
	ctx := context.Background()
	home := NewHomeService(h.Store, h.Privy, h.Pyth, h.Deposits, h.Symbols)
	governance := NewGovernanceService(h.Store, h.Privy)

	token := auth.AccessToken(h.ISO.UniqueToken("activity-sort"))
	auth.RegisterToken(h.Privy, token, auth.Identity{
		PrivyUserID: h.ISO.UniqueDynamicID("activity-sort"),
		DisplayName: "Sort User",
	})
	openTestSession(t, h.ISO, NewSessionService(h.Store, h.Privy), h.Privy, "activity-sort", "Sort User")

	group, err := governance.CreateGroupWithRules(ctx, string(token), testGroupName(h.ISO, "sort"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)

	older := time.Now().UTC().Add(-2 * time.Hour)
	newer := time.Now().UTC().Add(-1 * time.Hour)
	if _, err := h.Store.InsertFailedTransaction(ctx, group.GroupID, postgres.TransactionActionBuy, evm.USDCAddress, "0xb200000000000000000000c2e324d24d7eecd1fb", 1_000_000, testRequestID(h.ISO, "old")); err != nil {
		t.Fatalf("insert older failed buy: %v", err)
	}
	if _, err := h.DB.ExecContext(ctx, `UPDATE transactions SET created_at = $2 WHERE execute_request_id = $1`, testRequestID(h.ISO, "old"), older); err != nil {
		t.Fatalf("backdate older tx: %v", err)
	}
	if _, err := h.Store.InsertFailedTransaction(ctx, group.GroupID, postgres.TransactionActionSell, "0xb200000000000000000000c2e324d24d7eecd1fb", evm.USDCAddress, 1_000_000, testRequestID(h.ISO, "new")); err != nil {
		t.Fatalf("insert newer failed sell: %v", err)
	}
	if _, err := h.DB.ExecContext(ctx, `UPDATE transactions SET created_at = $2 WHERE execute_request_id = $1`, testRequestID(h.ISO, "new"), newer); err != nil {
		t.Fatalf("backdate newer tx: %v", err)
	}

	items, err := home.ListGroupActivity(ctx, string(token), group.GroupID)
	if err != nil {
		t.Fatalf("ListGroupActivity: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("items = %d, want at least 2", len(items))
	}
	if !items[0].CreatedAt.After(items[1].CreatedAt) && !items[0].CreatedAt.Equal(items[1].CreatedAt) {
		t.Fatalf("expected newest first, got %v then %v", items[0].CreatedAt, items[1].CreatedAt)
	}
}
