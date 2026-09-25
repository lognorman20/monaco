package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testKey = "k7m2p"

func newTestClient(t *testing.T, handler http.HandlerFunc) *MonacoClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewMonacoClient(srv.URL+"/", testKey, srv.Client())
}

func TestSubmitIntent_sendsKeyHeaderAndBuyBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/agent/intents" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Monaco-Agent-Key"); got != testKey {
			t.Errorf("key header %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"side":"buy","symbol":"GOOGLx","usdcMicros":1000000}` {
			t.Errorf("body %s", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"intentId": "i-1", "status": "executed", "transactionId": "tx-1"})
	})
	result, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "GOOGLx", UsdcMicros: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if result.IntentID != "i-1" || result.TransactionID != "tx-1" || result.Status != "executed" {
		t.Fatalf("result %+v", result)
	}
}

func TestSubmitIntent_sellBodyCarriesTokenAmountOnly(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"side":"sell","symbol":"AAPLx","tokenAmount":50000000}` {
			t.Errorf("body %s", body)
		}
		_, _ = w.Write([]byte(`{"intentId":"i-2","status":"executed"}`))
	})
	if _, err := client.SubmitIntent(context.Background(), Intent{Side: "sell", Symbol: "AAPLx", TokenAmount: 50_000_000}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitIntent_errorStatuses(t *testing.T) {
	respond := func(status int, header, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if header != "" {
				w.Header().Set("Retry-After", header)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}

	t.Run("401 is a bad key", func(t *testing.T) {
		client := newTestClient(t, respond(401, "", `{"error":"invalid agent api key"}`))
		_, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "X", UsdcMicros: 1})
		if !errors.Is(err, ErrBadKey) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("403 is paused", func(t *testing.T) {
		client := newTestClient(t, respond(403, "", `{"error":"agent is paused"}`))
		_, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "X", UsdcMicros: 1})
		if !errors.Is(err, ErrPaused) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("422 carries the server's reason", func(t *testing.T) {
		client := newTestClient(t, respond(422, "", `{"error":"agent intent rejected: trade exceeds agent allocation"}`))
		_, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "X", UsdcMicros: 1})
		var rejected *RejectedError
		if !errors.As(err, &rejected) || rejected.Reason != "trade exceeds agent allocation" {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("422 reads rejectReason and intentId from the body", func(t *testing.T) {
		client := newTestClient(t, respond(422, "", `{"error":"agent intent rejected: trade exceeds agent allocation","requestId":"r1","intentId":"intent-7","status":"rejected","rejectReason":"trade exceeds agent allocation"}`))
		_, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "X", UsdcMicros: 1})
		var rejected *RejectedError
		if !errors.As(err, &rejected) || rejected.Reason != "trade exceeds agent allocation" || rejected.IntentID != "intent-7" {
			t.Fatalf("got %#v", err)
		}
		if !strings.Contains(err.Error(), "intent-7") {
			t.Fatalf("error does not quote the intent id: %v", err)
		}
	})
	t.Run("500 for a failed swap carries the intent id and status", func(t *testing.T) {
		client := newTestClient(t, respond(500, "", `{"error":"internal server error","requestId":"r1","intentId":"intent-8","status":"failed","rejectReason":"execution failed"}`))
		_, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "X", UsdcMicros: 1})
		var status *StatusError
		if !errors.As(err, &status) || status.Status != 500 || status.IntentID != "intent-8" || status.IntentStatus != "failed" {
			t.Fatalf("got %#v", err)
		}
		if !strings.Contains(err.Error(), "intent-8") {
			t.Fatalf("error does not quote the intent id: %v", err)
		}
	})
	t.Run("403 with intent fields is still paused", func(t *testing.T) {
		client := newTestClient(t, respond(403, "", `{"error":"agent is paused","status":"rejected","rejectReason":"agent is paused"}`))
		_, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "X", UsdcMicros: 1})
		if !errors.Is(err, ErrPaused) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("429 carries Retry-After", func(t *testing.T) {
		client := newTestClient(t, respond(429, "42", `{"error":"too many requests"}`))
		_, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "X", UsdcMicros: 1})
		var throttled *ThrottledError
		if !errors.As(err, &throttled) || throttled.RetryAfter != 42*time.Second {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("429 without Retry-After waits a minute", func(t *testing.T) {
		client := newTestClient(t, respond(429, "", ``))
		_, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "X", UsdcMicros: 1})
		var throttled *ThrottledError
		if !errors.As(err, &throttled) || throttled.RetryAfter != time.Minute {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("500 with a non-JSON body is a status error", func(t *testing.T) {
		client := newTestClient(t, respond(502, "", `<html>bad gateway</html>`))
		_, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "X", UsdcMicros: 1})
		var status *StatusError
		if !errors.As(err, &status) || status.Status != 502 {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("200 with a malformed body is an error", func(t *testing.T) {
		client := newTestClient(t, respond(200, "", `not json`))
		if _, err := client.SubmitIntent(context.Background(), Intent{Side: "buy", Symbol: "X", UsdcMicros: 1}); err == nil {
			t.Fatal("want decode error")
		}
	})
}

func TestClient_networkErrorNeverLeaksTheKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	client := NewMonacoClient(srv.URL, testKey, srv.Client())
	srv.Close()
	_, err := client.Assets(context.Background(), "", 10)
	if err == nil {
		t.Fatal("want a connection error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("error leaks the key: %v", err)
	}
}

func TestAssets_queriesTheAgentCatalogWithTheKeyOnly(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/assets" || r.URL.Query().Get("query") != "GOOGLx" || r.URL.Query().Get("limit") != "1" {
			t.Errorf("got %s", r.URL.String())
		}
		if r.Header.Get("X-Monaco-Agent-Key") != testKey || r.Header.Get("Authorization") != "" {
			t.Errorf("headers %v", r.Header)
		}
		_, _ = w.Write([]byte(`{"assets":[{"symbol":"GOOGLx","name":"Alphabet xStock","solanaMint":"mintG","routable":true}],"hasMore":false}`))
	})
	assets, err := client.Assets(context.Background(), "GOOGLx", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].SolanaMint != "mintG" || !assets[0].Routable {
		t.Fatalf("assets %+v", assets)
	}
}

func TestSubmitIntent_sendsTheReason(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"side":"buy","symbol":"AAPLx","usdcMicros":1000000,"idempotencyKey":"k-1","reason":"momentum +1.00% over 5m"}` {
			t.Errorf("body %s", body)
		}
		_, _ = w.Write([]byte(`{"intentId":"i-3","status":"executed"}`))
	})
	intent := Intent{Side: "buy", Symbol: "AAPLx", UsdcMicros: 1_000_000, IdempotencyKey: "k-1", Reason: "momentum +1.00% over 5m"}
	if _, err := client.SubmitIntent(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
}

