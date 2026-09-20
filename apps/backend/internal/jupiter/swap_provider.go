package jupiter

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/monaco/monaco/apps/backend/internal/solana/txsign"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

// SwapProvider adapts the Jupiter order → sign → execute → poll flow to swapprovider.Provider.
type SwapProvider struct {
	client Client
	signer swapprovider.TransactionSigner
	chain  swapprovider.ChainReader
}

// NewSwapProvider wraps a Jupiter client. signer must add every required signature
// (relayer fee payer and treasury) to the unsigned transaction Jupiter returns.
func NewSwapProvider(client Client, signer swapprovider.TransactionSigner) *SwapProvider {
	return &SwapProvider{client: client, signer: signer}
}

// SetChainReader wires the Solana reader Resolve uses. Without it Resolve returns an error
// and unresolved swaps stay pending.
func (p *SwapProvider) SetChainReader(chain swapprovider.ChainReader) {
	p.chain = chain
}

// Name identifies the provider in logs.
func (p *SwapProvider) Name() string {
	return swapprovider.NameJupiter
}

// PrepareBuy fetches a buy order for the treasury taker and signs it. Nothing is sent.
func (p *SwapProvider) PrepareBuy(ctx context.Context, req swapprovider.Request) (swapprovider.Prepared, error) {
	req.Side = swapprovider.SideBuy
	order, err := p.client.OrderBuy(ctx, OrderBuyParams{
		GroupID:    req.GroupID,
		UserID:     req.UserID,
		Symbol:     req.Symbol,
		OutputMint: req.OutputMint,
		Amount:     req.Amount,
		Taker:      req.Wallet.SolanaAddress,
	})
	if err != nil {
		return swapprovider.Prepared{}, swapprovider.AtStage("order_buy", "", err)
	}
	return p.sign(ctx, req, order.RequestID, order.Transaction)
}

// PrepareSell quotes an xStock → USDC sell for the treasury taker and signs it. Nothing is sent.
func (p *SwapProvider) PrepareSell(ctx context.Context, req swapprovider.Request) (swapprovider.Prepared, error) {
	req.Side = swapprovider.SideSell
	quote, err := p.client.QuoteSell(ctx, QuoteSellParams{
		GroupID:   req.GroupID,
		UserID:    req.UserID,
		Symbol:    req.Symbol,
		InputMint: req.InputMint,
		Amount:    req.Amount,
		Taker:     req.Wallet.SolanaAddress,
	})
	if err != nil {
		if errors.Is(err, ErrNoRoute) || errors.Is(err, ErrBelowMinimumSize) {
			return swapprovider.Prepared{}, fmt.Errorf("%w: %w", swapprovider.ErrNotRoutable, err)
		}
		return swapprovider.Prepared{}, swapprovider.AtStage("quote_sell", "", err)
	}
	if !quote.Routable {
		return swapprovider.Prepared{}, fmt.Errorf("%w: no route", swapprovider.ErrNotRoutable)
	}
	return p.sign(ctx, req, quote.RequestID, quote.Transaction)
}

// sign adds the treasury signatures and reads the transaction id and blockhash off the signed
// bytes. A transaction whose id cannot be read is never submitted: it could not be reconciled.
func (p *SwapProvider) sign(ctx context.Context, req swapprovider.Request, requestID, unsignedTx string) (swapprovider.Prepared, error) {
	signedTx, err := p.signer.SignTreasuryTransaction(ctx, req.Wallet.PrivyWalletID, unsignedTx)
	if err != nil {
		return swapprovider.Prepared{}, swapprovider.AtStage("sign_treasury", requestID, err)
	}
	signature, blockhash, err := txsign.SignatureAndBlockhash(signedTx)
	if err != nil {
		return swapprovider.Prepared{}, swapprovider.AtStage("read_signed_tx", requestID, err)
	}
	return swapprovider.Prepared{
		RequestID:   requestID,
		TxSignature: signature,
		Blockhash:   blockhash,
		Payload:     signedTx,
		Request:     req,
	}, nil
}

