package worker

import (
	"context"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/clawpump"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	soltreasury "github.com/monaco/monaco/apps/backend/internal/solana/treasury"
	"github.com/monaco/monaco/packages/domain"
)

func TestAgentDeploymentPoller_deployRecallRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testApp := integrationWorkerApp(t)
	store := testApp.Store

	solana := soltreasury.NewFake()
	claw := clawpump.NewFake()
	svc := app.NewAgentDeploymentService(store, solana, claw, []byte("0123456789abcdef0123456789abcdef"),
		func(context.Context, string) (int64, error) { return 0, nil })
	governance := app.NewGovernanceService(store, testApp.Auth, testApp.Privy)
	governance.SetAgentDeploymentService(svc)

	token := auth.AccessToken(testApp.ISO.UniqueToken("deploy-poller"))
	auth.RegisterToken(testApp.Auth, token, auth.Identity{DynamicUserID: testApp.ISO.UniqueDynamicID("deploy-poller"), DisplayName: "Deploy Poller"})
	session, err := app.NewSessionService(store, testApp.Auth, testApp.Privy).OpenSession(ctx, string(token))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	testApp.ISO.TrackUser(session.UserID)
	group, err := governance.CreateGroupWithRules(ctx, string(token), "Deploy Poller "+testApp.ISO.Suffix(), app.DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	testApp.ISO.TrackGroup(group.GroupID)
	ref, err := svc.EnsureSolanaTreasury(ctx, group.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	solana.SetUSDCBalance(ref.SolanaAddress, 3_000_000)

	agent := soltreasury.FakeAddress("poller-agent-" + testApp.ISO.Suffix())
	vote := func(kind domain.ProposalKind, usdc int64) app.Proposal {
		t.Helper()
		proposal, err := governance.CreateProposal(ctx, app.CreateProposalInput{
			GroupID: group.GroupID, ProposerID: session.UserID, Kind: kind, AgentWalletAddress: agent, UsdcMicros: usdc,
		})
		if err != nil {
			t.Fatalf("create %s: %v", kind, err)
		}
		passed, err := governance.CastVote(ctx, app.CastVoteInput{ProposalID: proposal.ID, VoterID: session.UserID, Choice: domain.VoteYes})
		if err != nil || passed.Status != domain.ProposalPassed {
			t.Fatalf("pass %s: %+v, %v", kind, passed, err)
		}
		return passed
	}
	status := func() postgres.AgentDeploymentRow {
		t.Helper()
		rows, err := store.ListAgentDeploymentsByStatus(ctx, postgres.AgentDeploymentPendingTransfer, 1000)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []postgres.AgentDeploymentStatus{postgres.AgentDeploymentDeployed, postgres.AgentDeploymentRecalling, postgres.AgentDeploymentClosed} {
			more, err := store.ListAgentDeploymentsByStatus(ctx, s, 1000)
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, more...)
		}
		for _, row := range rows {
			if row.GroupID == group.GroupID {
				return row
			}
		}
		t.Fatal("deployment not found")
		return postgres.AgentDeploymentRow{}
	}

	deploy := vote(domain.ProposalKindDeployAgent, 3_000_000)

	// The buy/sell executor never runs a deploy vote.
	jupiter := dex.NewFakeClient()
	catalog := b20.NewFakeCatalog()
	buy := app.NewBuyService(jupiter, catalog)
	swap := app.NewSwapService(store, buy, jupiter, testApp.Privy, nil, app.NewSymbolResolver(catalog))
	executePoller := NewProposalExecutePoller(store, app.NewExecuteOnPassService(swap, store), NewStubClock(testApp.Now))
	executePoller.tick(ctx)
	if _, backedOff := executePoller.backoff[deploy.ID]; backedOff {
		t.Fatal("execute poller tried to run a deploy_agent proposal")
	}

	poller := NewAgentDeploymentPoller(store, svc, NewStubClock(testApp.Now))
	poller.Tick(ctx)
	if row := status(); row.Status != postgres.AgentDeploymentDeployed {
		t.Fatalf("after tick = %s, want deployed", row.Status)
	}
	poller.Tick(ctx)
	if solana.PrepareCount() != 1 {
		t.Fatalf("sent %d times, want once", solana.PrepareCount())
	}

	vote(domain.ProposalKindRecallAgent, 0)
	poller.Tick(ctx)
	if row := status(); row.Status != postgres.AgentDeploymentRecalling || row.ReturnedUsdcMicros != 0 {
		t.Fatalf("recalling before inbound = %+v", row)
	}
	if len(claw.Calls()) != 0 {
		t.Fatal("no operator key: clawpump must not be called")
	}

	solana.AddInboundTransfer(ref.SolanaAddress, soltreasury.InboundTransfer{
		TxSignature: "poller-return-" + testApp.ISO.Suffix(), FromAddress: agent, Amount: 3_000_000, BlockTime: time.Now(),
	})
	poller.Tick(ctx)
	poller.Tick(ctx)
	if row := status(); row.Status != postgres.AgentDeploymentClosed || row.ReturnedUsdcMicros != 3_000_000 {
		t.Fatalf("after inbound = %+v", row)
	}
}
