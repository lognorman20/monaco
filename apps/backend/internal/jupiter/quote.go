package jupiter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// USDCMint is the Solana mainnet USDC mint used as buy input.
	USDCMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	defaultBaseURL     = "https://api.jup.ag/swap/v2"
	defaultTimeout     = 15 * time.Second
	defaultSlippageBps = 50
)

// ErrNoRoute means Jupiter returned no routable path for the requested swap.
var ErrNoRoute = errors.New("jupiter: no route")

// ErrInsufficientFunds means the taker cannot fund the requested swap amount.
var ErrInsufficientFunds = errors.New("jupiter: insufficient funds")

// ErrInsufficientSOL means the taker or payer lacks SOL for fees or ATA rent.
var ErrInsufficientSOL = errors.New("jupiter: insufficient SOL for gas")

// ErrBelowMinimumSize means the swap is below Jupiter's executable minimum.
var ErrBelowMinimumSize = errors.New("jupiter: below minimum swap size")

// Client quotes and executes Jupiter Swap API v2 swaps.
type Client interface {
	QuoteBuy(ctx context.Context, params QuoteBuyParams) (BuyQuote, error)
	OrderBuy(ctx context.Context, params OrderBuyParams) (BuyOrder, error)
	ExecuteBuy(ctx context.Context, params ExecuteBuyParams) (ExecuteResult, error)
	PollExecute(ctx context.Context, params PollExecuteParams) (ExecuteResult, error)
	QuoteSell(ctx context.Context, params QuoteSellParams) (SellQuote, error)
	SellToUSDC(ctx context.Context, params SellToUSDCParams) (ExecuteResult, error)
}

// QuoteBuyParams identifies a USDC → xStock quote request.
type QuoteBuyParams struct {
	GroupID    string
	UserID     string
	Symbol     string
	OutputMint string
	USDCAmount int64
	// Taker is the treasury wallet that will sign the swap. When set, routability
	// requires a buildable unsigned transaction (same constraints as OrderBuy).
	Taker string
}

// BuyQuote is a parsed Jupiter buy quote response.
type BuyQuote struct {
	Routable   bool
	InputMint  string
	OutputMint string
	InAmount   string
	OutAmount  string
	RequestID  string
}

// HTTPClient calls Jupiter Swap API v2 on Solana mainnet.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
	payer      string
}

// NewHTTPClient returns a Jupiter v2 client using the production API.
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		baseURL: defaultBaseURL,
		httpClient: wrapHTTPClientForTests(&http.Client{
			Timeout: defaultTimeout,
		}),
	}
}

// NewHTTPClientWithPayer returns a production client that sponsors gas via payer.
// Jupiter requires payer != taker; treasury swaps need this when treasuries hold 0 SOL.
func NewHTTPClientWithPayer(payer string) *HTTPClient {
	client := NewHTTPClient()
	client.payer = strings.TrimSpace(payer)
	return client
}

// NewHTTPClientWithBaseURL injects a custom base URL and HTTP client for tests.
func NewHTTPClientWithBaseURL(baseURL string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &HTTPClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: wrapHTTPClientForTests(httpClient),
	}
}

type quoteResponse struct {
	InputMint    string          `json:"inputMint"`
	OutputMint   string          `json:"outputMint"`
	InAmount     string          `json:"inAmount"`
	OutAmount    string          `json:"outAmount"`
	Transaction  string          `json:"transaction"`
	RoutePlan    json.RawMessage `json:"routePlan"`
	RequestID    string          `json:"requestId"`
	Router       string          `json:"router"`
	ErrorCode    float64         `json:"errorCode"`
	ErrorMessage string          `json:"errorMessage"`
	Error        string          `json:"error"`
}

// QuoteBuy requests a USDC inputMint quote for an xStock outputMint.
func (c *HTTPClient) QuoteBuy(ctx context.Context, params QuoteBuyParams) (BuyQuote, error) {
	logQuoteAttempt(params.GroupID, params.UserID, params.Symbol, params.USDCAmount)

	if params.USDCAmount <= 0 {
		reason := "usdc amount must be positive"
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, reason)
		return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, fmt.Errorf("%w: %s", ErrNoRoute, reason)
	}
	if strings.TrimSpace(params.OutputMint) == "" {
		reason := "output mint is required"
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, reason)
		return BuyQuote{Routable: false, InputMint: USDCMint}, fmt.Errorf("%w: %s", ErrNoRoute, reason)
	}

	body, err := c.fetchBuyOrder(ctx, buyOrderRequest{
		InputMint:  USDCMint,
		OutputMint: params.OutputMint,
		Amount:     params.USDCAmount,
		Taker:      params.Taker,
	}, params.GroupID, params.UserID, params.Symbol)
	if err != nil {
		reason := err.Error()
		if strings.Contains(reason, "order status") {
			reason = fmt.Sprintf("status %s", strings.TrimPrefix(reason, "jupiter: order status "))
		}
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, reason)
		return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, fmt.Errorf("%w: %s", ErrNoRoute, reason)
	}

	quote, err := ParseBuyQuoteResponse(body, params.Taker != "")
	if err != nil {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, err.Error())
		return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, err
	}
	if !quote.Routable {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, "no route")
	} else {
		logQuoteSuccess(params.GroupID, params.UserID, params.Symbol, quote.RequestID, true)
	}
	return quote, nil
}