// Submit posts the signed transaction to /execute. Jupiter may broadcast it even when the
// call errors, so no Submit error is treated as definitive; the chain decides in Resolve.
func (p *SwapProvider) Submit(ctx context.Context, prepared swapprovider.Prepared) (swapprovider.Submission, error) {
	req := prepared.Request
	var err error
	if req.Side == swapprovider.SideSell {
		_, err = p.client.SellToUSDC(ctx, SellToUSDCParams{
			GroupID:           req.GroupID,
			UserID:            req.UserID,
			Symbol:            req.Symbol,
			RequestID:         prepared.RequestID,
			SignedTransaction: prepared.Payload,
			InputMint:         req.InputMint,
			OutputMint:        USDCMint,
			Amount:            req.Amount,
		})
	} else {
		_, err = p.client.ExecuteBuy(ctx, ExecuteBuyParams{
			GroupID:           req.GroupID,
			UserID:            req.UserID,
			Symbol:            req.Symbol,
			RequestID:         prepared.RequestID,
			SignedTransaction: prepared.Payload,
		})
	}
	if err != nil {
		return swapprovider.Submission{}, swapprovider.AtStage("execute_submit", prepared.RequestID, err)
	}
	return swapprovider.Submission{RequestID: prepared.RequestID, Receipt: prepared.Payload, Request: req}, nil
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

// Resolve decides a swap from Solana itself. Jupiter only answers for a requestId for a short
// while and reports timeouts as failures, so neither its errors nor its Failed status prove the
// transaction did not land. A swap is failed only when the chain shows it errored, or when its
// blockhash expired as of the finalized bank and the signature still cannot be found.
func (p *SwapProvider) Resolve(ctx context.Context, pending swapprovider.PendingSwap) (swapprovider.Resolution, error) {
	if p.chain == nil {
		return swapprovider.Resolution{}, fmt.Errorf("jupiter: chain reader is not configured")
	}
	if pending.TxSignature == "" || pending.Blockhash == "" {
		return swapprovider.Resolution{Outcome: swapprovider.OutcomeUnknown, Reason: "no signed transaction recorded"}, nil
	}

	status, err := p.chain.SignatureStatus(ctx, pending.TxSignature)
	if err != nil {
		return swapprovider.Resolution{}, err
	}
	if !status.Found {
		valid, err := p.chain.IsBlockhashValid(ctx, pending.Blockhash)
		if err != nil {
			return swapprovider.Resolution{}, err
		}
		if valid {
			return swapprovider.Resolution{Outcome: swapprovider.OutcomeUnknown, Reason: "not landed; blockhash still valid"}, nil
		}
		// The transaction can land right up to expiry, so look again now that it cannot.
		status, err = p.chain.SignatureStatus(ctx, pending.TxSignature)
		if err != nil {
			return swapprovider.Resolution{}, err
		}
		if !status.Found {
			return swapprovider.Resolution{Outcome: swapprovider.OutcomeFailed, Reason: "blockhash expired and signature not found"}, nil
		}
	}
	if !status.Finalized {
		return swapprovider.Resolution{Outcome: swapprovider.OutcomeUnknown, Reason: "landed; awaiting finality"}, nil
	}
	if status.Failed {
		return swapprovider.Resolution{Outcome: swapprovider.OutcomeFailed, Reason: "transaction failed on chain: " + status.Err}, nil
	}

	req := pending.Request
	changes, err := p.chain.TokenBalanceChanges(ctx, pending.TxSignature, req.Wallet.SolanaAddress)
	if err != nil {
		return swapprovider.Resolution{}, err
	}
	fill := swapprovider.Fill{
		Confirmed:    true,
		Signature:    pending.TxSignature,
		Status:       ExecuteStatusSuccess,
		InputAmount:  -changes[req.InputMint],
		OutputAmount: changes[req.OutputMint],
	}
	if fill.InputAmount <= 0 || fill.OutputAmount <= 0 {
		return swapprovider.Resolution{}, fmt.Errorf("jupiter: finalized swap %s moved input=%d output=%d, want both positive",
			pending.TxSignature, fill.InputAmount, fill.OutputAmount)
	}
	return swapprovider.Resolution{Outcome: swapprovider.OutcomeFilled, Fill: fill}, nil
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
