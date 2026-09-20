package worker

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/packages/domain"
)

func TestProposalExecutePoller_executesPassedProposal(t *testing.T) {
	t.Parallel()

	testApp := integrationWorkerApp(t)
	store := testApp.Store
	jupiterClient := dex.NewFakeClient()
	xstocksResolver := b20.NewFakeCatalog()
	buy := app.NewBuyService(jupiterClient, xstocksResolver)
	signer := app.NewFakePrivyTreasurySigner()
	swap := app.NewSwapService(store, buy, jupiterClient, testApp.Privy, signer, "", app.NewSymbolResolver(nil))
	governance := app.NewGovernanceService(store, testApp.Privy)
	governance.SetBuyService(buy)
	executeOnPass := app.NewExecuteOnPassService(swap, store)

	ctx := context.Background()
	privyUserID := testApp.ISO.UniqueDynamicID("execute-poller")
	token := auth.AccessToken(testApp.ISO.UniqueToken("execute-poller"))
	auth.RegisterToken(testApp.Privy, token, auth.Identity{PrivyUserID: privyUserID, DisplayName: "Execute Poller"})
	sessions := app.NewSessionService(store, auth.NewFakeVerifier(), testApp.Privy)
	session, err := sessions.OpenSession(ctx, string(token))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	testApp.ISO.TrackUser(session.UserID)

	const usdcAmount int64 = 2_000_000

	group, err := governance.CreateGroupWithRules(ctx, string(token), "Execute Poller "+testApp.ISO.Suffix(), app.DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	testApp.ISO.TrackGroup(group.GroupID)
	wallets.SetTreasuryUSDCBalance(testApp.Privy, group.TreasuryAddress, usdcAmount)

	requestID := "req-" + testApp.ISO.Suffix()
	signature := "sig-" + testApp.ISO.Suffix()
	b20.RegisterTokenAddress(xstocksResolver, "AAPLx", "0xb200000000000000000000c2e324d24d7eecd1fb")
	jupiter.RegisterQuoteBuy(jupiterClient, "0xb200000000000000000000c2e324d24d7eecd1fb", usdcAmount, jupiter.BuyQuote{
		Routable:   true,
		InputToken:  evm.USDCAddress,
		OutputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		InAmount:   "2000000",
		OutAmount:  "1000000",
		RequestID:  requestID,
	})
	jupiter.RegisterBuyOrder(jupiterClient, requestID, jupiter.BuyOrder{
		RequestID:   requestID,
		Transaction: "unsigned-buy-tx",
		InAmount:    "2000000",
		OutAmount:   "1000000",
		InputToken:   evm.USDCAddress,
		OutputToken:  "0xb200000000000000000000c2e324d24d7eecd1fb",
	})
	jupiter.RegisterExecutePoll(jupiterClient, requestID, []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusPending, Code: -1},
		{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          signature,
			InputAmountResult:  "2000000",
			OutputAmountResult: "1000000",
		},
	})

	proposal, err := governance.CreateProposal(ctx, app.CreateProposalInput{
		GroupID:    group.GroupID,
		ProposerID: session.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: usdcAmount,
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	passed, err := governance.CastVote(ctx, app.CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    session.UserID,
		Choice:     domain.VoteYes,
	})
	if err != nil {
		t.Fatalf("cast vote: %v", err)
	}
	if passed.Status != app.ProposalPassed {
		t.Fatalf("status = %q, want passed", passed.Status)
	}

	poller := NewProposalExecutePoller(store, executeOnPass, nil)
	poller.tick(ctx)

	tx, found, err := store.GetConfirmedTransactionByProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("get confirmed tx: %v", err)
	}
	if !found {
		t.Fatal("expected confirmed transaction after poller tick")
	}
	if !tx.TxHash.Valid || tx.TxHash.String != signature {
		t.Fatalf("tx_signature = %v, want %q", tx.TxHash, signature)
	}
}

func TestProposalExecutePoller_nilSafe(t *testing.T) {
	t.Parallel()

	RunProposalExecutePoller(context.Background(), nil, DefaultProposalExecuteInterval)
	poller := NewProposalExecutePoller(nil, nil, nil)
	poller.tick(context.Background())
}