func TestAgent_readsCabalAndBudgetFromTheKey(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/agent" || r.Header.Get("X-Monaco-Agent-Key") != testKey {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"cabalName":"Tech Bros","agentName":"Momentum","status":"active","budget":{"allocationUsd":"100.00","allocationUsdcMicros":100000000,"availableUsd":"42.10","availableUsdcMicros":42100000}}`))
	})
	agent, err := client.Agent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if agent.CabalName != "Tech Bros" || agent.AgentName != "Momentum" || agent.Status != "active" || agent.Budget.AvailableUsd != "42.10" || agent.Budget.AllocationUsd != "100.00" {
		t.Fatalf("agent %+v", agent)
	}
}

func TestIntent_readsOneIntentsOutcome(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/agent/intents/intent-7" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"intentId":"intent-7","side":"buy","symbol":"AAPLx","status":"executed","transactionId":"tx-7","txSignature":"5x","filledTokenAmount":4560000,"filledUsdcMicros":10500000}`))
	})
	record, err := client.Intent(context.Background(), "intent-7")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "executed" || record.TransactionID != "tx-7" || record.FilledTokenAmount == nil || *record.FilledTokenAmount != 4_560_000 {
		t.Fatalf("record %+v", record)
	}
}

func TestPrices_comeFromMonacoMarksAcrossPages(t *testing.T) {
	var offsets []string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/assets" || r.URL.Query().Get("limit") != "100" {
			t.Errorf("got %s", r.URL.String())
		}
		offsets = append(offsets, r.URL.Query().Get("offset"))
		switch r.URL.Query().Get("offset") {
		case "":
			_, _ = w.Write([]byte(`{"assets":[{"symbol":"GOOGLx","markUsdcMicros":351730000},{"symbol":"NVDAx","markUsdcMicros":null},{"symbol":"TSLAx","markUsdcMicros":1}],"hasMore":true}`))
		case "3":
			_, _ = w.Write([]byte(`{"assets":[{"symbol":"AAPLx","markUsd":"230.12","markUsdcMicros":230120000}],"hasMore":true}`))
		default:
			t.Errorf("read past the page that had every symbol: offset %s", r.URL.Query().Get("offset"))
		}
	})
	prices, err := client.Prices(context.Background(), []string{"GOOGLx", "NVDAx", "AAPLx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 2 || prices["GOOGLx"] != 351.73 || prices["AAPLx"] != 230.12 {
		t.Fatalf("prices %v, want GOOGLx and AAPLx only (NVDAx has no mark)", prices)
	}
	if len(offsets) != 2 {
		t.Fatalf("read offsets %v, want two pages", offsets)
	}
}

func TestPrices_stopsAtTheLastPage(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"assets":[{"symbol":"GOOGLx","markUsdcMicros":100000000}],"hasMore":false}`))
	})
	prices, err := client.Prices(context.Background(), []string{"GOOGLx", "GONEx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 1 || prices["GOOGLx"] != 100 {
		t.Fatalf("prices %v", prices)
	}
}

func TestPrices_errorStatusesSurface(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := client.Prices(context.Background(), []string{"GOOGLx"})
	var throttled *ThrottledError
	if !errors.As(err, &throttled) || throttled.RetryAfter != 30*time.Second {
		t.Fatalf("got %v", err)
	}
}
