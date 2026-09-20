package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func statusRPCServer(t *testing.T, status int, body any) *HTTPSolanaRPC {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	rpc := NewHTTPSolanaRPC("mainnet-beta")
	rpc.endpoint = server.URL
	return rpc
}

func signatureStatusBody(entry any) map[string]any {
	return map[string]any{"result": map[string]any{"value": []any{entry}}}
}

func TestHTTPSolanaRPC_SignatureStatus_mapsChainOutcomes(t *testing.T) {
	chainErr := map[string]any{"InstructionError": []any{1, map[string]int{"Custom": 1}}}
	cases := []struct {
		name  string
		entry any
		want  SignatureState
	}{
		{"unknown signature", nil, SignatureNotFound},
		{"processed only", map[string]any{"err": nil, "confirmationStatus": "processed"}, SignaturePending},
		{"confirmed", map[string]any{"err": nil, "confirmationStatus": "confirmed"}, SignatureConfirmed},
		{"finalized", map[string]any{"err": nil, "confirmationStatus": "finalized"}, SignatureConfirmed},
		{"failed and finalized", map[string]any{"err": chainErr, "confirmationStatus": "finalized"}, SignatureFailed},
		// A failure seen only at processed commitment can still be forked away.
		{"failed but only processed", map[string]any{"err": chainErr, "confirmationStatus": "processed"}, SignaturePending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			rpc := statusRPCServer(t, http.StatusOK, signatureStatusBody(tc.entry))

			// Act
			status, err := rpc.SignatureStatus(context.Background(), "sig")

			// Assert: an on-chain failure is an answer, not an RPC error.
			if err != nil {
				t.Fatalf("SignatureStatus: %v", err)
			}
			if status.State != tc.want {
				t.Fatalf("state = %q, want %q", status.State, tc.want)
			}
			if tc.want == SignatureFailed && !strings.Contains(status.Err, "InstructionError") {
				t.Fatalf("status err = %q, want chain error detail", status.Err)
			}
		})
	}
}

func TestHTTPSolanaRPC_SignatureStatus_rpcFailuresAreErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   any
	}{
		{"http 503", http.StatusServiceUnavailable, map[string]string{"error": "unavailable"}},
		{"rpc error object", http.StatusOK, map[string]any{"error": map[string]string{"message": "node is behind"}}},
		{"malformed body", http.StatusOK, "not-an-object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rpc := statusRPCServer(t, tc.status, tc.body)
			if _, err := rpc.SignatureStatus(context.Background(), "sig"); err == nil {
				t.Fatal("SignatureStatus succeeded, want error")
			}
		})
	}
	rpc := statusRPCServer(t, http.StatusOK, signatureStatusBody(nil))
	if _, err := rpc.SignatureStatus(context.Background(), "  "); err == nil {
		t.Fatal("empty signature accepted")
	}
}

func TestHTTPSolanaRPC_IsConfirmed_onChainFailureIsAnError(t *testing.T) {
	// Arrange
	rpc := statusRPCServer(t, http.StatusOK, signatureStatusBody(map[string]any{
		"err":                map[string]any{"InstructionError": []any{0, "Custom"}},
		"confirmationStatus": "finalized",
	}))

	// Act
	confirmed, err := rpc.IsConfirmed(context.Background(), "sig")

	// Assert
	if confirmed || err == nil || !strings.Contains(err.Error(), "failed on chain") {
		t.Fatalf("IsConfirmed = %v, %v; want false and a failed-on-chain error", confirmed, err)
	}
}

func TestHTTPSolanaRPC_FinalizedBlockHeight(t *testing.T) {
	// Arrange
	var gotMethod string
	var gotCommitment any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req solanaRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotMethod = req.Method
		if len(req.Params) == 1 {
			if params, ok := req.Params[0].(map[string]any); ok {
				gotCommitment = params["commitment"]
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": 287_654_321})
	}))
	defer server.Close()
	rpc := NewHTTPSolanaRPC("mainnet-beta")
	rpc.endpoint = server.URL

	// Act
	height, err := rpc.FinalizedBlockHeight(context.Background())

	// Assert
	if err != nil {
		t.Fatalf("FinalizedBlockHeight: %v", err)
	}
	if height != 287_654_321 {
		t.Fatalf("height = %d, want 287654321", height)
	}
	if gotMethod != "getBlockHeight" || gotCommitment != "finalized" {
		t.Fatalf("rpc call = %s %v, want getBlockHeight at finalized commitment", gotMethod, gotCommitment)
	}

	failing := statusRPCServer(t, http.StatusOK, map[string]any{"error": map[string]string{"message": "node is behind"}})
	if _, err := failing.FinalizedBlockHeight(context.Background()); err == nil {
		t.Fatal("FinalizedBlockHeight succeeded on rpc error, want error")
	}
}
