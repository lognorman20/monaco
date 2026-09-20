package main

import (
	"bytes"
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

const agentKeyHeader = "X-Monaco-Agent-Key"

// Errors the bot reacts to differently. Anything else is a transient failure.
var (
	// ErrBadKey is a 401: the key is unknown, revoked, or belongs to another cabal.
	// Retrying only feeds the server's wrong-key throttle, so the bot stops.
	ErrBadKey = errors.New("agent key rejected")
	// ErrPaused is a 403: the cabal voted to pause the agent. The key still works,
	// so the bot waits for a resume vote instead of exiting.
	ErrPaused = errors.New("agent is paused")
)

// RejectedError is a 422: the server understood the intent and refused it
// (over budget, treasury short of USDC, selling more than the cabal holds).
type RejectedError struct{ Reason string }

func (e *RejectedError) Error() string { return "intent rejected: " + e.Reason }

// ThrottledError is a 429 with the server's Retry-After.
type ThrottledError struct{ RetryAfter time.Duration }

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("throttled, retry after %s", e.RetryAfter)
}

// StatusError is any other non-2xx response.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("monaco api status %d: %s", e.Status, e.Message)
}

// Asset is one tradable xStock from the cabal's catalog.
type Asset struct {
	Symbol     string `json:"symbol"`
	Name       string `json:"name"`
	SolanaMint string `json:"solanaMint"`
	Routable   bool   `json:"routable"`
}

// Intent is the body of POST /v1/groups/{id}/agents/intents. Buys carry usdcMicros
// (USD x 1e6); sells carry tokenAmount (shares x 1e8).
type Intent struct {
	Side        string `json:"side"`
	Symbol      string `json:"symbol"`
	UsdcMicros  int64  `json:"usdcMicros,omitempty"`
	TokenAmount int64  `json:"tokenAmount,omitempty"`
}

// IntentResult is the server's answer for an executed intent.
type IntentResult struct {
	IntentID      string `json:"intentId"`
	Status        string `json:"status"`
	TransactionID string `json:"transactionId"`
	RejectReason  string `json:"rejectReason"`
}

// MonacoClient talks to one cabal's agent endpoints. The key only ever leaves this
// struct as a request header.
type MonacoClient struct {
	baseURL string
	groupID string
	key     string
	http    *http.Client
}

// NewMonacoClient returns a client for one cabal. httpClient may be nil.
func NewMonacoClient(baseURL, groupID, key string, httpClient *http.Client) *MonacoClient {
	if httpClient == nil {
		// Intents settle an on-chain swap before answering, so this is generous.
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	return &MonacoClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		groupID: groupID,
		key:     key,
		http:    httpClient,
	}
}

// Assets returns one page of the cabal's tradable catalog. A ticker query
// ("GOOGLx") resolves to that single asset.
func (c *MonacoClient) Assets(ctx context.Context, query string, limit int) ([]Asset, error) {
	q := url.Values{}
	if query != "" {
		q.Set("query", query)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	endpoint := fmt.Sprintf("%s/v1/groups/%s/assets?%s", c.baseURL, url.PathEscape(c.groupID), q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Assets []Asset `json:"assets"`
	}
	if err := c.do(req, &out); err != nil {
		return nil, err
	}
	return out.Assets, nil
}

// SubmitIntent posts one trade. It is not idempotent and is never retried here:
// a request that fails after it was sent may still have executed.
func (c *MonacoClient) SubmitIntent(ctx context.Context, intent Intent) (IntentResult, error) {
	body, err := json.Marshal(intent)
	if err != nil {
		return IntentResult{}, err
	}
	endpoint := fmt.Sprintf("%s/v1/groups/%s/agents/intents", c.baseURL, url.PathEscape(c.groupID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return IntentResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out IntentResult
	if err := c.do(req, &out); err != nil {
		return IntentResult{}, err
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

	message := errorMessage(body)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrBadKey
	case http.StatusForbidden:
		return ErrPaused
	case http.StatusUnprocessableEntity:
		return &RejectedError{Reason: strings.TrimPrefix(message, "agent intent rejected: ")}
	case http.StatusTooManyRequests:
		return &ThrottledError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	default:
		return &StatusError{Status: resp.StatusCode, Message: message}
	}
}

// errorMessage pulls {"error": "..."} out of an API error body, falling back to a
// short prefix of whatever came back (a proxy's HTML page, say).
func errorMessage(body []byte) string {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		return payload.Error
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 120 {
		text = text[:120]
	}
	return text
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
