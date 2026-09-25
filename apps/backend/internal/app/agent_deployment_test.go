package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/clawpump"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	soltreasury "github.com/monaco/monaco/apps/backend/internal/solana/treasury"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/packages/domain"
)

type deploymentHarness struct {
	governanceHarness
	Home     *HomeService
	Solana   *soltreasury.Fake
	Clawpump *clawpump.Fake
	Svc      *AgentDeploymentService
	Token    string
	UserID   string
	GroupID  string
	// BaseTreasury is the group's Base treasury address; SolanaTreasury its Solana one.
	BaseTreasury   string
	SolanaTreasury string
}

var testOperatorKeyKey = []byte("0123456789abcdef0123456789abcdef")

// newDeploymentHarness creates a one-member cabal funded with 10 USDC of share units:
// 4 USDC in the Base treasury and 6 USDC SPL USDC in its Solana treasury.
func newDeploymentHarness(t *testing.T, label string) deploymentHarness {
	t.Helper()
	ctx := context.Background()
	h := integrationGovernanceApp(t)
	home := NewHomeService(h.Store, h.Auth, h.Wallets, nil, nil, h.Symbols)
	solana := soltreasury.NewFake()
	home.SetSolanaTreasury(solana)
	h.Governance.SetHomeService(home)
	claw := clawpump.NewFake()
	svc := NewAgentDeploymentService(h.Store, solana, claw, testOperatorKeyKey, home.PotTreasuryUSDCMicros)
	h.Governance.SetAgentDeploymentService(svc)

	session := openTestSession(t, h.ISO, h.Sessions, h.Auth, label, "Deployer")
	token := h.ISO.UniqueToken(label)
	created, err := h.Governance.CreateGroupWithRules(ctx, token, testGroupName(h.ISO, label), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)

	tx, err := h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Store.IncrementPositionTx(ctx, tx, session.UserID, created.GroupID, 10_000_000, 10_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	wallets.SetTreasuryUSDCBalance(h.Wallets, created.TreasuryAddress, 4_000_000)
	ref, err := svc.EnsureSolanaTreasury(ctx, created.GroupID)
	if err != nil {
		t.Fatalf("EnsureSolanaTreasury: %v", err)
	}
	solana.SetUSDCBalance(ref.SolanaAddress, 6_000_000)

	return deploymentHarness{
		governanceHarness: h,
		Home:              home,
		Solana:            solana,
		Clawpump:          claw,
		Svc:               svc,
		Token:             token,
		UserID:            session.UserID,
		GroupID:           created.GroupID,
		BaseTreasury:      created.TreasuryAddress,
		SolanaTreasury:    ref.SolanaAddress,
	}
}

func (d deploymentHarness) propose(t *testing.T, in CreateProposalInput) (Proposal, error) {
	t.Helper()
	in.GroupID = d.GroupID
	in.ProposerID = d.UserID
	return d.Governance.CreateProposal(context.Background(), in)
}

func (d deploymentHarness) pass(t *testing.T, proposal Proposal) {
	t.Helper()
	got, err := d.Governance.CastVote(context.Background(), CastVoteInput{ProposalID: proposal.ID, VoterID: d.UserID, Choice: domain.VoteYes})
	if err != nil {
		t.Fatalf("cast vote: %v", err)
	}
	if got.Status != ProposalPassed {
		t.Fatalf("proposal status = %s, want passed", got.Status)
	}
}

func (d deploymentHarness) deployment(t *testing.T, wallet string) postgres.AgentDeploymentRow {
	t.Helper()
	var id string
	err := d.DB.QueryRowContext(context.Background(), `
SELECT id FROM group_agent_deployments WHERE group_id = $1 AND agent_wallet_address = $2
ORDER BY created_at DESC LIMIT 1`, d.GroupID, wallet).Scan(&id)
	if err != nil {
		t.Fatalf("find deployment: %v", err)
	}
	row, found, err := d.Store.GetAgentDeploymentByID(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("GetAgentDeploymentByID: found=%v err=%v", found, err)
	}
	return row
}

func (d deploymentHarness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.DB.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func (d deploymentHarness) shareUnits(t *testing.T) int64 {
	t.Helper()
	var units int64
	if err := d.DB.QueryRowContext(context.Background(),
		`SELECT share_units FROM positions WHERE group_id = $1 AND user_id = $2`, d.GroupID, d.UserID).Scan(&units); err != nil {
		t.Fatalf("share units: %v", err)
	}
	return units
}

func (d deploymentHarness) view(t *testing.T) GroupViewResult {
	t.Helper()
	view, err := d.Home.GetGroupView(context.Background(), d.Token, d.GroupID)
	if err != nil {
		t.Fatalf("GetGroupView: %v", err)
	}
	return view
}

func deployedRow(view GroupViewResult) (GroupViewPotRow, bool) {
	for _, row := range view.Pot {
		if row.Symbol == DeployedUSDCPotSymbol {
			return row, true
		}
	}
	return GroupViewPotRow{}, false
}

// deployAndSend passes a deploy vote and runs the send step to deployed.
func (d deploymentHarness) deployAndSend(t *testing.T, wallet string, micros int64, operatorKey string) postgres.AgentDeploymentRow {
	t.Helper()
	proposal, err := d.propose(t, CreateProposalInput{
		Kind:               domain.ProposalKindDeployAgent,
		AgentWalletAddress: wallet,
		UsdcMicros:         micros,
		OperatorKey:        operatorKey,
	})
	if err != nil {
		t.Fatalf("create deploy proposal: %v", err)
	}
	d.pass(t, proposal)
	outcome, err := d.Svc.ProcessPendingTransfer(context.Background(), d.deployment(t, wallet))
	if err != nil || outcome != AgentDeploymentOutcomeDeployed {
		t.Fatalf("ProcessPendingTransfer = %s, %v; want deployed", outcome, err)
	}
	return d.deployment(t, wallet)
}

func TestAgentDeployment_createGates(t *testing.T) {
	t.Parallel()
	d := newDeploymentHarness(t, "deploy-gates")
	agent := soltreasury.FakeAddress("gates-agent-" + d.ISO.Suffix())

	cases := []struct {
		name string
		in   CreateProposalInput
		want error
	}{
		{"missing wallet", CreateProposalInput{Kind: domain.ProposalKindDeployAgent, UsdcMicros: 1_000_000}, ErrAgentWalletRequired},
		{"base address is not solana", CreateProposalInput{Kind: domain.ProposalKindDeployAgent, UsdcMicros: 1_000_000, AgentWalletAddress: "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"}, ErrInvalidAgentWallet},
		{"treasury itself", CreateProposalInput{Kind: domain.ProposalKindDeployAgent, UsdcMicros: 1_000_000, AgentWalletAddress: d.SolanaTreasury}, ErrInvalidAgentWallet},
		// 6 USDC SPL cash; pot NAV is 10 USDC but only Solana cash can be sent.
		{"above solana treasury cash", CreateProposalInput{Kind: domain.ProposalKindDeployAgent, UsdcMicros: 6_000_001, AgentWalletAddress: agent}, ErrExceedsTreasuryUSDC},
		{"non cpk operator key", CreateProposalInput{Kind: domain.ProposalKindDeployAgent, UsdcMicros: 1_000_000, AgentWalletAddress: agent, OperatorKey: "sk_live_nope"}, ErrInvalidOperatorKey},
		{"recall with nothing deployed", CreateProposalInput{Kind: domain.ProposalKindRecallAgent, AgentWalletAddress: agent}, ErrAgentDeploymentNotFound},
	}
	for _, tc := range cases {
		if _, err := d.propose(t, tc.in); !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindDeployAgent, UsdcMicros: 0, AgentWalletAddress: agent}); err == nil {
		t.Fatal("zero usdc deploy accepted")
	}

	// Exactly the treasury cash is allowed; a second open deploy for the same wallet is not.
	first, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindDeployAgent, UsdcMicros: 6_000_000, AgentWalletAddress: agent})
	if err != nil {
		t.Fatalf("deploy at cash ceiling: %v", err)
	}
	if first.Symbol != agent {
		t.Fatalf("symbol = %q, want the agent wallet", first.Symbol)
	}
	if _, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindDeployAgent, UsdcMicros: 1_000_000, AgentWalletAddress: agent}); !errors.Is(err, ErrAgentDeploymentProposalOpen) {
		t.Fatalf("second open deploy err = %v", err)
	}

	// Once deployed, a new deploy for the same wallet is a second active deployment.
	d.pass(t, first)
	if _, err := d.Svc.ProcessPendingTransfer(context.Background(), d.deployment(t, agent)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindDeployAgent, UsdcMicros: 1, AgentWalletAddress: agent}); !errors.Is(err, ErrAgentDeploymentExists) {
		t.Fatalf("second active deployment err = %v", err)
	}
}

