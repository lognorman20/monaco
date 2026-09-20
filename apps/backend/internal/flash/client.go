package flash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://flash.definitive.fi/v1"
	defaultTimeout = 15 * time.Second
	apiKeyHeader   = "x-definitive-api-key"
)

// Client quotes, submits, and tracks Definitive Flash orders.
type Client interface {
	Quote(ctx context.Context, params QuoteParams) (Quote, error)
	SubmitOrder(ctx context.Context, params SubmitOrderParams) (string, error)
	GetOrder(ctx context.Context, params GetOrderParams) (Order, error)
}

// HTTPClient calls the Flash REST API with an integrator key.
type HTTPClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewHTTPClient returns a Flash client for production use. apiKey comes from FLASH_API_KEY.
func NewHTTPClient(apiKey string) *HTTPClient {
	return NewHTTPClientWithBaseURL(defaultBaseURL, apiKey, nil)
}

// NewHTTPClientWithBaseURL injects a custom base URL and HTTP client for tests.
func NewHTTPClientWithBaseURL(baseURL, apiKey string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &HTTPClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: wrapHTTPClientForTests(httpClient),
	}
}

type orderFields struct {
	TargetChain   string `json:"targetChain"`
	ContraChain   string `json:"contraChain"`
	TargetAsset   string `json:"targetAsset"`
	ContraAsset   string `json:"contraAsset"`
	Side          string `json:"side"`
	Qty           string `json:"qty"`
	OrderType     string `json:"orderType"`
	MaxSlippage   string `json:"maxSlippage,omitempty"`
	FunderAddress string `json:"funderAddress,omitempty"`
}

type submitOrderRequest struct {
	orderFields
	QuoteID                string `json:"quoteId"`
	UserSignature          string `json:"userSignature"`
	SvmNonce               string `json:"svmNonce"`
	SvmDeadline            string `json:"svmDeadline"`
	SvmSponsoredDelegateTx string `json:"svmSponsoredDelegateTx,omitempty"`
}

type quoteResponse struct {
	QuoteID string   `json:"quoteId"`
	From    QuoteLeg `json:"from"`
	To      QuoteLeg `json:"to"`
	Svm     *struct {
		ATASetupIxs         []Instruction `json:"ataSetupIxs"`
		DelegateIx          *Instruction  `json:"delegateIx"`
		SponsoredDelegateTx string        `json:"sponsoredDelegateTx"`
		OrderMessage        string        `json:"orderMessage"`
		Nonce               string        `json:"nonce"`
		Deadline            string        `json:"deadline"`
	} `json:"svm"`
}

type submitOrderResponse struct {
	OrderID string `json:"orderId"`
}

type getOrderResponse struct {
	Order struct {
		OrderID     string `json:"orderId"`
		Status      string `json:"status"`
		CloseReason string `json:"closeReason"`
		Filled      *struct {
			TargetAmount string `json:"targetAmount"`
			ContraAmount string `json:"contraAmount"`
		} `json:"filled"`
	} `json:"order"`
	Fills []struct {
		TransactionID string `json:"transactionId"`
	} `json:"fills"`
}

func (p QuoteParams) fields() orderFields {
	return orderFields{
		TargetChain:   chainSolana,
		ContraChain:   chainSolana,
		TargetAsset:   p.TargetAsset,
		ContraAsset:   p.ContraAsset,
		Side:          p.Side,
		Qty:           p.Qty,
		OrderType:     orderTypeMarket,
		MaxSlippage:   p.MaxSlippage,
		FunderAddress: p.FunderAddress,
	}
}

// Quote requests market pricing and, when a funder is set, the Solana signing payload.
func (c *HTTPClient) Quote(ctx context.Context, params QuoteParams) (Quote, error) {
	logQuoteAttempt(params)

	if strings.TrimSpace(params.TargetAsset) == "" || strings.TrimSpace(params.ContraAsset) == "" {
		err := fmt.Errorf("%w: target and contra assets are required", ErrNoRoute)
		logQuoteResult(params, "", err)
		return Quote{}, err
	}
	if strings.TrimSpace(params.Qty) == "" {
		err := fmt.Errorf("%w: qty is required", ErrNoRoute)
		logQuoteResult(params, "", err)
		return Quote{}, err
	}

	body, err := c.do(ctx, http.MethodPost, "/quote", params.fields())
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.unknownAsset() {
			err = fmt.Errorf("%w: %s", ErrNoRoute, apiErr.Message)
		}
		logQuoteResult(params, "", err)
		return Quote{}, err
	}
	quote, err := ParseQuoteResponse(body)
	logQuoteResult(params, quote.QuoteID, err)
	return quote, err
}

