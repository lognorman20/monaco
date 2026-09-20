package privy

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sweepTestServer fakes Solana RPC (getLatestBlockhash) and Privy (wallet lookup, wallet rpc).
// rpcStatus/rpcBody control the signAndSendTransaction response.
func sweepTestServer(t *testing.T, memberAddress string, rpcStatus int, rpcBody any, rpcCalls *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]any{
					"value": map[string]any{
						"blockhash":            "EkSnNWid2cvATRWXHtQqy3C6xToBFWtBiCBMuMhvv2Sv",
						"lastValidBlockHeight": 123_456,
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/wallets/address":
			_ = json.NewEncoder(w).Encode(walletResponse{ID: "wallet-member-http", Address: memberAddress})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rpc"):
			*rpcCalls++
			w.WriteHeader(rpcStatus)
			_ = json.NewEncoder(w).Encode(rpcBody)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
}

func sweepTestClient(t *testing.T, server *httptest.Server) *HTTPClient {
	t.Helper()
	cfg := testConfig()
	cfg.PrivyAuthorizationPrivateKey = testAuthorizationPrivateKey(t)
	client := NewHTTPClientWithTransport(cfg, server.URL, server.Client().Transport)
	client.solanaRPCURL = server.URL + "/"
	return client
}

func TestHTTPClient_PrepareSweep_returnsTransactionIDBeforeAnythingIsBroadcast(t *testing.T) {
	// Arrange
	req, keys := testSweepRequest(t)
	rpcCalls := 0
	server := sweepTestServer(t, req.MemberAddress, http.StatusOK, walletRPCResponse{}, &rpcCalls)
	defer server.Close()
	client := sweepTestClient(t, server)

	// Act
	prepared, err := client.PrepareSweep(context.Background(), req)

	// Assert: nothing sent, and the signature is the fee payer's signature over the message.
	if err != nil {
		t.Fatalf("PrepareSweep: %v", err)
	}
	if rpcCalls != 0 {
		t.Fatalf("wallet rpc calls during prepare = %d, want 0", rpcCalls)
	}
	if prepared.LastValidBlockHeight != 123_456 {
		t.Fatalf("last valid block height = %d, want 123456", prepared.LastValidBlockHeight)
	}
	if prepared.WalletID != "wallet-member-http" {
		t.Fatalf("wallet id = %q", prepared.WalletID)
	}
	raw, err := base64.StdEncoding.DecodeString(prepared.TransactionBase64)
	if err != nil {
		t.Fatalf("decode transaction: %v", err)
	}
	sigCount, offset, err := decodeCompactU16(raw)
	if err != nil || sigCount != 2 {
		t.Fatalf("signature count = %d err = %v, want 2", sigCount, err)
	}
	feePayerSig := raw[offset : offset+ed25519.SignatureSize]
	message := raw[offset+sigCount*ed25519.SignatureSize:]
	if prepared.TxSignature != encodeBase58(feePayerSig) {
		t.Fatalf("tx signature = %q, want first transaction signature %q", prepared.TxSignature, encodeBase58(feePayerSig))
	}
	if !ed25519.Verify(keys.relayer.Public().(ed25519.PublicKey), message, feePayerSig) {
		t.Fatal("tx signature is not the relayer's signature over the transaction message")
	}

	// The same inputs on the same blockhash give the same id: it is a function of the message.
	again, err := client.PrepareSweep(context.Background(), req)
	if err != nil {
		t.Fatalf("PrepareSweep again: %v", err)
	}
	if again.TxSignature != prepared.TxSignature {
		t.Fatalf("tx signature not deterministic: %q vs %q", again.TxSignature, prepared.TxSignature)
	}
}

func TestHTTPClient_PrepareSweep_invalidInputIsADefiniteRejection(t *testing.T) {
	// Arrange
	req, _ := testSweepRequest(t)
	rpcCalls := 0
	server := sweepTestServer(t, req.MemberAddress, http.StatusOK, walletRPCResponse{}, &rpcCalls)
	defer server.Close()
	client := sweepTestClient(t, server)
	req.RelayerKey = "not-a-key"

	// Act
	_, err := client.PrepareSweep(context.Background(), req)

	// Assert
	if !errors.Is(err, ErrBroadcastRejected) {
		t.Fatalf("err = %v, want ErrBroadcastRejected", err)
	}
}

func TestHTTPClient_BroadcastSweep_classifiesPrivyFailures(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		wantRejected bool
	}{
		{"policy violation is definite", http.StatusBadRequest, true},
		{"unauthorized is definite", http.StatusUnauthorized, true},
		{"rate limited may be retried", http.StatusTooManyRequests, false},
		{"request timeout is ambiguous", http.StatusRequestTimeout, false},
		{"server error is ambiguous", http.StatusBadGateway, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			req, _ := testSweepRequest(t)
			rpcCalls := 0
			server := sweepTestServer(t, req.MemberAddress, tc.status, map[string]string{"error": "nope"}, &rpcCalls)
			defer server.Close()
			client := sweepTestClient(t, server)
			prepared, err := client.PrepareSweep(context.Background(), req)
			if err != nil {
				t.Fatalf("PrepareSweep: %v", err)
			}

			// Act
			_, err = client.BroadcastSweep(context.Background(), prepared)

			// Assert
			if err == nil {
				t.Fatal("BroadcastSweep succeeded, want error")
			}
			if got := errors.Is(err, ErrBroadcastRejected); got != tc.wantRejected {
				t.Fatalf("errors.Is(err, ErrBroadcastRejected) = %v, want %v (err: %v)", got, tc.wantRejected, err)
			}
			if rpcCalls != 1 {
				t.Fatalf("wallet rpc calls = %d, want 1", rpcCalls)
			}
		})
	}
}

