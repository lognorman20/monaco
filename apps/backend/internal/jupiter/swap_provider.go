package jupiter

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

func swapSlippageBps(req swapprovider.Request) int {
	if req.Kind == swapprovider.AssetKindPreIPO {
		return PreIPOSlippageBps
	}
	return defaultSlippageBps
}

// SlippageBpsForRequest exposes order slippage for tests.
func SlippageBpsForRequest(req swapprovider.Request) int {
	return swapSlippageBps(req)
}

// SwapProvider adapts the Jupiter order → sign → execute → poll flow to swapprovider.Provider.
type SwapProvider struct {
	client Client
	signer swapprovider.TransactionSigner
}

// NewSwapProvider wraps a Jupiter client. signer must add every required signature
// (relayer fee payer and treasury) to the unsigned transaction Jupiter returns.
func NewSwapProvider(client Client, signer swapprovider.TransactionSigner) *SwapProvider {
	return &SwapProvider{client: client, signer: signer}
}

// Name identifies the provider in logs.
func (p *SwapProvider) Name() string {
	return swapprovider.NameJupiter
}

// SubmitBuy fetches a buy order for the treasury taker, signs it, and submits it to /execute.
func (p *SwapProvider) SubmitBuy(ctx context.Context, req swapprovider.Request) (swapprovider.Submission, error) {
	order, err := p.client.OrderBuy(ctx, OrderBuyParams{
		GroupID:     req.GroupID,
		UserID:      req.UserID,
		Symbol:      req.Symbol,
		OutputMint:  req.OutputMint,
		Amount:      req.Amount,
		Taker:       req.Wallet.SolanaAddress,
		SlippageBps: swapSlippageBps(req),
	})
	if err != nil {
		return swapprovider.Submission{}, swapprovider.AtStage("order_buy", "", err)
	}

	signedTx, err := p.signer.SignTreasuryTransaction(ctx, req.Wallet.PrivyWalletID, order.Transaction)
	if err != nil {
		return swapprovider.Submission{}, swapprovider.AtStage("sign_treasury", order.RequestID, err)
	}

	_, err = p.client.ExecuteBuy(ctx, ExecuteBuyParams{
		GroupID:           req.GroupID,
		UserID:            req.UserID,
		Symbol:            req.Symbol,
		RequestID:         order.RequestID,
		SignedTransaction: signedTx,
	})
	if err != nil {
		return swapprovider.Submission{}, swapprovider.AtStage("execute_submit", order.RequestID, err)
	}
	quotedOut, _ := parseFillAmount(order.OutAmount)
	return swapprovider.Submission{
		RequestID:          order.RequestID,
		Receipt:            signedTx,
		Request:            req,
		QuotedOutputAmount: quotedOut,
	}, nil
}

// SubmitSell quotes an xStock → USDC sell for the treasury taker, signs it, and submits it.
func (p *SwapProvider) SubmitSell(ctx context.Context, req swapprovider.Request) (swapprovider.Submission, error) {
	quote, err := p.client.QuoteSell(ctx, QuoteSellParams{
		GroupID:     req.GroupID,
		UserID:      req.UserID,
		Symbol:      req.Symbol,
		InputMint:   req.InputMint,
		Amount:      req.Amount,
		Taker:       req.Wallet.SolanaAddress,
		SlippageBps: swapSlippageBps(req),
	})
	if err != nil {
		if errors.Is(err, ErrNoRoute) || errors.Is(err, ErrBelowMinimumSize) {
			return swapprovider.Submission{}, fmt.Errorf("%w: %w", swapprovider.ErrNotRoutable, err)
		}
		return swapprovider.Submission{}, swapprovider.AtStage("quote_sell", "", err)
	}
	if !quote.Routable {
		return swapprovider.Submission{}, fmt.Errorf("%w: no route", swapprovider.ErrNotRoutable)
	}

	signedTx, err := p.signer.SignTreasuryTransaction(ctx, req.Wallet.PrivyWalletID, quote.Transaction)
	if err != nil {
		return swapprovider.Submission{}, swapprovider.AtStage("sign_treasury", quote.RequestID, err)
	}

	_, err = p.client.SellToUSDC(ctx, SellToUSDCParams{
		GroupID:           req.GroupID,
		UserID:            req.UserID,
		Symbol:            req.Symbol,
		RequestID:         quote.RequestID,
		SignedTransaction: signedTx,
		InputMint:         req.InputMint,
		OutputMint:        USDCMint,
		Amount:            req.Amount,
	})
	if err != nil {
		return swapprovider.Submission{}, swapprovider.AtStage("execute_submit", quote.RequestID, err)
	}
	quotedIn, _ := parseFillAmount(quote.InAmount)
	return swapprovider.Submission{
		RequestID:         quote.RequestID,
		Receipt:           signedTx,
		Request:           req,
		QuotedInputAmount: quotedIn,
	}, nil
}

// AwaitFill re-submits /execute until Jupiter confirms the swap or reports a terminal failure.
func (p *SwapProvider) AwaitFill(ctx context.Context, sub swapprovider.Submission, cfg swapprovider.PollConfig) (swapprovider.Fill, error) {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultPollConfig()
	}
	side := string(sub.Request.Side)

	result, err := PollUntilConfirmed(ctx, p.client, PollExecuteParams{
		GroupID:           sub.Request.GroupID,
		UserID:            sub.Request.UserID,
		Symbol:            sub.Request.Symbol,
		RequestID:         sub.RequestID,
		SignedTransaction: sub.Receipt,
	}, cfg)
	fill := swapprovider.Fill{Signature: result.Signature, Status: result.Status, Code: result.Code}
	if err != nil {
		return fill, err
	}
	if !result.IsConfirmedSuccess() {
		return fill, fmt.Errorf("jupiter %s not confirmed: status=%s code=%d", side, result.Status, result.Code)
	}
	fill.Confirmed = true
	fill.QuotedOutputAmount = sub.QuotedOutputAmount
	fill.QuotedInputAmount = sub.QuotedInputAmount

	fill.OutputAmount, err = parseFillAmount(result.OutputAmountResult)
	if err != nil {
		return fill, err
	}
	// Sells only persist USDC proceeds; buys need the spent USDC for cost basis.
	if sub.Request.Side == swapprovider.SideBuy {
		fill.InputAmount, err = parseFillAmount(result.InputAmountResult)
		if err != nil {
			return fill, err
		}
	}
	return fill, nil
}

func parseFillAmount(raw string) (int64, error) {
	if raw == "" {
		return 0, fmt.Errorf("missing fill amount")
	}
	amount, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid fill amount %q: %w", raw, err)
	}
	if amount <= 0 {
		return 0, fmt.Errorf("fill amount must be positive")
	}
	return amount, nil
}
