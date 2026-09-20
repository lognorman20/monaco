package privy

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// payoutRPCStub serves the Solana JSON-RPC and Privy wallet RPC a payout touches and records
// every method it saw, in order.
type payoutRPCStub struct {
	t       *testing.T
	methods []string
	// solana maps a JSON-RPC method to its successive `result` values; the last one repeats.
	solana map[string][]any
	// solanaErr maps a JSON-RPC method to an error message returned instead of a result.
	solanaErr map[string]string
	// treasuryKey co-signs signTransaction requests the way Privy would.
	treasuryKey ed25519.PrivateKey
	sentTx      string
}

func (s *payoutRPCStub) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/":
		var req solanaRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.t.Fatalf("decode solana rpc: %v", err)
		}
		s.methods = append(s.methods, req.Method)
		if req.Method == "sendTransaction" {
			s.sentTx, _ = req.Params[0].(string)
		}
		if msg, ok := s.solanaErr[req.Method]; ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32002, "message": msg}})
			return
		}
		results := s.solana[req.Method]
		if len(results) == 0 {
			s.t.Fatalf("unexpected solana rpc %q", req.Method)
		}
		result := results[0]
		if len(results) > 1 {
			s.solana[req.Method] = results[1:]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	case strings.HasSuffix(r.URL.Path, "/rpc"):
		var req signTransactionRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.t.Fatalf("decode privy rpc: %v", err)
		}
		s.methods = append(s.methods, "privy:"+req.Method)
		raw, err := base64.StdEncoding.DecodeString(req.Params.Transaction)
		if err != nil {
			s.t.Fatalf("decode unsigned tx: %v", err)
		}
		signed, err := signTransaction(raw, s.treasuryKey, 1)
		if err != nil {
			s.t.Fatalf("co-sign tx: %v", err)
		}
		_ = json.NewEncoder(w).Encode(walletRPCResponse{Data: walletRPCData{SignedTransaction: base64.StdEncoding.EncodeToString(signed)}})
	default:
		s.t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}
}

func newPayoutTestClient(t *testing.T, stub *payoutRPCStub) (*HTTPClient, PayUSDCRequest) {
	t.Helper()
	stub.t = t
	treasurySeed := sha256.Sum256([]byte("payout-treasury"))
	stub.treasuryKey = ed25519.NewKeyFromSeed(treasurySeed[:])
	relayerSeed := sha256.Sum256([]byte("payout-relayer"))
	recipientSeed := sha256.Sum256([]byte("payout-recipient"))

	server := httptest.NewServer(http.HandlerFunc(stub.handler))
	t.Cleanup(server.Close)

	cfg := testConfig()
	cfg.PrivyAuthorizationPrivateKey = testAuthorizationPrivateKey(t)
	cfg.RelayerPrivateKey = encodeBase58(ed25519.NewKeyFromSeed(relayerSeed[:]))
	client := NewHTTPClientWithTransport(cfg, server.URL, server.Client().Transport)
	client.solanaRPCURL = server.URL + "/"

	return client, PayUSDCRequest{
		TreasuryPrivyWalletID: "wallet-treasury",
		TreasuryAddress:       encodeBase58(stub.treasuryKey.Public().(ed25519.PublicKey)),
		ToAddress:             encodeBase58(ed25519.NewKeyFromSeed(recipientSeed[:]).Public().(ed25519.PublicKey)),
		Amount:                480_000,
	}
}

func latestBlockhashResult(lastValidBlockHeight int64) any {
	return map[string]any{"value": map[string]any{
		"blockhash":            "EkSnNWid2cvATRWXHtQqy3C6xToBFWtBiCBMuMhvv2Sv",
		"lastValidBlockHeight": lastValidBlockHeight,
	}}
}

func signatureStatusResult(confirmationStatus string, chainErr any) any {
	if confirmationStatus == "" {
		return map[string]any{"value": []any{nil}}
	}
	return map[string]any{"value": []any{map[string]any{"confirmationStatus": confirmationStatus, "err": chainErr}}}
}

// The signature has to be known, and nothing sent, when PrepareUSDCPayout returns: that is
// what lets the caller persist it before the broadcast.
func TestHTTPClient_PrepareUSDCPayout_signsWithoutBroadcasting(t *testing.T) {
	// Arrange
	stub := &payoutRPCStub{solana: map[string][]any{"getLatestBlockhash": {latestBlockhashResult(321)}}}
	client, req := newPayoutTestClient(t, stub)

	// Act
	prepared, err := client.PrepareUSDCPayout(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("PrepareUSDCPayout: %v", err)
	}
	if got := strings.Join(stub.methods, ","); got != "getLatestBlockhash,privy:signTransaction" {
		t.Fatalf("calls = %s, want a blockhash read and a sign with no send", got)
	}
	if prepared.LastValidBlockHeight != 321 {
		t.Fatalf("last valid block height = %d, want 321", prepared.LastValidBlockHeight)
	}
	raw, err := base64.StdEncoding.DecodeString(prepared.SignedTransaction)
	if err != nil {
		t.Fatalf("decode signed tx: %v", err)
	}
	if got := encodeBase58(raw[1 : 1+ed25519.SignatureSize]); got != prepared.TxSignature {
		t.Fatalf("tx signature = %q, want the fee payer signature %q", prepared.TxSignature, got)
	}
	treasurySignature := raw[1+ed25519.SignatureSize : 1+2*ed25519.SignatureSize]
	if !ed25519.Verify(stub.treasuryKey.Public().(ed25519.PublicKey), raw[1+2*ed25519.SignatureSize:], treasurySignature) {
		t.Fatal("signed tx does not carry a valid treasury signature")
	}
}

