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
	return NewMonacoClient(srv.URL+"/", "group-1", testKey, srv.Client())
}

func TestSubmitIntent_sendsKeyHeaderAndBuyBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/groups/group-1/agents/intents" {
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
	client := NewMonacoClient(srv.URL, "group-1", testKey, srv.Client())
	srv.Close()
	_, err := client.Assets(context.Background(), "", 10)
	if err == nil {
		t.Fatal("want a connection error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("error leaks the key: %v", err)
	}
}

func TestAssets_queriesTheCabalCatalogWithTheKey(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/groups/group-1/assets" || r.URL.Query().Get("query") != "GOOGLx" || r.URL.Query().Get("limit") != "1" {
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

func TestPrices_parsesJupiterV3AndSkipsMissingMints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ids") != "mintG,mintA" {
			t.Errorf("ids %q", r.URL.Query().Get("ids"))
		}
		_, _ = w.Write([]byte(`{"mintG":{"usdPrice":351.73,"decimals":8,"priceChange24h":0.34},"mintZ":{"usdPrice":0}}`))
	}))
	defer srv.Close()
	prices, err := NewPriceClient(srv.URL, srv.Client()).Prices(context.Background(), []string{"mintG", "mintA"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 1 || prices["mintG"] != 351.73 {
		t.Fatalf("prices %v", prices)
	}
}

func TestPrices_unhappyPaths(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"500":       func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"malformed": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[`)) },
	} {
		srv := httptest.NewServer(handler)
		if _, err := NewPriceClient(srv.URL, srv.Client()).Prices(context.Background(), []string{"m"}); err == nil {
			t.Errorf("%s: want error", name)
		}
		srv.Close()
	}
}
