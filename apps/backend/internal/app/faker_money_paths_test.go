package app

import (
	"context"
	"errors"
	"testing"
)

// #153 on main's money paths (#185/#187/#193/#194/#195): fund, withdraw-to-balance, redeem,
// governed sells, and ops sweep sources never touch faker scale clubs or ghost proposals.
func TestFakerMoneyPaths_rejectScaleClubAndGhostSells(t *testing.T) {
	fx := newSpectatorFixture(t)
	ctx := context.Background()
	shares := int64(1_000_000)

	if _, err := fx.home.deposits.FundGroup(ctx, fx.operatorToken, fx.fakerGroupID, 1_000_000); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("FundGroup err = %v, want ErrFakerGroupReadOnly", err)
	}
	if _, err := fx.h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{AccessToken: fx.operatorToken, GroupID: fx.fakerGroupID, ShareAmountMicros: &shares}); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("WithdrawToBalance err = %v, want ErrFakerGroupReadOnly", err)
	}
	if _, err := fx.h.Redeem.Redeem(ctx, RedeemRequest{AccessToken: fx.operatorToken, GroupID: fx.fakerGroupID, ShareAmountMicros: &shares}); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("Redeem err = %v, want ErrFakerGroupReadOnly", err)
	}
	if _, err := fx.governance.QuoteProposal(ctx, QuoteProposalInput{GroupID: fx.fakerGroupID, UserID: fx.operatorID, Symbol: "AAPLx", Kind: ProposalKindSell, TokenAmount: 10_000}); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("QuoteProposal(sell) err = %v, want ErrFakerGroupReadOnly", err)
	}
	if _, err := fx.governance.CreateProposal(ctx, CreateProposalInput{GroupID: fx.fakerGroupID, ProposerID: fx.operatorID, Symbol: "AAPLx", Kind: ProposalKindSell, TokenAmount: 10_000}); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("CreateProposal(sell) err = %v, want ErrFakerGroupReadOnly", err)
	}

	exec := NewExecuteOnPassService(fx.h.Swap, fx.h.Store)
	for _, p := range []Proposal{
		{ID: fx.fakerOpenID, GroupID: fx.fakerGroupID, ProposerID: fx.fakerBID, Symbol: "AAPLx", Kind: ProposalKindSell, TokenAmount: 10_000, Status: ProposalPassed},
		{ID: fx.ghostProposalID, GroupID: fx.realGroupID, ProposerID: fx.ghostID, Symbol: "AAPLx", Kind: ProposalKindSell, TokenAmount: 10_000, Status: ProposalPassed},
	} {
		if _, err := exec.ExecuteOnPass(ctx, p); !errors.Is(err, ErrFakerGroupReadOnly) {
			t.Errorf("ExecuteOnPass(sell in %s) err = %v, want ErrFakerGroupReadOnly", p.GroupID, err)
		}
	}

	// Ops sweep sources (sweep-wallets --source db): dummy faker treasuries are never listed.
	treasuries, err := fx.h.Store.ListTreasuries(ctx)
	if err != nil {
		t.Fatalf("ListTreasuries: %v", err)
	}
	for _, tr := range treasuries {
		if tr.GroupID == fx.fakerGroupID || tr.Address == fx.fakerTreasury {
			t.Errorf("ListTreasuries includes faker treasury %+v", tr)
		}
	}

	// Operator is still the only live holder in the mixed club: ghost shares stay out of the
	// share total that redeem/withdraw price against.
	total, err := fx.h.Store.SumShareUnitsByGroup(ctx, fx.realGroupID)
	if err != nil || total != 10_000_000 {
		t.Errorf("real club share total = %d (err %v), want operator's 10000000 only", total, err)
	}
	fx.assertNoFakerPrivy(t)
}
