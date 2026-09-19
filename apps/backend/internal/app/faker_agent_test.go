package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/packages/domain"
)

// #153 on #202 agent trading: faker scale clubs never get agent governance or agent trades,
// and an agent row on a faker club (should one ever exist) cannot reach Privy or Jupiter.
func TestFakerAgentTrading_scaleClubIsReadOnly(t *testing.T) {
	fx := newSpectatorFixture(t)
	ctx := context.Background()

	for _, kind := range []domain.ProposalKind{
		domain.ProposalKindAddAgent, domain.ProposalKindPauseAgent,
		domain.ProposalKindResumeAgent, domain.ProposalKindRevokeAgent,
	} {
		_, err := fx.governance.CreateProposal(ctx, CreateProposalInput{
			GroupID: fx.fakerGroupID, ProposerID: fx.operatorID, Kind: kind,
			AgentDisplayName: "Demo Bot", AllocationUsdcMicros: 1_000_000,
		})
		if !errors.Is(err, ErrFakerGroupReadOnly) {
			t.Errorf("CreateProposal(%s) err = %v, want ErrFakerGroupReadOnly", kind, err)
		}
	}

	// Worst case: an active agent with a valid key on a faker club.
	const key = "abcde"
	execSQL(t, fx.h.DB, `INSERT INTO group_agents (group_id, status, agent_display_name, allocation_usdc_micros, api_key_hash, api_key_prefix)
VALUES ($1, 'active', 'Demo Bot', 5000000, $2, 'abc')`, fx.fakerGroupID, HashAgentAPIKey(key))

	intents := NewAgentIntentService(fx.h.Store, NewSwapService(fx.h.Store, NewBuyService(fx.h.Jupiter, fx.h.XStocks), fx.h.Jupiter, fx.privy, NewFakePrivyTreasurySigner(), "", fx.h.Symbols), fx.h.Symbols)
	for _, in := range []SubmitAgentIntentInput{
		{GroupID: fx.fakerGroupID, AgentKey: key, Side: domain.AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 1_000_000},
		{GroupID: fx.fakerGroupID, AgentKey: key, Side: domain.AgentIntentSell, Symbol: "AAPLx", TokenAmount: 1_000},
	} {
		if _, err := intents.SubmitAgentIntent(ctx, in); !errors.Is(err, ErrFakerGroupReadOnly) {
			t.Errorf("SubmitAgentIntent(%s) err = %v, want ErrFakerGroupReadOnly", in.Side, err)
		}
	}
	var n int
	if err := fx.h.DB.QueryRowContext(ctx, `SELECT count(*) FROM agent_intents WHERE group_id = $1`, fx.fakerGroupID).Scan(&n); err != nil || n != 0 {
		t.Errorf("faker agent intents = %d (err %v), want 0", n, err)
	}
	fx.assertNoFakerPrivy(t)
}