func TestHTTPClient_BroadcastSweep_sendsThePreparedTransaction(t *testing.T) {
	// Arrange
	req, _ := testSweepRequest(t)
	rpcCalls := 0
	server := sweepTestServer(t, req.MemberAddress, http.StatusOK, walletRPCResponse{Data: walletRPCData{Hash: "SWEEPHTTPsig123"}}, &rpcCalls)
	defer server.Close()
	client := sweepTestClient(t, server)
	prepared, err := client.PrepareSweep(context.Background(), req)
	if err != nil {
		t.Fatalf("PrepareSweep: %v", err)
	}

	// Act
	result, err := client.BroadcastSweep(context.Background(), prepared)

	// Assert
	if err != nil {
		t.Fatalf("BroadcastSweep: %v", err)
	}
	if result.TxSignature != "SWEEPHTTPsig123" || rpcCalls != 1 {
		t.Fatalf("result = %+v rpcCalls = %d", result, rpcCalls)
	}
	if _, err := client.BroadcastSweep(context.Background(), PreparedSweep{}); !errors.Is(err, ErrBroadcastRejected) {
		t.Fatalf("unprepared BroadcastSweep err = %v, want ErrBroadcastRejected", err)
	}
}

func TestFakeClient_PrepareSweep_movesNoFundsUntilBroadcast(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	sweeps := client.(SweepClient)
	SetMemberUSDCBalance(client, "FAKEmember", 1_000_000)
	req := SweepRequest{MemberAddress: "FAKEmember", TreasuryAddress: "FAKEtreasury", Amount: 1_000_000, RelayerKey: "relayer-key"}

	// Act
	prepared, err := sweeps.PrepareSweep(context.Background(), req)
	if err != nil {
		t.Fatalf("PrepareSweep: %v", err)
	}

	// Assert
	if balance, _ := client.MemberUSDCBalance(context.Background(), "FAKEmember"); balance != 1_000_000 {
		t.Fatalf("balance after prepare = %d, want untouched", balance)
	}
	if SweepCount(client) != 0 {
		t.Fatal("prepare counted as a sweep")
	}
	result, err := sweeps.BroadcastSweep(context.Background(), prepared)
	if err != nil {
		t.Fatalf("BroadcastSweep: %v", err)
	}
	if result.TxSignature != prepared.TxSignature || SweepCount(client) != 1 {
		t.Fatalf("result = %+v sweeps = %d", result, SweepCount(client))
	}
}