func TestAgentDeployment_unavailableWithoutSolanaTreasury(t *testing.T) {
	t.Parallel()
	d := newDeploymentHarness(t, "deploy-unconfigured")
	d.Governance.SetAgentDeploymentService(nil)
	_, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindDeployAgent, UsdcMicros: 1_000_000, AgentWalletAddress: soltreasury.FakeAddress("x")})
	if !errors.Is(err, ErrAgentDeploymentsUnavailable) {
		t.Fatalf("err = %v, want ErrAgentDeploymentsUnavailable", err)
	}
}

func TestAgentDeployment_passSendsOnceAndPotCountsOnlyAfterConfirm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := newDeploymentHarness(t, "deploy-send")
	agent := soltreasury.FakeAddress("send-agent-" + d.ISO.Suffix())
	sharesBefore := d.shareUnits(t)

	if view := d.view(t); view.PotTotalUsd != "10.00" {
		t.Fatalf("pot before deploy = %s, want 10.00", view.PotTotalUsd)
	}

	proposal, err := d.propose(t, CreateProposalInput{
		Kind:               domain.ProposalKindDeployAgent,
		AgentWalletAddress: agent,
		UsdcMicros:         5_000_000,
		OperatorKey:        "cpk_secret_operator",
	})
	if err != nil {
		t.Fatalf("create deploy proposal: %v", err)
	}
	if n := d.count(t, `SELECT COUNT(*) FROM group_agent_deploy_operator_keys WHERE proposal_id = $1 AND operator_key_enc NOT LIKE '%cpk_%'`, proposal.ID); n != 1 {
		t.Fatalf("sealed operator keys = %d, want 1 encrypted row", n)
	}
	if n := d.count(t, `SELECT COUNT(*) FROM group_agent_deployments WHERE group_id = $1`, d.GroupID); n != 0 {
		t.Fatalf("deployments before pass = %d", n)
	}

	d.pass(t, proposal)

	row := d.deployment(t, agent)
	if row.Status != postgres.AgentDeploymentPendingTransfer || row.DeployedUsdcMicros != 5_000_000 || row.ReturnedUsdcMicros != 0 {
		t.Fatalf("deployment after pass = %+v", row)
	}
	if !row.DeployProposalID.Valid || row.DeployProposalID.String != proposal.ID {
		t.Fatalf("deploy_proposal_id = %v", row.DeployProposalID)
	}
	if !row.OperatorKeyEnc.Valid || strings.Contains(row.OperatorKeyEnc.String, "cpk_") {
		t.Fatal("operator key must be copied to the deployment, encrypted")
	}
	if n := d.count(t, `SELECT COUNT(*) FROM group_agent_deploy_operator_keys WHERE proposal_id = $1`, proposal.ID); n != 0 {
		t.Fatal("sealed operator key must move to the deployment on pass")
	}
	if n := d.count(t, `SELECT COUNT(*) FROM group_agents WHERE group_id = $1`, d.GroupID); n != 0 {
		t.Fatal("deploy pass must not create a group_agents row or Monaco API key")
	}

	// pending_transfer: the cash is still in the treasury, so nothing is added on top.
	view := d.view(t)
	if view.PotTotalUsd != "10.00" {
		t.Fatalf("pot while pending_transfer = %s, want 10.00", view.PotTotalUsd)
	}
	if _, ok := deployedRow(view); ok {
		t.Fatal("no Deployed USDC row before the outbound transfer confirms")
	}

	// The proposal execute poller's query never hands a deploy vote to ExecuteOnPass.
	pending, err := d.Store.ListPassedProposalsPendingExecute(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pending {
		if p.ID == proposal.ID {
			t.Fatal("deploy_agent listed for buy/sell execution")
		}
	}

	outcome, err := d.Svc.ProcessPendingTransfer(ctx, row)
	if err != nil || outcome != AgentDeploymentOutcomeDeployed {
		t.Fatalf("ProcessPendingTransfer = %s, %v", outcome, err)
	}
	if d.Solana.PrepareCount() != 1 || d.Solana.BroadcastCount() != 1 {
		t.Fatalf("prepare=%d broadcast=%d, want 1/1", d.Solana.PrepareCount(), d.Solana.BroadcastCount())
	}
	payout := d.Solana.LastPayout()
	if payout.ToAddress != agent || payout.Amount != 5_000_000 || payout.TreasuryRef.SolanaAddress != d.SolanaTreasury {
		t.Fatalf("payout = %+v", payout)
	}
	if cash, _ := d.Solana.TreasuryUSDCBalance(ctx, d.SolanaTreasury); cash != 1_000_000 {
		t.Fatalf("solana treasury after send = %d, want 1000000", cash)
	}

	row = d.deployment(t, agent)
	if row.Status != postgres.AgentDeploymentDeployed || !row.OutboundTransferID.Valid {
		t.Fatalf("deployment after send = %+v", row)
	}
	transfer, found, err := d.Store.GetAgentOutboundTransfer(ctx, row.OutboundTransferID.String)
	if err != nil || !found || !transfer.TxSignature.Valid || transfer.Status != "confirmed" || transfer.Amount != 5_000_000 || transfer.ToAddress != agent {
		t.Fatalf("outbound transfer = %+v, found=%v err=%v", transfer, found, err)
	}

	// A deployed row is never sent again.
	if outcome, err := d.Svc.ProcessPendingTransfer(ctx, row); err != nil || outcome != AgentDeploymentOutcomeWaiting {
		t.Fatalf("re-run on deployed = %s, %v", outcome, err)
	}
	if d.Solana.PrepareCount() != 1 {
		t.Fatal("deployed row signed a second transfer")
	}

	// deployed: 4 Base + 1 Solana + 5 outstanding = 10.
	view = d.view(t)
	if view.PotTotalUsd != "10.00" {
		t.Fatalf("pot after deploy = %s, want 10.00", view.PotTotalUsd)
	}
	deployed, ok := deployedRow(view)
	if !ok || deployed.ValueUsd != "5.00" {
		t.Fatalf("Deployed USDC row = %+v, %v; want 5.00", deployed, ok)
	}
	if view.You.EquityUsd != "10.00" {
		t.Fatalf("member equity = %s, want 10.00", view.You.EquityUsd)
	}

	if n := d.count(t, `SELECT COUNT(*) FROM nav_snapshots WHERE group_id = $1 AND reason = 'agent_deployment'`, d.GroupID); n != 1 {
		t.Fatalf("agent_deployment snapshots = %d, want 1", n)
	}
	var snapshotNav int64
	if err := d.DB.QueryRowContext(ctx, `SELECT pot_nav_micros FROM nav_snapshots WHERE group_id = $1 AND reason = 'agent_deployment'`, d.GroupID).Scan(&snapshotNav); err != nil || snapshotNav != 10_000_000 {
		t.Fatalf("snapshot pot nav = %d, %v; want 10000000", snapshotNav, err)
	}

	if n := d.count(t, `SELECT COUNT(*) FROM agent_intents WHERE group_id = $1`, d.GroupID); n != 0 {
		t.Fatal("deployment inserted agent intents")
	}
	if n := d.count(t, `SELECT COUNT(*) FROM transactions WHERE group_id = $1`, d.GroupID); n != 0 {
		t.Fatal("deployment inserted swap transactions")
	}
	if n := d.count(t, `SELECT COUNT(*) FROM withdrawals WHERE group_id = $1`, d.GroupID); n != 0 {
		t.Fatal("deployment used the member withdrawals rail")
	}
	if got := d.shareUnits(t); got != sharesBefore {
		t.Fatalf("share units %d -> %d on deploy", sharesBefore, got)
	}

	// Buys can only spend what is in the treasury, not USDC out with the agent.
	total, err := d.Governance.proposalTreasuryTotalMicros(ctx, d.GroupID)
	if err != nil || total != 5_000_000 {
		t.Fatalf("buy ceiling = %d, %v; want 5000000", total, err)
	}
}

