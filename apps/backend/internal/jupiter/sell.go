package jupiter

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// QuoteSellParams identifies an xStock → USDC sell quote request.
type QuoteSellParams struct {
	GroupID     string
	UserID      string
	Symbol      string
	InputMint   string
	Amount      int64
	Taker       string
	SlippageBps int
}

// SellToUSDCParams executes a signed treasury sell to USDC.
type SellToUSDCParams struct {
	GroupID           string
	UserID            string
	Symbol            string
	RequestID         string
	SignedTransaction string
	InputMint         string
	OutputMint        string
	Amount            int64
}

// QuoteSell requests an xStock inputMint quote for USDC outputMint.
func (c *HTTPClient) QuoteSell(ctx context.Context, params QuoteSellParams) (SellQuote, error) {
	logQuoteAttempt(params.GroupID, params.UserID, params.Symbol, params.Amount)

	if params.Amount <= 0 {
		reason := "sell amount must be positive"
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, reason)
		return SellQuote{Routable: false, InputMint: params.InputMint, OutputMint: USDCMint}, fmt.Errorf("%w: %s", ErrNoRoute, reason)
	}
	if strings.TrimSpace(params.InputMint) == "" {
		reason := "input mint is required"
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, reason)
		return SellQuote{Routable: false, OutputMint: USDCMint}, fmt.Errorf("%w: %s", ErrNoRoute, reason)
	}

	body, err := c.fetchBuyOrder(ctx, buyOrderRequest{
		InputMint:   params.InputMint,
		OutputMint:  USDCMint,
		Amount:      params.Amount,
		Taker:       params.Taker,
		SlippageBps: params.SlippageBps,
	}, params.GroupID, params.UserID, params.Symbol)
	if err != nil {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, err.Error())
		return SellQuote{Routable: false, InputMint: params.InputMint, OutputMint: USDCMint}, err
	}

	quote, err := ParseSellQuoteResponse(body, params.Taker != "")
	if err != nil {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, err.Error())
		return SellQuote{Routable: false, InputMint: params.InputMint, OutputMint: USDCMint}, err
	}
	if !quote.Routable {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, "no route")
	} else {
		logQuoteSuccess(params.GroupID, params.UserID, params.Symbol, quote.RequestID, true)
	}
	return quote, nil
}

// ParseSellQuoteResponse parses Jupiter v2 sell order JSON.
// requireTransaction mirrors ParseBuyQuoteResponse: price-only quotes omit
// transaction when no taker is sent; executable quotes require it.
func ParseSellQuoteResponse(body []byte, requireTransaction bool) (SellQuote, error) {
	var raw orderResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return SellQuote{}, fmt.Errorf("jupiter: invalid sell quote json: %w", err)
	}

	quote := SellQuote{
		InputMint:   raw.InputMint,
		OutputMint:  raw.OutputMint,
		InAmount:    raw.InAmount,
		OutAmount:   raw.OutAmount,
		RequestID:   raw.RequestID,
		Transaction: raw.Transaction,
		Routable:    isSellRoutable(raw, requireTransaction),
	}
	if quote.OutputMint == "" {
		quote.OutputMint = USDCMint
	}
	if !quote.Routable {
		return quote, ErrNoRoute
	}
	return quote, nil
}

func isSellRoutable(raw orderResponse, requireTransaction bool) bool {
	if raw.ErrorCode != 0 || strings.TrimSpace(raw.Error) != "" || strings.TrimSpace(raw.ErrorMessage) != "" {
		return false
	}
	if requireTransaction && strings.TrimSpace(raw.Transaction) == "" {
		return false
	}
	outAmount := strings.TrimSpace(raw.OutAmount)
	if outAmount == "" || outAmount == "0" {
		return false
	}
	return true
}

// SellToUSDC POSTs a signed treasury sell to Jupiter /execute.
func (c *HTTPClient) SellToUSDC(ctx context.Context, params SellToUSDCParams) (ExecuteResult, error) {
	logExecuteSubmit(params.GroupID, params.UserID, params.Symbol, "", params.RequestID)
	return c.postExecute(ctx, params.GroupID, params.UserID, params.Symbol, params.RequestID, params.SignedTransaction)
}

// RedeemSellSlippageBufferBps is the extra size added to a redeem sell so Jupiter slippage and
// fees do not leave the payout a few micros short of what the member is owed.
const RedeemSellSlippageBufferBps = 100

// RedeemShortfallSellAmount returns the xStock atomics to sell so a redeem payout can be funded
// in USDC. It sizes the sale to the treasury's cash shortfall against the marked value of the
// holdings, not to the member's whole slice: a job that already sold once then raises only what
// is still missing instead of selling the slice twice. The result is capped at the whole holding.
func RedeemShortfallSellAmount(holdingAtomics, shortfallUsdc, stockValueUsdc, bufferBps int64) int64 {
	if holdingAtomics <= 0 || shortfallUsdc <= 0 || stockValueUsdc <= 0 {
		return 0
	}

	target := new(big.Int).SetInt64(shortfallUsdc)
	buffer := new(big.Int).Mul(target, big.NewInt(bufferBps))
	buffer.Div(buffer, big.NewInt(10_000))
	target.Add(target, buffer)
	target.Add(target, big.NewInt(1))

	if target.Cmp(big.NewInt(stockValueUsdc)) >= 0 {
		return holdingAtomics
	}

	// Round up so rounding never leaves the payout short.
	amount := new(big.Int).Mul(big.NewInt(holdingAtomics), target)
	amount.Add(amount, big.NewInt(stockValueUsdc-1))
	amount.Div(amount, big.NewInt(stockValueUsdc))
	if !amount.IsInt64() || amount.Int64() > holdingAtomics {
		return holdingAtomics
	}
	return amount.Int64()
}