// ParseBuyQuoteResponse parses Jupiter v2 order/quote JSON.
// requireTransaction mirrors Jupiter /order with taker: price-only quotes omit
// transaction; executable quotes must include a buildable unsigned transaction.
func ParseBuyQuoteResponse(body []byte, requireTransaction bool) (BuyQuote, error) {
	var raw quoteResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return BuyQuote{}, fmt.Errorf("jupiter: invalid quote json: %w", err)
	}

	quote := BuyQuote{
		InputMint:  raw.InputMint,
		OutputMint: raw.OutputMint,
		InAmount:   raw.InAmount,
		OutAmount:  raw.OutAmount,
		RequestID:  raw.RequestID,
		Routable:   isRoutableBuyQuote(raw, requireTransaction),
	}
	if quote.InputMint == "" {
		quote.InputMint = USDCMint
	}
	if !quote.Routable {
		return quote, ErrNoRoute
	}
	return quote, nil
}

func isRoutableBuyQuote(raw quoteResponse, requireTransaction bool) bool {
	if raw.ErrorCode != 0 || strings.TrimSpace(raw.Error) != "" || strings.TrimSpace(raw.ErrorMessage) != "" {
		return false
	}
	if requireTransaction && strings.TrimSpace(raw.Transaction) == "" {
		return false
	}
	if len(raw.RoutePlan) == 0 || string(raw.RoutePlan) == "null" {
		return false
	}
	var steps []json.RawMessage
	if err := json.Unmarshal(raw.RoutePlan, &steps); err != nil || len(steps) == 0 {
		return false
	}
	outAmount := strings.TrimSpace(raw.OutAmount)
	if outAmount == "" || outAmount == "0" {
		return false
	}
	return true
}

type buyOrderRequest struct {
	InputMint  string
	OutputMint string
	Amount     int64
	Taker      string
}

func (c *HTTPClient) fetchBuyOrder(ctx context.Context, req buyOrderRequest, groupID, userID, symbol string) ([]byte, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("jupiter: amount must be positive")
	}
	if strings.TrimSpace(req.OutputMint) == "" {
		return nil, fmt.Errorf("jupiter: output mint is required")
	}
	inputMint := req.InputMint
	if inputMint == "" {
		inputMint = USDCMint
	}

	query := url.Values{}
	query.Set("inputMint", inputMint)
	query.Set("outputMint", req.OutputMint)
	query.Set("amount", strconv.FormatInt(req.Amount, 10))
	query.Set("swapMode", "ExactIn")
	query.Set("slippageBps", strconv.Itoa(defaultSlippageBps))
	taker := strings.TrimSpace(req.Taker)
	if taker != "" {
		query.Set("taker", taker)
	}
	if payer := orderPayer(c.payer, taker); payer != "" {
		query.Set("payer", payer)
	}

	endpoint := c.baseURL + "/order?" + query.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		logOrderHTTPFailure(groupID, userID, symbol, req, c.payer, 0, nil, err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logOrderHTTPFailure(groupID, userID, symbol, req, c.payer, resp.StatusCode, body, err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		if routedErr := orderResponseBuildError(body); routedErr != nil {
			logOrderHTTPFailure(groupID, userID, symbol, req, c.payer, resp.StatusCode, body, routedErr)
			return nil, routedErr
		}
		apiErr := fmt.Errorf("jupiter: order status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		logOrderHTTPFailure(groupID, userID, symbol, req, c.payer, resp.StatusCode, body, apiErr)
		return nil, apiErr
	}
	if err := orderResponseBuildError(body); err != nil {
		logOrderHTTPFailure(groupID, userID, symbol, req, c.payer, resp.StatusCode, body, err)
		return nil, err
	}
	return body, nil
}

func orderPayer(configuredPayer, taker string) string {
	payer := strings.TrimSpace(configuredPayer)
	if payer == "" || payer == strings.TrimSpace(taker) {
		return ""
	}
	return payer
}

func orderResponseBuildError(body []byte) error {
	var raw quoteResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	if strings.TrimSpace(raw.Transaction) != "" {
		return nil
	}
	if raw.ErrorCode == 0 && strings.TrimSpace(raw.Error) == "" && strings.TrimSpace(raw.ErrorMessage) == "" {
		return nil
	}
	return orderBuildError(raw.Router, raw.ErrorCode, raw.ErrorMessage, raw.Error)
}

func orderBuildError(router string, errorCode float64, errorMessage, errText string) error {
	msg := strings.TrimSpace(errorMessage)
	if msg == "" {
		msg = strings.TrimSpace(errText)
	}
	if msg == "" {
		msg = "order could not be built"
	}
	if router != "" {
		msg = fmt.Sprintf("[%s] %s", router, msg)
	}
	switch int(errorCode) {
	case 1:
		return fmt.Errorf("%w: %s", ErrInsufficientFunds, msg)
	case 2:
		return fmt.Errorf("%w: %s", ErrInsufficientSOL, msg)
	case 3:
		return fmt.Errorf("%w: %s", ErrBelowMinimumSize, msg)
	default:
		if strings.Contains(strings.ToLower(msg), "failed to get quotes") {
			return fmt.Errorf("%w: %s (treasury may need SOL or integrator payer)", ErrNoRoute, msg)
		}
		return fmt.Errorf("%w: %s", ErrNoRoute, msg)
	}
}