func TestAgentDeployment_sendResumesFromStoredSignature(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := newDeploymentHarness(t, "deploy-resume")
	agent := soltreasury.FakeAddress("resume-agent-" + d.ISO.Suffix())

	proposal, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindDeployAgent, AgentWalletAddress: agent, UsdcMicros: 2_000_000})
	if err != nil {
		t.Fatal(err)
	}
	d.pass(t, proposal)

	// Send fails before a signature exists: nothing recorded, still pending_transfer.
	d.Solana.FailPrepare(errors.New("privy down"))
	if _, err := d.Svc.ProcessPendingTransfer(ctx, d.deployment(t, agent)); err == nil {
		t.Fatal("expected prepare failure")
	}
	d.Solana.FailPrepare(nil)
	row := d.deployment(t, agent)
	if row.Status != postgres.AgentDeploymentPendingTransfer || row.OutboundTransferID.Valid {
		t.Fatalf("after prepare failure = %+v", row)
	}

	// Signed and stored, broadcast errors, chain has not seen it yet.
	d.Solana.FailBroadcast(errors.New("rpc timeout"))
	d.Solana.SetDefaultPayoutState(soltreasury.PayoutStatePending)
	if outcome, err := d.Svc.ProcessPendingTransfer(ctx, row); err != nil || outcome != AgentDeploymentOutcomeSent {
		t.Fatalf("broadcast failure = %s, %v", outcome, err)
	}
	d.Solana.FailBroadcast(nil)
	row = d.deployment(t, agent)
	if row.Status != postgres.AgentDeploymentPendingTransfer || !row.OutboundTransferID.Valid {
		t.Fatalf("after broadcast failure = %+v", row)
	}
	first, _, _ := d.Store.GetAgentOutboundTransfer(ctx, row.OutboundTransferID.String)

	// Still pending on chain: no second signature.
	if outcome, err := d.Svc.ProcessPendingTransfer(ctx, row); err != nil || outcome != AgentDeploymentOutcomeWaiting {
		t.Fatalf("pending = %s, %v", outcome, err)
	}
	if d.Solana.PrepareCount() != 1 {
		t.Fatalf("prepare count = %d, want 1", d.Solana.PrepareCount())
	}

	// Blockhash expired without landing: unlink, then the next tick signs a fresh transfer.
	d.Solana.SetPayoutState(first.TxSignature.String, soltreasury.PayoutStateDropped)
	if outcome, err := d.Svc.ProcessPendingTransfer(ctx, d.deployment(t, agent)); err != nil || outcome != AgentDeploymentOutcomeResend {
		t.Fatalf("dropped = %s, %v", outcome, err)
	}
	failed, _, _ := d.Store.GetAgentOutboundTransfer(ctx, first.ID)
	if failed.Status != "failed" {
		t.Fatalf("dropped transfer status = %s", failed.Status)
	}
	d.Solana.SetDefaultPayoutState(soltreasury.PayoutStateConfirmed)
	if outcome, err := d.Svc.ProcessPendingTransfer(ctx, d.deployment(t, agent)); err != nil || outcome != AgentDeploymentOutcomeDeployed {
		t.Fatalf("resend = %s, %v", outcome, err)
	}
	if d.Solana.PrepareCount() != 2 {
		t.Fatalf("prepare count = %d, want 2", d.Solana.PrepareCount())
	}
	row = d.deployment(t, agent)
	if row.Status != postgres.AgentDeploymentDeployed || row.OutboundTransferID.String == first.ID {
		t.Fatalf("after resend = %+v", row)
	}
	if n := d.count(t, `SELECT COUNT(*) FROM group_agent_outbound_transfers WHERE group_id = $1 AND status = 'confirmed'`, d.GroupID); n != 1 {
		t.Fatalf("confirmed outbound transfers = %d, want exactly 1", n)
	}
}

