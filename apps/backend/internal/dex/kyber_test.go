package dex

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKyber_QuoteBuy_parsesRouteSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/base/api/v1/routes" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if r.Header.Get("x-client-id") != "monaco" {
			t.Fatalf("client id %s", r.Header.Get("x-client-id"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"routeSummary":  map[string]any{"amountOut": "42"},
				"routerAddress": "0xrouter",
			},
		})
	}))
	t.Cleanup(srv.Close)
	c := newKyberClient(srv.Client(), "monaco", srv.URL)
	q, err := c.QuoteBuy(t.Context(), "0xtoken", big.NewInt(1_000_000))
	if err != nil {
		t.Fatal(err)
	}
	if !q.Routable || q.AmountOut.String() != "42" {
		t.Fatalf("quote = %+v", q)
	}
}

func TestKyber_QuoteBuy_noRoute_returnsRoutableFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "data": map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	c := newKyberClient(srv.Client(), "monaco", srv.URL)
	q, err := c.QuoteBuy(t.Context(), "0xtoken", big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	if q.Routable {
		t.Fatal("expected not routable")
	}
}

func TestKyber_BuildSwap_httpError_returnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":1}`))
	}))
	t.Cleanup(srv.Close)
	c := newKyberClient(srv.Client(), "monaco", srv.URL)
	_, err := c.BuildSwap(t.Context(), Quote{RouteSummary: json.RawMessage(`{}`)}, "0xsender", "0xrecip")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestKyber_BuildSwap_usesTwoPercentSlippage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body kyberBuildRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.SlippageTolerance != kyberSlippageBps {
			t.Fatalf("slippage = %d, want %d", body.SlippageTolerance, kyberSlippageBps)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"data":          "0xdeadbeef",
				"routerAddress": "0xabc0000000000000000000000000000000000001",
				"amountOutMin":  "9",
			},
		})
	}))
	t.Cleanup(srv.Close)
	c := newKyberClient(srv.Client(), "monaco", srv.URL)
	if _, err := c.BuildSwap(t.Context(), Quote{RouteSummary: json.RawMessage(`{}`)}, "0xsender", "0xrecip"); err != nil {
		t.Fatal(err)
	}
}

func TestKyber_BuildSwap_returnsRouterAndCalldata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/route/build") {
			t.Fatalf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"data":          "0xdeadbeef",
				"routerAddress": "0xabc0000000000000000000000000000000000001",
				"amountOutMin":  "9",
			},
		})
	}))
	t.Cleanup(srv.Close)
	c := newKyberClient(srv.Client(), "monaco", srv.URL)
	call, err := c.BuildSwap(t.Context(), Quote{RouteSummary: json.RawMessage(`{}`)}, "0xsender", "0xrecip")
	if err != nil {
		t.Fatal(err)
	}
	if call.Router != "0xabc0000000000000000000000000000000000001" {
		t.Fatalf("router %s", call.Router)
	}
	if string(call.Data) == "" && len(call.Data) == 0 {
		t.Fatal("empty data")
	}
}
