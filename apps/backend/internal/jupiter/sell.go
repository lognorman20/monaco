package jupiter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// QuoteSellParams identifies an xStock → USDC sell quote request.
type QuoteSellParams struct {
	GroupID   string
	UserID    string
	Symbol    string
	InputMint string
	Amount    int64
	Taker     string
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

	query := url.Values{}
	query.Set("inputMint", params.InputMint)
	query.Set("outputMint", USDCMint)
	query.Set("amount", strconv.FormatInt(params.Amount, 10))
	query.Set("swapMode", "ExactIn")
	query.Set("slippageBps", strconv.Itoa(defaultSlippageBps))
	if params.Taker != "" {
		query.Set("taker", params.Taker)
	}

	endpoint := c.baseURL + "/order?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return SellQuote{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, err.Error())
		return SellQuote{Routable: false, InputMint: params.InputMint, OutputMint: USDCMint}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return SellQuote{}, err
	}
	if resp.StatusCode != http.StatusOK {
		reason := fmt.Sprintf("status %d", resp.StatusCode)
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, reason)
		return SellQuote{Routable: false, InputMint: params.InputMint, OutputMint: USDCMint}, fmt.Errorf("%w: %s", ErrNoRoute, reason)
	}

	quote, err := ParseSellQuoteResponse(body)
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
func ParseSellQuoteResponse(body []byte) (SellQuote, error) {
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
		Routable:    isSellRoutable(raw),
	}
	if quote.OutputMint == "" {
		quote.OutputMint = USDCMint
	}
	if !quote.Routable {
		return quote, ErrNoRoute
	}
	return quote, nil
}

func isSellRoutable(raw orderResponse) bool {
	if strings.TrimSpace(raw.Error) != "" {
		return false
	}
	if strings.TrimSpace(raw.Transaction) == "" {
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

// RedeemSliceSellAmount returns proportional xStock atomics to sell for a redeem slice.
func RedeemSliceSellAmount(holdingAtomics, sharesRedeemedMicros, totalSharesMicros int64) int64 {
	if holdingAtomics <= 0 || sharesRedeemedMicros <= 0 || totalSharesMicros <= 0 {
		return 0
	}
	return (holdingAtomics * sharesRedeemedMicros) / totalSharesMicros
}