func TestAgentDeployment_recallWithOperatorKeyCommandsOnceAndWaitsForInbound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := newDeploymentHarness(t, "recall-operated")
	agent := soltreasury.FakeAddress("operated-agent-" + d.ISO.Suffix())
	d.deployAndSend(t, agent, 5_000_000, "cpk_operator_key")
	sharesBefore := d.shareUnits(t)

	recall, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindRecallAgent, AgentWalletAddress: agent})
	if err != nil {
		t.Fatalf("create recall: %v", err)
	}
	if _, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindRecallAgent, AgentWalletAddress: agent}); !errors.Is(err, ErrAgentDeploymentProposalOpen) {
		t.Fatalf("second open recall err = %v", err)
	}
	if _, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindRecallAgent, AgentWalletAddress: agent, UsdcMicros: 1}); err == nil {
		t.Fatal("recall with usdc accepted")
	}
	d.pass(t, recall)

	row := d.deployment(t, agent)
	if row.Status != postgres.AgentDeploymentRecalling || row.RecallProposalID.String != recall.ID || row.ReturnedUsdcMicros != 0 {
		t.Fatalf("after recall pass = %+v", row)
	}

	// The command goes out; nothing is credited from its response.
	if outcome, err := d.Svc.ProcessRecalling(ctx, row); err != nil || outcome != AgentDeploymentOutcomeWaiting {
		t.Fatalf("recall step = %s, %v", outcome, err)
	}
	calls := d.Clawpump.Calls()
	if len(calls) != 2 || calls[0].Tool != clawpump.ToolSetExternalWallet || calls[0].Address != d.SolanaTreasury ||
		calls[1].Tool != clawpump.ToolAgentSend || calls[1].Address != d.SolanaTreasury || calls[1].AmountMicros != 5_000_000 ||
		calls[0].OperatorKey != "cpk_operator_key" {
		t.Fatalf("clawpump calls = %+v", calls)
	}
	row = d.deployment(t, agent)
	if row.ReturnedUsdcMicros != 0 || row.Status != postgres.AgentDeploymentRecalling {
		t.Fatalf("command must not credit a return: %+v", row)
	}
	if _, err := d.Svc.ProcessRecalling(ctx, row); err != nil {
		t.Fatal(err)
	}
	if len(d.Clawpump.Calls()) != 2 {
		t.Fatal("agent_send must be commanded once per recall")
	}

	now := time.Now().UTC()
	d.Solana.AddInboundTransfer(d.SolanaTreasury, soltreasury.InboundTransfer{TxSignature: "in-1-" + d.ISO.Suffix(), FromAddress: agent, Amount: 2_000_000, BlockTime: now})
	d.Solana.AddInboundTransfer(d.SolanaTreasury, soltreasury.InboundTransfer{TxSignature: "stranger-" + d.ISO.Suffix(), FromAddress: soltreasury.FakeAddress("stranger"), Amount: 9_000_000, BlockTime: now})
	if outcome, err := d.Svc.ProcessRecalling(ctx, d.deployment(t, agent)); err != nil || outcome != AgentDeploymentOutcomeCredited {
		t.Fatalf("first inbound = %s, %v", outcome, err)
	}
	row = d.deployment(t, agent)
	if row.ReturnedUsdcMicros != 2_000_000 || row.Status != postgres.AgentDeploymentRecalling || !row.InboundTransferID.Valid {
		t.Fatalf("after partial return = %+v", row)
	}

	// Replaying the same signature credits nothing.
	if outcome, err := d.Svc.ProcessRecalling(ctx, row); err != nil || outcome != AgentDeploymentOutcomeWaiting {
		t.Fatalf("replay = %s, %v", outcome, err)
	}
	if got := d.deployment(t, agent).ReturnedUsdcMicros; got != 2_000_000 {
		t.Fatalf("replay changed returned to %d", got)
	}

	// More than outstanding comes back: credit caps at deployed, the extra stays uncredited.
	d.Solana.AddInboundTransfer(d.SolanaTreasury, soltreasury.InboundTransfer{TxSignature: "in-2-" + d.ISO.Suffix(), FromAddress: agent, Amount: 4_000_000, BlockTime: now})
	if outcome, err := d.Svc.ProcessRecalling(ctx, d.deployment(t, agent)); err != nil || outcome != AgentDeploymentOutcomeClosed {
		t.Fatalf("final inbound = %s, %v", outcome, err)
	}
	row = d.deployment(t, agent)
	if row.ReturnedUsdcMicros != 5_000_000 || row.Status != postgres.AgentDeploymentClosed {
		t.Fatalf("after full return = %+v", row)
	}
	if n := d.count(t, `SELECT COUNT(*) FROM group_agent_inbound_transfers WHERE group_id = $1`, d.GroupID); n != 2 {
		t.Fatalf("inbound rows = %d, want 2 (stranger ignored)", n)
	}
	if n := d.count(t, `SELECT COUNT(*) FROM deposits WHERE group_id = $1`, d.GroupID); n != 0 {
		t.Fatal("return used the member deposits rail")
	}
	if got := d.shareUnits(t); got != sharesBefore {
		t.Fatalf("share units %d -> %d on recall", sharesBefore, got)
	}
	// deploy + two credited returns.
	if n := d.count(t, `SELECT COUNT(*) FROM nav_snapshots WHERE group_id = $1 AND reason = 'agent_deployment'`, d.GroupID); n != 3 {
		t.Fatalf("agent_deployment snapshots = %d, want 3", n)
	}
	if _, ok := deployedRow(d.view(t)); ok {
		t.Fatal("closed deployment still shown as Deployed USDC")
	}
}

