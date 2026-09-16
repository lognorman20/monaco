package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// ErrQuoteNotRoutable means Jupiter returned no route for the requested buy.
var ErrQuoteNotRoutable = errors.New("quote not routable")

// StartBuyRequest is input for the M3 dev stub buy gate (M4 vote gate later).
type StartBuyRequest struct {
	GroupID    string
	UserID     string
	Symbol     string
	USDCAmount int64
}

// StartBuyResult holds a routable Jupiter quote ready for execute.
type StartBuyResult struct {
	OutputMint string
	Quote      jupiter.BuyQuote
}

// BuyService gates treasury buys behind quote availability.
type BuyService struct {
	jupiter  jupiter.Client
	xstocks  xstocks.Resolver
}

// NewBuyService wires Jupiter quote and xStocks mint resolution.
func NewBuyService(jupiterClient jupiter.Client, resolver xstocks.Resolver) *BuyService {
	return &BuyService{
		jupiter: jupiterClient,
		xstocks: resolver,
	}
}

// StartBuy resolves the xStock mint and refuses when Jupiter has no route.
func (s *BuyService) StartBuy(ctx context.Context, req StartBuyRequest) (StartBuyResult, error) {
	logSwapQuoteAttempt(req.GroupID, req.UserID, req.Symbol, req.USDCAmount)

	outputMint, err := s.xstocks.ResolveSolanaMint(ctx, req.Symbol)
	if err != nil {
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, err.Error())
		return StartBuyResult{}, err
	}

	quote, err := s.jupiter.QuoteBuy(ctx, jupiter.QuoteBuyParams{
		GroupID:    req.GroupID,
		UserID:     req.UserID,
		Symbol:     req.Symbol,
		OutputMint: outputMint,
		USDCAmount: req.USDCAmount,
	})
	if err != nil {
		reason := err.Error()
		if errors.Is(err, jupiter.ErrNoRoute) {
			reason = "no route"
		}
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, reason)
		if errors.Is(err, jupiter.ErrNoRoute) {
			return StartBuyResult{}, fmt.Errorf("%w: %s", ErrQuoteNotRoutable, reason)
		}
		return StartBuyResult{}, err
	}
	if !quote.Routable {
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, "no route")
		return StartBuyResult{}, fmt.Errorf("%w: no route", ErrQuoteNotRoutable)
	}

	return StartBuyResult{
		OutputMint: outputMint,
		Quote:      quote,
	}, nil
}
