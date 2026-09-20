package swapchain

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testTreasury = "TreasuryOwner111111111111111111111111111111"
	testUSDC     = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	testXStock   = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"
)

// rpcServer answers each JSON-RPC method with a canned body and records the params it saw.
func rpcServer(t *testing.T, status int, responses map[string]string) (*HTTPReader, map[string]json.RawMessage) {
	t.Helper()
	seen := make(map[string]json.RawMessage)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("invalid rpc request: %v", err)
		}
		seen[req.Method] = req.Params
		w.WriteHeader(status)
		_, _ = io.WriteString(w, responses[req.Method])
	}))
	t.Cleanup(server.Close)
	return NewHTTPReader(server.URL, ""), seen
}

func TestHTTPReader_signatureStatus(t *testing.T) {
	cases := map[string]struct {
		body                     string
		found, finalized, failed bool
	}{
		"not found":         {`{"result":{"value":[null]}}`, false, false, false},
		"confirmed only":    {`{"result":{"value":[{"err":null,"confirmationStatus":"confirmed"}]}}`, true, false, false},
		"finalized":         {`{"result":{"value":[{"err":null,"confirmationStatus":"finalized"}]}}`, true, true, false},
		"finalized failure": {`{"result":{"value":[{"err":{"InstructionError":[2,{"Custom":6001}]},"confirmationStatus":"finalized"}]}}`, true, true, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			reader, seen := rpcServer(t, http.StatusOK, map[string]string{"getSignatureStatuses": tc.body})

			got, err := reader.SignatureStatus(context.Background(), "sig-1")

			if err != nil {
				t.Fatalf("SignatureStatus() error = %v", err)
			}
			if got.Found != tc.found || got.Finalized != tc.finalized || got.Failed != tc.failed {
				t.Fatalf("SignatureStatus() = %+v, want found=%v finalized=%v failed=%v", got, tc.found, tc.finalized, tc.failed)
			}
			// Without full history an old signature reads as "not found", which would look expired.
			if want := `[["sig-1"],{"searchTransactionHistory":true}]`; string(seen["getSignatureStatuses"]) != want {
				t.Fatalf("params = %s, want %s", seen["getSignatureStatuses"], want)
			}
		})
	}
}

func TestHTTPReader_isBlockhashValid_asksTheFinalizedBank(t *testing.T) {
	reader, seen := rpcServer(t, http.StatusOK, map[string]string{"isBlockhashValid": `{"result":{"context":{"slot":1},"value":false}}`})

	valid, err := reader.IsBlockhashValid(context.Background(), "hash-1")

	if err != nil || valid {
		t.Fatalf("IsBlockhashValid() = %v, %v, want false", valid, err)
	}
	if want := `["hash-1",{"commitment":"finalized"}]`; string(seen["isBlockhashValid"]) != want {
		t.Fatalf("params = %s, want %s", seen["isBlockhashValid"], want)
	}
}

func TestHTTPReader_tokenBalanceChanges_sumsOwnerAccountsPerMint(t *testing.T) {
	body := `{"result":{"meta":{"err":null,
	  "preTokenBalances":[
	    {"accountIndex":1,"mint":"` + testUSDC + `","owner":"` + testTreasury + `","uiTokenAmount":{"amount":"5000000"}},
	    {"accountIndex":4,"mint":"` + testUSDC + `","owner":"SomeAmmPool","uiTokenAmount":{"amount":"900000000"}}
	  ],
	  "postTokenBalances":[
	    {"accountIndex":1,"mint":"` + testUSDC + `","owner":"` + testTreasury + `","uiTokenAmount":{"amount":"3000000"}},
	    {"accountIndex":2,"mint":"` + testXStock + `","owner":"` + testTreasury + `","uiTokenAmount":{"amount":"1000000"}},
	    {"accountIndex":4,"mint":"` + testUSDC + `","owner":"SomeAmmPool","uiTokenAmount":{"amount":"902000000"}}
	  ]}}}`
	reader, _ := rpcServer(t, http.StatusOK, map[string]string{"getTransaction": body})

	changes, err := reader.TokenBalanceChanges(context.Background(), "sig-1", testTreasury)

	if err != nil {
		t.Fatalf("TokenBalanceChanges() error = %v", err)
	}
	// The xStock account did not exist before the swap, so it has no pre-balance entry.
	if changes[testUSDC] != -2_000_000 || changes[testXStock] != 1_000_000 || len(changes) != 2 {
		t.Fatalf("changes = %v, want USDC -2000000 and xStock +1000000 for the treasury only", changes)
	}
}

func TestHTTPReader_unhappyPaths_returnErrorsNotVerdicts(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"rate limited":      {http.StatusTooManyRequests, `{"error":"slow down"}`},
		"rpc error":         {http.StatusOK, `{"error":{"code":-32005,"message":"node is behind"}}`},
		"not json":          {http.StatusOK, `<html>bad gateway</html>`},
		"no result":         {http.StatusOK, `{}`},
		"malformed result":  {http.StatusOK, `{"result":"nope"}`},
		"empty status list": {http.StatusOK, `{"result":{"value":[]}}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			reader, _ := rpcServer(t, tc.status, map[string]string{
				"getSignatureStatuses": tc.body,
				"getTransaction":       tc.body,
			})

			if _, err := reader.SignatureStatus(context.Background(), "sig-1"); err == nil {
				t.Fatal("SignatureStatus() error = nil, want an error")
			}
			if name == "empty status list" {
				return
			}
			if _, err := reader.TokenBalanceChanges(context.Background(), "sig-1", testTreasury); err == nil {
				t.Fatal("TokenBalanceChanges() error = nil, want an error")
			}
		})
	}
}

func TestHTTPReader_tokenBalanceChanges_refusesMissingOrFailedTransactions(t *testing.T) {
	for name, body := range map[string]string{
		"not found":       `{"result":null}`,
		"failed on chain": `{"result":{"meta":{"err":{"InstructionError":[2,{"Custom":1}]},"preTokenBalances":[],"postTokenBalances":[]}}}`,
		"bad amount":      `{"result":{"meta":{"err":null,"preTokenBalances":[],"postTokenBalances":[{"mint":"` + testUSDC + `","owner":"` + testTreasury + `","uiTokenAmount":{"amount":"1.5"}}]}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			reader, _ := rpcServer(t, http.StatusOK, map[string]string{"getTransaction": body})

			if _, err := reader.TokenBalanceChanges(context.Background(), "sig-1", testTreasury); err == nil {
				t.Fatal("TokenBalanceChanges() error = nil, want an error")
			}
		})
	}
}

func TestHTTPReader_networkFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	reader := NewHTTPReader(server.URL, "")
	server.Close()

	if _, err := reader.IsBlockhashValid(context.Background(), "hash-1"); err == nil {
		t.Fatal("IsBlockhashValid() error = nil, want a transport error")
	}
}
