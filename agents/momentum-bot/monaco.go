package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
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

const agentKeyHeader = "X-Monaco-Agent-Key"

// Errors the bot reacts to differently. Anything else is a transient failure.
var (
	// ErrBadKey is a 401: the key is unknown or revoked.
	// Retrying only feeds the server's wrong-key throttle, so the bot stops.
	ErrBadKey = errors.New("agent key rejected")
	// ErrPaused is a 403: the cabal voted to pause the agent. The key still works,
	// so the bot waits for a resume vote instead of exiting.
	ErrPaused = errors.New("agent is paused")
)

// RejectedError is a 422: the server understood the intent and refused it
// (over budget, treasury short of USDC, selling more than the cabal holds). Reason is the
// body's rejectReason. IntentID is the rejected intent in the cabal's audit trail; it is
// empty when Monaco refused the intent before recording it.
type RejectedError struct {
	Reason   string
	IntentID string
}

func (e *RejectedError) Error() string {
	if e.IntentID == "" {
		return "intent rejected: " + e.Reason
	}
	return fmt.Sprintf("intent %s rejected: %s", e.IntentID, e.Reason)
}

// ThrottledError is a 429 with the server's Retry-After.
type ThrottledError struct{ RetryAfter time.Duration }

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("throttled, retry after %s", e.RetryAfter)
}

// StatusError is any other non-2xx response. IntentID and IntentStatus are set when the
// body names the intent: "failed" on a 5xx whose swap failed, "accepted" on the 409 for a
// resend that arrived while the first request was still executing.
type StatusError struct {
	Status       int
	Message      string
	IntentID     string
	IntentStatus string
}

