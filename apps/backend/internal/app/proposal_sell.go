package app

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/packages/domain"
)

// QuoteProposalInput is member-only quote gating for buy or sell.
type QuoteProposalInput struct {
	GroupID     string
	UserID      string
	Symbol      string
	Kind        domain.ProposalKind
	UsdcMicros  int64
	TokenAmount int64
}

// QuoteProposalResult is the JSON-facing quote payload.
type QuoteProposalResult struct {
	Kind             domain.ProposalKind
	Symbol           string
	UsdcMicros       int64
	TokenAmount      int64
	Routable         bool
	OutputAmount     string
	OutputUsdcMicros string
	PriceUsdcMicros  string
}

// QuoteProposal checks group membership only (not proposer eligibility).
func (g *GovernanceService) QuoteProposal(ctx context.Context, in QuoteProposalInput) (QuoteProposalResult, error) {
	kind := in.Kind
	if kind == "" {
		kind = domain.ProposalKindBuy
	}
	if in.GroupID == "" || in.UserID == "" {
		return QuoteProposalResult{}, fmt.Errorf("group_id and user_id are required")
	}
	if err := rejectFakerGroup(ctx, g.store, in.GroupID); err != nil {
		return QuoteProposalResult{}, err
	}
	member, err := g.store.IsGroupMember(ctx, in.GroupID, in.UserID)
	if err != nil {
		return QuoteProposalResult{}, err
	}
	if !member {
		return QuoteProposalResult{}, ErrNotGroupMember
	}

	switch kind {
	case domain.ProposalKindBuy:
		if in.UsdcMicros <= 0 {
			return QuoteProposalResult{}, fmt.Errorf("usdc must be positive")
		}
		if g.buy == nil {
			return QuoteProposalResult{}, fmt.Errorf("buy service is required")
		}
		result, err := g.buy.StartBuy(ctx, StartBuyRequest{
			GroupID:    in.GroupID,
			UserID:     in.UserID,
			Symbol:     in.Symbol,
			USDCAmount: in.UsdcMicros,
		})
		if err != nil {
			return QuoteProposalResult{}, err
		}
		outMicros, err := dexAmountOutMicros(result.Quote)
		if err != nil {
			return QuoteProposalResult{}, err
		}
		return QuoteProposalResult{
			Kind:         domain.ProposalKindBuy,
			Symbol:       in.Symbol,
			UsdcMicros:   in.UsdcMicros,
			Routable:     result.Quote.Routable,
			OutputAmount: strconv.FormatInt(outMicros, 10),
		}, nil
	case domain.ProposalKindSell:
		return g.quoteSellForMember(ctx, in)
	default:
		return QuoteProposalResult{}, fmt.Errorf("invalid proposal kind")
	}
}

func (g *GovernanceService) quoteSellForMember(ctx context.Context, in QuoteProposalInput) (QuoteProposalResult, error) {
	if in.TokenAmount <= 0 {
		return QuoteProposalResult{}, fmt.Errorf("token amount must be positive")
	}
	if g.swap == nil {
		return QuoteProposalResult{}, fmt.Errorf("swap service is required")
	}
	quote, err := g.swap.QuoteSell(ctx, SellQuoteRequest{
		GroupID:     in.GroupID,
		UserID:      in.UserID,
		Symbol:      in.Symbol,
		TokenAmount: in.TokenAmount,
	})
	if err != nil {
		if errors.Is(err, ErrExceedsTreasuryHolding) || errors.Is(err, ErrQuoteNotRoutable) {
			return QuoteProposalResult{}, err
		}
		return QuoteProposalResult{}, err
	}
	outMicros, err := dexAmountOutMicros(quote.Quote)
	if err != nil {
		return QuoteProposalResult{}, err
	}
	return QuoteProposalResult{
		Kind:             domain.ProposalKindSell,
		Symbol:           in.Symbol,
		TokenAmount:      in.TokenAmount,
		Routable:         quote.Quote.Routable,
		OutputUsdcMicros: strconv.FormatInt(outMicros, 10),
	}, nil
}

// SellQuoteRequest is a price-only (no taker) sell quote against confirmed holdings.
type SellQuoteRequest struct {
	GroupID     string
	UserID      string
	Symbol      string
	TokenAmount int64
	Taker       string
}

// SellQuoteResult is a routable or refused sell quote.
type SellQuoteResult struct {
	InputToken string
	Quote      dex.Quote
}

// QuoteSell resolves the mint, enforces the confirmed holding ceiling, and quotes without a taker.
func (s *SwapService) QuoteSell(ctx context.Context, req SellQuoteRequest) (SellQuoteResult, error) {
	if s == nil || s.buy == nil || s.dex == nil {
		return SellQuoteResult{}, fmt.Errorf("swap service is required")
	}
	if req.TokenAmount <= 0 {
		return SellQuoteResult{}, fmt.Errorf("token amount must be positive")
	}
	inputMint, err := s.buy.ResolveOutputToken(ctx, req.Symbol)
	if err != nil {
		return SellQuoteResult{}, err
	}
	holdings, err := s.store.ListNetTokenHoldingsByGroup(ctx, req.GroupID)
	if err != nil {
		return SellQuoteResult{}, err
	}
	var held int64
	for _, holding := range holdings {
		if holding.Mint == inputMint {
			held = holding.Amount
			break
		}
	}
	if req.TokenAmount > held {
		return SellQuoteResult{}, ErrExceedsTreasuryHolding
	}
	quote, err := s.dex.QuoteSell(ctx, inputMint, big.NewInt(req.TokenAmount))
	if err != nil {
		return SellQuoteResult{}, err
	}
	if !quote.Routable {
		return SellQuoteResult{}, ErrQuoteNotRoutable
	}
	return SellQuoteResult{InputToken: inputMint, Quote: quote}, nil
}