func TestAgentDeployment_recallWithoutOperatorKeyNeverCallsClawpump(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := newDeploymentHarness(t, "recall-request")
	agent := soltreasury.FakeAddress("third-party-agent-" + d.ISO.Suffix())
	d.deployAndSend(t, agent, 3_000_000, "")

	recall, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindRecallAgent, AgentWalletAddress: agent})
	if err != nil {
		t.Fatal(err)
	}
	d.pass(t, recall)
	if _, err := d.Svc.ProcessRecalling(ctx, d.deployment(t, agent)); err != nil {
		t.Fatal(err)
	}
	if got := d.deployment(t, agent); got.Status != postgres.AgentDeploymentRecalling || got.ReturnedUsdcMicros != 0 {
		t.Fatalf("recall request = %+v", got)
	}
	view := d.view(t)
	if deployed, ok := deployedRow(view); !ok || deployed.ValueUsd != "3.00" {
		t.Fatalf("recalling deployment must stay in the pot: %+v %v", deployed, ok)
	}

	d.Solana.AddInboundTransfer(d.SolanaTreasury, soltreasury.InboundTransfer{TxSignature: "owner-return-" + d.ISO.Suffix(), FromAddress: agent, Amount: 3_000_000, BlockTime: time.Now()})
	if outcome, err := d.Svc.ProcessRecalling(ctx, d.deployment(t, agent)); err != nil || outcome != AgentDeploymentOutcomeClosed {
		t.Fatalf("inbound = %s, %v", outcome, err)
	}
	if len(d.Clawpump.Calls()) != 0 {
		t.Fatalf("clawpump called without an operator key: %+v", d.Clawpump.Calls())
	}
}