func (e *StatusError) Error() string {
	if e.IntentID == "" {
		return fmt.Sprintf("monaco api status %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("monaco api status %d: %s (intent %s %s)", e.Status, e.Message, e.IntentID, e.IntentStatus)
}

// Asset is one tradable xStock from the cabal's catalog. MarkUsdcMicros is Monaco's
// current price for one share, or nil when Monaco has no mark for it right now.
type Asset struct {
	Symbol         string `json:"symbol"`
	Name           string `json:"name"`
	SolanaMint     string `json:"solanaMint"`
	Routable       bool   `json:"routable"`
	MarkUsdcMicros *int64 `json:"markUsdcMicros"`
}

// Intent is the body of POST /v1/agent/intents. Buys carry usdcMicros (USD x 1e6);
// sells carry tokenAmount (shares x 1e8).
type Intent struct {
	Side        string `json:"side"`
	Symbol      string `json:"symbol"`
	UsdcMicros  int64  `json:"usdcMicros,omitempty"`
	TokenAmount int64  `json:"tokenAmount,omitempty"`
	// IdempotencyKey makes a resend safe: the server answers a key it has already seen
	// with the first intent's outcome instead of trading again.
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	// Reason is why the bot made the trade; the cabal sees it next to the trade.
	Reason string `json:"reason,omitempty"`
}

// IntentResult is the server's 2xx answer. An intent that was refused or failed comes back
// as a non-2xx with the same intentId, status and rejectReason next to the error message.
type IntentResult struct {
	IntentID      string `json:"intentId"`
	Status        string `json:"status"`
	TransactionID string `json:"transactionId"`
	RejectReason  string `json:"rejectReason"`
}

// IntentRecord is GET /v1/agent/intents/{id}: where an intent ended up.
// FilledTokenAmount is set once a swap executed.
type IntentRecord struct {
	IntentID          string `json:"intentId"`
	Status            string `json:"status"`
	RejectReason      string `json:"rejectReason"`
	TransactionID     string `json:"transactionId"`
	FilledTokenAmount *int64 `json:"filledTokenAmount"`
}

// AgentInfo is the part of GET /v1/agent the bot shows at startup.
type AgentInfo struct {
	CabalName string `json:"cabalName"`
	AgentName string `json:"agentName"`
	Status    string `json:"status"`
	Budget    struct {
		AllocationUsd string `json:"allocationUsd"`
		AvailableUsd  string `json:"availableUsd"`
	} `json:"budget"`
}

// MonacoClient talks to Monaco's agent endpoints. The key alone names the agent and its
// cabal, and it only ever leaves this struct as a request header.
type MonacoClient struct {
	baseURL string
	key     string
	http    *http.Client
}

// NewMonacoClient returns a client for the agent the key belongs to. httpClient may be nil.
func NewMonacoClient(baseURL, key string, httpClient *http.Client) *MonacoClient {
	if httpClient == nil {
		// Intents settle an on-chain swap before answering, so this is generous.
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	return &MonacoClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		key:     key,
		http:    httpClient,
	}
}

// Agent returns the agent's cabal, status and budget.
func (c *MonacoClient) Agent(ctx context.Context) (AgentInfo, error) {
	var out AgentInfo
	err := c.get(ctx, "/v1/agent", &out)
	return out, err
}

// Assets returns one page of the cabal's tradable catalog. A ticker query
// ("GOOGLx") resolves to that single asset.
func (c *MonacoClient) Assets(ctx context.Context, query string, limit int) ([]Asset, error) {
	page, err := c.assetPage(ctx, query, limit, 0)
	return page.Assets, err
}

type assetPage struct {
	Assets  []Asset `json:"assets"`
	HasMore bool    `json:"hasMore"`
}

func (c *MonacoClient) assetPage(ctx context.Context, query string, limit, offset int) (assetPage, error) {
	q := url.Values{}
	if query != "" {
		q.Set("query", query)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	var out assetPage
	err := c.get(ctx, "/v1/agent/assets?"+q.Encode(), &out)
	return out, err
}

// Prices returns USD per share for the given symbols from Monaco's marks, reading
// catalog pages until every symbol is found. Symbols without a mark are absent.
func (c *MonacoClient) Prices(ctx context.Context, symbols []string) (map[string]float64, error) {
	want := make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		want[symbol] = true
	}
	out := make(map[string]float64, len(symbols))
	for offset, found := 0, 0; found < len(want); {
		page, err := c.assetPage(ctx, "", catalogPageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, asset := range page.Assets {
			if !want[asset.Symbol] {
				continue
			}
			found++
			if asset.MarkUsdcMicros != nil && *asset.MarkUsdcMicros > 0 {
				out[asset.Symbol] = float64(*asset.MarkUsdcMicros) / usdcMicrosPerUSD
			}
		}
		if !page.HasMore || len(page.Assets) == 0 {
			break
		}
		offset += len(page.Assets)
	}
	return out, nil
}

// Intent reads where an intent ended up, for an outcome a submit could not settle.
func (c *MonacoClient) Intent(ctx context.Context, intentID string) (IntentRecord, error) {
	var out IntentRecord
	err := c.get(ctx, "/v1/agent/intents/"+url.PathEscape(intentID), &out)
	return out, err
}

func (c *MonacoClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// NewIdempotencyKey returns 128 random bits as hex, one per trade decision.
func NewIdempotencyKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("idempotency key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// UnsettledError is a 2xx answer for an intent that did not execute: the replay of an
// intent whose swap failed on the server.
type UnsettledError struct{ Status string }

func (e *UnsettledError) Error() string { return "intent " + e.Status }

// SubmitIntent posts one trade and never retries on its own. A request that fails after
// it was sent may still have executed, so only resend an intent that carries an
// IdempotencyKey, and resend it unchanged.
func (c *MonacoClient) SubmitIntent(ctx context.Context, intent Intent) (IntentResult, error) {
	body, err := json.Marshal(intent)
	if err != nil {
		return IntentResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/agent/intents", bytes.NewReader(body))
	if err != nil {
		return IntentResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out IntentResult
	if err := c.do(req, &out); err != nil {
		return IntentResult{}, err
	}
	if out.Status != "executed" {
		return out, &UnsettledError{Status: out.Status}
	}
	return out, nil
}

func (c *MonacoClient) do(req *http.Request, out any) error {
	req.Header.Set(agentKeyHeader, c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("monaco api: %w", redactURLError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("monaco api: read body: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("monaco api: decode response: %w", err)
		}
		return nil
	}

	payload := parseErrorBody(body)
	message := payload.Error
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrBadKey
	case http.StatusForbidden:
		return ErrPaused
	case http.StatusUnprocessableEntity:
		reason := payload.RejectReason
		if reason == "" {
			reason = strings.TrimPrefix(message, "agent intent rejected: ")
		}
		return &RejectedError{Reason: reason, IntentID: payload.IntentID}
	case http.StatusTooManyRequests:
		return &ThrottledError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	default:
		return &StatusError{Status: resp.StatusCode, Message: message, IntentID: payload.IntentID, IntentStatus: payload.Status}
	}
}

// errorBody is Monaco's error shape. The intent fields are only present on the intents
// route, and only once the server had an outcome for the intent.
type errorBody struct {
	Error        string `json:"error"`
	IntentID     string `json:"intentId"`
	Status       string `json:"status"`
	RejectReason string `json:"rejectReason"`
}

// parseErrorBody reads an API error body. Anything that is not Monaco's JSON (a proxy's
// HTML page, say) becomes a short prefix of whatever came back, as the message.
func parseErrorBody(body []byte) errorBody {
	var payload errorBody
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		return payload
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 120 {
		text = text[:120]
	}
	return errorBody{Error: text}
}

// parseRetryAfter reads delta-seconds. A missing or malformed header falls back to a
// minute, the server's refill rate for wrong keys.
func parseRetryAfter(raw string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || seconds <= 0 {
		return time.Minute
	}
	return time.Duration(seconds) * time.Second
}

// redactURLError drops the URL from transport errors so logs carry the cause only.
func redactURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