// ParseQuoteResponse parses Flash /quote JSON.
func ParseQuoteResponse(body []byte) (Quote, error) {
	var raw quoteResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return Quote{}, fmt.Errorf("flash: invalid quote json: %w", err)
	}
	if strings.TrimSpace(raw.QuoteID) == "" {
		return Quote{}, fmt.Errorf("flash: quote missing quoteId")
	}
	quote := Quote{QuoteID: raw.QuoteID, From: raw.From, To: raw.To}
	if raw.Svm != nil {
		quote.OrderMessage = raw.Svm.OrderMessage
		quote.Nonce = raw.Svm.Nonce
		quote.Deadline = raw.Svm.Deadline
		quote.ATASetupIxs = raw.Svm.ATASetupIxs
		quote.DelegateIx = raw.Svm.DelegateIx
		quote.SponsoredDelegateTx = raw.Svm.SponsoredDelegateTx
	}
	return quote, nil
}

// SubmitOrder POSTs a signed quote to /order and returns the Flash order id.
func (c *HTTPClient) SubmitOrder(ctx context.Context, params SubmitOrderParams) (string, error) {
	if params.QuoteID == "" || params.UserSignature == "" || params.Nonce == "" || params.Deadline == "" {
		return "", fmt.Errorf("flash: quoteId, userSignature, nonce and deadline are required")
	}
	if strings.TrimSpace(params.Quote.FunderAddress) == "" {
		return "", fmt.Errorf("flash: funder address is required")
	}

	body, err := c.do(ctx, http.MethodPost, "/order", submitOrderRequest{
		orderFields:            params.Quote.fields(),
		QuoteID:                params.QuoteID,
		UserSignature:          params.UserSignature,
		SvmNonce:               params.Nonce,
		SvmDeadline:            params.Deadline,
		SvmSponsoredDelegateTx: params.SignedSponsoredDelegateTx,
	})
	if err != nil {
		logOrderSubmit(params.Quote, params.QuoteID, "", err)
		return "", err
	}

	var raw submitOrderResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		err = fmt.Errorf("flash: invalid order json: %w", err)
		logOrderSubmit(params.Quote, params.QuoteID, "", err)
		return "", err
	}
	if strings.TrimSpace(raw.OrderID) == "" {
		err := fmt.Errorf("flash: order response missing orderId")
		logOrderSubmit(params.Quote, params.QuoteID, "", err)
		return "", err
	}
	logOrderSubmit(params.Quote, params.QuoteID, raw.OrderID, nil)
	return raw.OrderID, nil
}

// GetOrder fetches one order with its fills.
func (c *HTTPClient) GetOrder(ctx context.Context, params GetOrderParams) (Order, error) {
	if params.OrderID == "" || params.FunderAddress == "" {
		return Order{}, fmt.Errorf("flash: orderId and funderAddress are required")
	}
	path := "/orders/" + url.PathEscape(params.OrderID) + "?funderAddress=" + url.QueryEscape(params.FunderAddress)
	body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Order{}, err
	}
	return ParseOrderResponse(body)
}

// ParseOrderResponse parses Flash GET /orders/{orderId} JSON.
func ParseOrderResponse(body []byte) (Order, error) {
	var raw getOrderResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return Order{}, fmt.Errorf("flash: invalid order detail json: %w", err)
	}
	if strings.TrimSpace(raw.Order.Status) == "" {
		return Order{}, fmt.Errorf("flash: order detail missing status")
	}
	order := Order{
		OrderID:     raw.Order.OrderID,
		Status:      raw.Order.Status,
		CloseReason: raw.Order.CloseReason,
	}
	if raw.Order.Filled != nil {
		order.FilledTargetAmount = raw.Order.Filled.TargetAmount
		order.FilledContraAmount = raw.Order.Filled.ContraAmount
	}
	// A market order settles in one transaction; the last fill carries its signature.
	for _, fill := range raw.Fills {
		if strings.TrimSpace(fill.TransactionID) != "" {
			order.TransactionID = fill.TransactionID
		}
	}
	return order, nil
}

func (c *HTTPClient) do(ctx context.Context, method, path string, payload any) ([]byte, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("%w: FLASH_API_KEY is not set", ErrUnauthorized)
	}

	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(apiKeyHeader, c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logHTTPFailure(method, path, 0, nil, err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logHTTPFailure(method, path, resp.StatusCode, body, err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := apiError(resp.StatusCode, body)
		logHTTPFailure(method, path, resp.StatusCode, body, apiErr)
		return nil, apiErr
	}
	return body, nil
}

// apiError maps a non-200 Flash response. Flash returns {"error":{"code","message"}}
// for request errors and {"error":"..."} for auth errors.
func apiError(status int, body []byte) error {
	code, message := "", strings.TrimSpace(string(body))
	var structured struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	var flat struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &structured); err == nil && structured.Error.Message != "" {
		code, message = structured.Error.Code, structured.Error.Message
	} else if err := json.Unmarshal(body, &flat); err == nil && flat.Error != "" {
		message = flat.Error
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("%w: status %d: %s", ErrUnauthorized, status, message)
	}
	return &APIError{Status: status, Code: code, Message: message}
}

// APIError is a non-200 Flash response that is not an auth failure.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("flash: status %d %s: %s", e.Status, e.Code, e.Message)
}

// unknownAsset reports Flash's 400 for a mint it does not index on Solana.
func (e *APIError) unknownAsset() bool {
	return e.Status == http.StatusBadRequest && strings.Contains(strings.ToLower(e.Message), "not found on chain")
}