func TestAgentDeployment_operatorCommandFailureStaysRecalling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := newDeploymentHarness(t, "recall-fail")
	agent := soltreasury.FakeAddress("fail-agent-" + d.ISO.Suffix())
	d.deployAndSend(t, agent, 1_000_000, "cpk_operator")
	recall, err := d.propose(t, CreateProposalInput{Kind: domain.ProposalKindRecallAgent, AgentWalletAddress: agent})
	if err != nil {
		t.Fatal(err)
	}
	d.pass(t, recall)

	d.Clawpump.Fail(clawpump.ErrToolFailed)
	if _, err := d.Svc.ProcessRecalling(ctx, d.deployment(t, agent)); !errors.Is(err, clawpump.ErrToolFailed) {
		t.Fatalf("err = %v, want tool failure", err)
	}
	row := d.deployment(t, agent)
	if row.Status != postgres.AgentDeploymentRecalling || row.RecallCommandSent.Valid {
		t.Fatalf("after failed command = %+v", row)
	}
	d.Clawpump.Fail(nil)
	if _, err := d.Svc.ProcessRecalling(ctx, row); err != nil {
		t.Fatal(err)
	}
	if !d.deployment(t, agent).RecallCommandSent.Valid {
		t.Fatal("retried command not recorded")
	}
}

func TestAgentDeployment_failedDeployVoteDropsSealedOperatorKey(t *testing.T) {
	t.Parallel()
	d := newDeploymentHarness(t, "deploy-fail-vote")
	proposal, err := d.propose(t, CreateProposalInput{
		Kind:               domain.ProposalKindDeployAgent,
		AgentWalletAddress: soltreasury.FakeAddress("no-" + d.ISO.Suffix()),
		UsdcMicros:         1_000_000,
		OperatorKey:        "cpk_x",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Governance.CastVote(context.Background(), CastVoteInput{ProposalID: proposal.ID, VoterID: d.UserID, Choice: domain.VoteNo})
	if err != nil || got.Status != ProposalFailed {
		t.Fatalf("vote no = %+v, %v", got, err)
	}
	if n := d.count(t, `SELECT COUNT(*) FROM group_agent_deploy_operator_keys WHERE proposal_id = $1`, proposal.ID); n != 0 {
		t.Fatal("failed deploy vote left its operator key behind")
	}
	if n := d.count(t, `SELECT COUNT(*) FROM group_agent_deployments WHERE group_id = $1`, d.GroupID); n != 0 {
		t.Fatal("failed deploy vote created a deployment")
	}
}
