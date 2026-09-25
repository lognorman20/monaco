package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/clawpump"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	soltreasury "github.com/monaco/monaco/apps/backend/internal/solana/treasury"
)

func TestAgentDeploymentRoutes_deployVoteAndPotRow(t *testing.T) {
	a := newMultiUserApp(t)
	alice := a.signIn(t, "deploy-alice", "Alice")
	c := a.createClub(t, alice, "Deploy Cabal")
	agent := soltreasury.FakeAddress("http-agent-" + a.ISO.Suffix())
	path := "/v1/groups/" + c.ID + "/proposals"
	post := func(body string) int {
		return a.call(t, a.Proposals.CreateProposalHandler, http.MethodPost, path, alice.Token, body, "id", c.ID).Code
	}

	if code := post(`{"kind":"deploy_agent","usdc":1000000}`); code != http.StatusBadRequest {
		t.Fatalf("deploy without wallet = %d, want 400", code)
	}
	if code := post(`{"kind":"recall_agent","agentWalletAddress":"` + agent + `","usdc":5}`); code != http.StatusBadRequest {
		t.Fatalf("recall with usdc = %d, want 400", code)
	}
	if code := post(`{"kind":"deploy_agent","agentWalletAddress":"` + agent + `","usdc":1000000}`); code != http.StatusServiceUnavailable {
		t.Fatalf("deploy without solana treasury = %d, want 503", code)
	}

	solana := soltreasury.NewFake()
	a.Groups.Home.SetSolanaTreasury(solana)
	svc := app.NewAgentDeploymentService(a.Store, solana, clawpump.NewFake(), []byte("0123456789abcdef0123456789abcdef"), a.Groups.Home.PotTreasuryUSDCMicros)
	a.Groups.Governance.SetAgentDeploymentService(svc)
	ref, err := svc.EnsureSolanaTreasury(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	solana.SetUSDCBalance(ref.SolanaAddress, 2_000_000)

	if code := post(`{"kind":"deploy_agent","agentWalletAddress":"0x833589fcd6edb6e08f4c7c32d4f71b54bda02913","usdc":1000000}`); code != http.StatusBadRequest {
		t.Fatalf("base address = %d, want 400", code)
	}
	if code := post(`{"kind":"deploy_agent","agentWalletAddress":"` + agent + `","usdc":2000001}`); code != http.StatusBadRequest {
		t.Fatalf("deploy above treasury cash = %d, want 400", code)
	}
	rec := a.call(t, a.Proposals.CreateProposalHandler, http.MethodPost, path, alice.Token,
		`{"kind":"deploy_agent","agentWalletAddress":"`+agent+`","usdc":2000000,"operatorKey":"cpk_test"}`, "id", c.ID)
	requireStatus(t, rec, http.StatusOK, "deploy_agent create")
	proposalID := decodeBody[createProposalResponse](t, rec).ProposalID

	detail := a.proposalDetail(t, alice, proposalID)
	if detail.Kind != "deploy_agent" || detail.AgentWalletAddress != agent || detail.UsdcMicros != "2000000" {
		t.Fatalf("detail = %+v", detail)
	}
	requireStatus(t, a.vote(t, alice, proposalID, "yes"), http.StatusNoContent, "deploy vote")

	pending, err := a.Store.ListAgentDeploymentsByStatus(context.Background(), postgres.AgentDeploymentPendingTransfer, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var mine *postgres.AgentDeploymentRow
	for i := range pending {
		if pending[i].GroupID == c.ID {
			mine = &pending[i]
		}
	}
	if mine == nil {
		t.Fatal("no pending_transfer deployment after the vote passed")
	}
	if _, err := svc.ProcessPendingTransfer(context.Background(), *mine); err != nil {
		t.Fatal(err)
	}

	view := a.groupView(t, alice, c)
	var found bool
	for _, row := range view.Pot {
		if row.Symbol == app.DeployedUSDCPotSymbol {
			found = row.ValueUsd == "2.00"
		}
	}
	if !found {
		t.Fatalf("pot rows = %+v, want a Deployed USDC row worth 2.00", view.Pot)
	}
}
