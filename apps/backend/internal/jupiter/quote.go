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

// Client quotes and executes Jupiter Swap API v2 swaps.
type Client interface {
	QuoteBuy(ctx context.Context, params QuoteBuyParams) (BuyQuote, error)
}

// QuoteBuyParams identifies a USDC → xStock quote request.
type QuoteBuyParams struct {
	GroupID    string
	UserID     string
	Symbol     string
	OutputMint string
	USDCAmount int64
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
}

// NewHTTPClient returns a Jupiter v2 client using the production API.
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// NewHTTPClientWithBaseURL injects a custom base URL and HTTP client for tests.
func NewHTTPClientWithBaseURL(baseURL string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &HTTPClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

type quoteResponse struct {
	InputMint  string          `json:"inputMint"`
	OutputMint string          `json:"outputMint"`
	InAmount   string          `json:"inAmount"`
	OutAmount  string          `json:"outAmount"`
	RoutePlan  json.RawMessage `json:"routePlan"`
	RequestID  string          `json:"requestId"`
	Error      string          `json:"error"`
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

	query := url.Values{}
	query.Set("inputMint", USDCMint)
	query.Set("outputMint", params.OutputMint)
	query.Set("amount", strconv.FormatInt(params.USDCAmount, 10))
	query.Set("swapMode", "ExactIn")
	query.Set("slippageBps", strconv.Itoa(defaultSlippageBps))

	endpoint := c.baseURL + "/order?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return BuyQuote{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, err.Error())
		return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return BuyQuote{}, err
	}

	if resp.StatusCode != http.StatusOK {
		reason := fmt.Sprintf("status %d", resp.StatusCode)
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, reason)
		return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, fmt.Errorf("%w: %s", ErrNoRoute, reason)
	}

	quote, err := ParseBuyQuoteResponse(body)
	if err != nil {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, err.Error())
		return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, err
	}
	if !quote.Routable {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, "no route")
	}
	return quote, nil
}

// ParseBuyQuoteResponse parses Jupiter v2 order/quote JSON.
func ParseBuyQuoteResponse(body []byte) (BuyQuote, error) {
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
		Routable:   isRoutableQuote(raw),
	}
	if quote.InputMint == "" {
		quote.InputMint = USDCMint
	}
	if !quote.Routable {
		return quote, ErrNoRoute
	}
	return quote, nil
}

func isRoutableQuote(raw quoteResponse) bool {
	if strings.TrimSpace(raw.Error) != "" {
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
