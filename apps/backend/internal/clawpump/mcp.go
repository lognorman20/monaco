// Package clawpump calls the ClawPump MCP server as the operator of a ClawPump agent.
//
// Monaco only uses it to recall USDC a cabal deployed to an agent it operates: point the
// agent's external wallet at the group treasury, then ask the agent to send USDC there.
// A tool response is never proof of a return; the treasury inbound poller is.
package clawpump

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultMCPURL is ClawPump's hosted streamable-HTTP MCP endpoint.
const DefaultMCPURL = "https://clawpump.tech/api/mcp"

const (
	ToolSetExternalWallet = "set_external_wallet"
	ToolAgentSend         = "agent_send"

	mcpProtocolVersion = "2025-03-26"
	sessionHeader      = "Mcp-Session-Id"
)

// ErrToolFailed means the MCP server ran the tool and reported an error result.
var ErrToolFailed = errors.New("clawpump: tool failed")

// Client is the operator surface Monaco needs from ClawPump.
type Client interface {
	// SetExternalWallet sets the Solana wallet the agent withdraws to.
	SetExternalWallet(ctx context.Context, operatorKey, solanaAddress string) error
	// AgentSend asks the agent to send amountMicros of USDC to toAddress.
	AgentSend(ctx context.Context, operatorKey, toAddress string, amountMicros int64) error
}

// HTTPClient speaks MCP JSON-RPC over streamable HTTP, one session per call.
type HTTPClient struct {
	url        string
	httpClient *http.Client
	nextID     atomic.Int64
}

// NewHTTPClient builds a client for url (DefaultMCPURL when empty).
func NewHTTPClient(url string, httpClient *http.Client) *HTTPClient {
	if strings.TrimSpace(url) == "" {
		url = DefaultMCPURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &HTTPClient{url: url, httpClient: httpClient}
}

func (c *HTTPClient) SetExternalWallet(ctx context.Context, operatorKey, solanaAddress string) error {
	return c.callTool(ctx, operatorKey, ToolSetExternalWallet, map[string]any{
		"address": solanaAddress,
	})
}

func (c *HTTPClient) AgentSend(ctx context.Context, operatorKey, toAddress string, amountMicros int64) error {
	if amountMicros <= 0 {
		return fmt.Errorf("clawpump: agent_send amount must be positive")
	}
	return c.callTool(ctx, operatorKey, ToolAgentSend, map[string]any{
		"to":     toAddress,
		"token":  "USDC",
		"amount": FormatUSDC(amountMicros),
	})
}

// FormatUSDC renders micros as a decimal USDC amount ("12.5", "0.000001").
func FormatUSDC(micros int64) string {
	whole := micros / 1_000_000
	frac := micros % 1_000_000
	if frac == 0 {
		return fmt.Sprintf("%d", whole)
	}
	return strings.TrimRight(fmt.Sprintf("%d.%06d", whole, frac), "0")
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     *int64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type toolResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (c *HTTPClient) callTool(ctx context.Context, operatorKey, name string, args map[string]any) error {
	operatorKey = strings.TrimSpace(operatorKey)
	if !strings.HasPrefix(operatorKey, "cpk_") {
		return fmt.Errorf("clawpump: operator key must be a cpk_ key")
	}

	session, _, err := c.post(ctx, operatorKey, "", "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "monaco-backend", "version": "1"},
	}, true)
	if err != nil {
		return fmt.Errorf("clawpump: initialize: %w", err)
	}
	if _, _, err := c.post(ctx, operatorKey, session, "notifications/initialized", nil, false); err != nil {
		return fmt.Errorf("clawpump: initialized: %w", err)
	}
	_, raw, err := c.post(ctx, operatorKey, session, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	}, true)
	if err != nil {
		return fmt.Errorf("clawpump: %s: %w", name, err)
	}
	var result toolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("clawpump: %s result: %w", name, err)
	}
	if result.IsError {
		var texts []string
		for _, part := range result.Content {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		return fmt.Errorf("%w: %s: %s", ErrToolFailed, name, strings.Join(texts, "; "))
	}
	slog.Info("clawpump tool ok", "tool", name)
	return nil
}

// post sends one JSON-RPC message. Requests (wantResult) return the matching result from a
// JSON body or an SSE stream; notifications expect 202/200 with no result.
func (c *HTTPClient) post(ctx context.Context, operatorKey, session, method string, params any, wantResult bool) (string, json.RawMessage, error) {
	msg := rpcRequest{JSONRPC: "2.0", Method: method, Params: params}
	var id int64
	if wantResult {
		id = c.nextID.Add(1)
		msg.ID = &id
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return "", nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+operatorKey)
	if session != "" {
		req.Header.Set(sessionHeader, session)
		req.Header.Set("Mcp-Protocol-Version", mcpProtocolVersion)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if next := resp.Header.Get(sessionHeader); next != "" {
		session = next
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return session, nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if !wantResult {
		_, _ = io.Copy(io.Discard, resp.Body)
		return session, nil, nil
	}

	var messages [][]byte
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		messages, err = readSSEData(resp.Body)
		if err != nil {
			return session, nil, err
		}
	} else {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return session, nil, err
		}
		messages = [][]byte{raw}
	}
	for _, raw := range messages {
		var out rpcResponse
		if err := json.Unmarshal(raw, &out); err != nil || out.ID == nil || *out.ID != id {
			continue
		}
		if out.Error != nil {
			return session, nil, fmt.Errorf("rpc error %d: %s", out.Error.Code, out.Error.Message)
		}
		return session, out.Result, nil
	}
	return session, nil, fmt.Errorf("no response for %s", method)
}

func readSSEData(r io.Reader) ([][]byte, error) {
	var out [][]byte
	var current bytes.Buffer
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	flush := func() {
		if current.Len() > 0 {
			out = append(out, append([]byte(nil), current.Bytes()...))
			current.Reset()
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "data:"):
			if current.Len() > 0 {
				current.WriteByte('\n')
			}
			current.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	flush()
	return out, scanner.Err()
}

var _ Client = (*HTTPClient)(nil)
