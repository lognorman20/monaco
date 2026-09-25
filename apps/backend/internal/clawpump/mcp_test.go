package clawpump

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFormatUSDC(t *testing.T) {
	for micros, want := range map[int64]string{1_000_000: "1", 12_500_000: "12.5", 1: "0.000001", 1_234_567: "1.234567"} {
		if got := FormatUSDC(micros); got != want {
			t.Fatalf("FormatUSDC(%d) = %q, want %q", micros, got, want)
		}
	}
}

type mcpStub struct {
	t        *testing.T
	sse      bool
	isError  bool
	toolArgs map[string]map[string]any
	methods  []string
}

func (s *mcpStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer cpk_test" {
		s.t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
	}
	var msg struct {
		ID     *int64         `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&msg)
	s.methods = append(s.methods, msg.Method)
	if msg.Method != "initialize" && r.Header.Get(sessionHeader) != "sess-1" {
		s.t.Fatalf("%s missing session header", msg.Method)
	}
	if msg.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any = map[string]any{"protocolVersion": mcpProtocolVersion}
	if msg.Method == "initialize" {
		w.Header().Set(sessionHeader, "sess-1")
	}
	if msg.Method == "tools/call" {
		name, _ := msg.Params["name"].(string)
		args, _ := msg.Params["arguments"].(map[string]any)
		s.toolArgs[name] = args
		result = map[string]any{"isError": s.isError, "content": []any{map[string]any{"type": "text", "text": "done"}}}
	}
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": result})
	if s.sse {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func TestHTTPClient_toolCalls(t *testing.T) {
	for _, sse := range []bool{false, true} {
		stub := &mcpStub{t: t, sse: sse, toolArgs: map[string]map[string]any{}}
		srv := httptest.NewServer(stub)
		c := NewHTTPClient(srv.URL, srv.Client())

		if err := c.SetExternalWallet(context.Background(), "cpk_test", "Treasury111"); err != nil {
			t.Fatalf("sse=%v set_external_wallet: %v", sse, err)
		}
		if err := c.AgentSend(context.Background(), "cpk_test", "Treasury111", 2_500_000); err != nil {
			t.Fatalf("sse=%v agent_send: %v", sse, err)
		}
		srv.Close()

		if got := stub.toolArgs[ToolSetExternalWallet]["address"]; got != "Treasury111" {
			t.Fatalf("set_external_wallet address = %v", got)
		}
		send := stub.toolArgs[ToolAgentSend]
		if send["to"] != "Treasury111" || send["amount"] != "2.5" || send["token"] != "USDC" {
			t.Fatalf("agent_send args = %v", send)
		}
		want := []string{"initialize", "notifications/initialized", "tools/call"}
		if len(stub.methods) != 6 || stub.methods[0] != want[0] || stub.methods[1] != want[1] || stub.methods[2] != want[2] {
			t.Fatalf("methods = %v", stub.methods)
		}
	}
}

func TestHTTPClient_toolErrorResult(t *testing.T) {
	stub := &mcpStub{t: t, isError: true, toolArgs: map[string]map[string]any{}}
	srv := httptest.NewServer(stub)
	defer srv.Close()
	err := NewHTTPClient(srv.URL, srv.Client()).AgentSend(context.Background(), "cpk_test", "T", 1)
	if !errors.Is(err, ErrToolFailed) {
		t.Fatalf("err = %v, want ErrToolFailed", err)
	}
}

func TestHTTPClient_rejectsNonOperatorKey(t *testing.T) {
	if err := NewHTTPClient("http://unused.invalid", nil).SetExternalWallet(context.Background(), "sk_nope", "T"); err == nil {
		t.Fatal("expected cpk_ key requirement")
	}
}