func TestHTTPClient_PrepareUSDCPayout_blockhashRPCError_signsNothing(t *testing.T) {
	// Arrange
	stub := &payoutRPCStub{solanaErr: map[string]string{"getLatestBlockhash": "node is behind"}}
	client, req := newPayoutTestClient(t, stub)

	// Act
	_, err := client.PrepareUSDCPayout(context.Background(), req)

	// Assert
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("err = %v, want ErrAPI", err)
	}
	if got := strings.Join(stub.methods, ","); got != "getLatestBlockhash" {
		t.Fatalf("calls = %s, want no signing after the blockhash failed", got)
	}
}

func TestHTTPClient_BroadcastUSDCPayout_sendsTheSignedTransaction(t *testing.T) {
	// Arrange
	stub := &payoutRPCStub{solana: map[string][]any{"sendTransaction": {"5sig"}}}
	client, _ := newPayoutTestClient(t, stub)

	// Act
	err := client.BroadcastUSDCPayout(context.Background(), PreparedPayout{TxSignature: "5sig", SignedTransaction: "c2lnbmVk", LastValidBlockHeight: 10})

	// Assert
	if err != nil {
		t.Fatalf("BroadcastUSDCPayout: %v", err)
	}
	if stub.sentTx != "c2lnbmVk" {
		t.Fatalf("sent tx = %q, want the recorded signed transaction", stub.sentTx)
	}
}

func TestHTTPClient_BroadcastUSDCPayout_rpcError_isReturned(t *testing.T) {
	// Arrange
	stub := &payoutRPCStub{solanaErr: map[string]string{"sendTransaction": "Blockhash not found"}}
	client, _ := newPayoutTestClient(t, stub)

	// Act
	err := client.BroadcastUSDCPayout(context.Background(), PreparedPayout{TxSignature: "5sig", SignedTransaction: "c2lnbmVk", LastValidBlockHeight: 10})

	// Assert
	if !errors.Is(err, ErrAPI) || !strings.Contains(err.Error(), "Blockhash not found") {
		t.Fatalf("err = %v, want the rpc error", err)
	}
}

func TestHTTPClient_USDCPayoutStatus(t *testing.T) {
	payout := PreparedPayout{TxSignature: "5sig", SignedTransaction: "c2lnbmVk", LastValidBlockHeight: 1_000}
	instructionErr := map[string]any{"InstructionError": []any{1, map[string]any{"Custom": 1}}}

	cases := []struct {
		name      string
		statuses  []any
		height    int64
		want      PayoutState
		wantCalls string
	}{
		{"confirmed settles", []any{signatureStatusResult("confirmed", nil)}, 0, PayoutStateConfirmed, "getSignatureStatuses"},
		{"finalized settles", []any{signatureStatusResult("finalized", nil)}, 0, PayoutStateConfirmed, "getSignatureStatuses"},
		{"landed with an error is failed", []any{signatureStatusResult("finalized", instructionErr)}, 0, PayoutStateFailed, "getSignatureStatuses"},
		{"processed can still be skipped", []any{signatureStatusResult("processed", nil)}, 0, PayoutStatePending, "getSignatureStatuses"},
		{"unseen with a live blockhash is pending", []any{signatureStatusResult("", nil)}, 1_000, PayoutStatePending, "getSignatureStatuses,getBlockHeight"},
		{"unseen past its blockhash is dropped", []any{signatureStatusResult("", nil)}, 1_001, PayoutStateDropped, "getSignatureStatuses,getBlockHeight,getSignatureStatuses"},
		{"landing in the last valid block beats the expiry", []any{signatureStatusResult("", nil), signatureStatusResult("finalized", nil)}, 1_001, PayoutStateConfirmed, "getSignatureStatuses,getBlockHeight,getSignatureStatuses"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			stub := &payoutRPCStub{solana: map[string][]any{"getSignatureStatuses": tc.statuses, "getBlockHeight": {tc.height}}}
			client, _ := newPayoutTestClient(t, stub)

			// Act
			status, err := client.USDCPayoutStatus(context.Background(), payout)

			// Assert
			if err != nil {
				t.Fatalf("USDCPayoutStatus: %v", err)
			}
			if status.State != tc.want {
				t.Fatalf("state = %q, want %q", status.State, tc.want)
			}
			if got := strings.Join(stub.methods, ","); got != tc.wantCalls {
				t.Fatalf("calls = %s, want %s", got, tc.wantCalls)
			}
			if tc.want == PayoutStateFailed && !strings.Contains(status.Reason, "Custom") {
				t.Fatalf("reason = %q, want the chain error", status.Reason)
			}
		})
	}
}

// An RPC outage is not a verdict: neither "dropped" nor "pending" may be inferred from it.
func TestHTTPClient_USDCPayoutStatus_rpcErrors_giveNoVerdict(t *testing.T) {
	payout := PreparedPayout{TxSignature: "5sig", SignedTransaction: "c2lnbmVk", LastValidBlockHeight: 1_000}

	for _, failing := range []string{"getSignatureStatuses", "getBlockHeight"} {
		t.Run(failing, func(t *testing.T) {
			// Arrange
			stub := &payoutRPCStub{
				solana:    map[string][]any{"getSignatureStatuses": {signatureStatusResult("", nil)}},
				solanaErr: map[string]string{failing: "rpc overloaded"},
			}
			client, _ := newPayoutTestClient(t, stub)

			// Act
			status, err := client.USDCPayoutStatus(context.Background(), payout)

			// Assert
			if !errors.Is(err, ErrAPI) || status.State != "" {
				t.Fatalf("status = %+v err = %v, want an error and no state", status, err)
			}
		})
	}
}
