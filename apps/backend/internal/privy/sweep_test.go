package privy

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubmitSweep_callsPrivyClientWithMemberAndTreasuryAddresses(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	req := SweepRequest{
		MemberAddress:   "FAKEmember123",
		TreasuryAddress: "FAKEtreasury456",
		Amount:          1_000_000,
		RelayerKey:      "relayer-key",
	}

	// Act
	result, err := client.SubmitSweep(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("SubmitSweep: %v", err)
	}
	if result.TxSignature == "" {
		t.Fatal("expected tx signature")
	}
	last, ok := LastSweepRequest(client)
	if !ok {
		t.Fatal("expected last sweep request")
	}
	if last.MemberAddress != req.MemberAddress {
		t.Fatalf("member = %q, want %q", last.MemberAddress, req.MemberAddress)
	}
	if last.TreasuryAddress != req.TreasuryAddress {
		t.Fatalf("treasury = %q, want %q", last.TreasuryAddress, req.TreasuryAddress)
	}
}

func TestSubmitSweep_includesRelayerAsFeePayer(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	req, err := BuildSweepRequest("FAKEmember", "FAKEtreasury", 500_000, "relayer-fee-payer-key")
	if err != nil {
		t.Fatalf("BuildSweepRequest: %v", err)
	}

	// Act
	_, err = client.SubmitSweep(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("SubmitSweep: %v", err)
	}
	last, ok := LastSweepRequest(client)
	if !ok {
		t.Fatal("expected last sweep request")
	}
	if last.RelayerKey != "relayer-fee-payer-key" {
		t.Fatalf("relayer key = %q, want relayer-fee-payer-key", last.RelayerKey)
	}
}

func TestHTTPClient_SubmitSweep_callsPrivyWithMemberAndTreasuryAddresses(t *testing.T) {
	// Arrange
	memberSeed := sha256.Sum256([]byte("member-wallet"))
	memberPriv := ed25519.NewKeyFromSeed(memberSeed[:])
	memberPub := memberPriv.Public().(ed25519.PublicKey)
	memberAddress := encodeBase58(memberPub)

	treasurySeed := sha256.Sum256([]byte("treasury-wallet"))
	treasuryPub := ed25519.NewKeyFromSeed(treasurySeed[:]).Public().(ed25519.PublicKey)
	treasuryAddress := encodeBase58(treasuryPub)

	relayerSeed := sha256.Sum256([]byte("relayer-wallet"))
	relayerPriv := ed25519.NewKeyFromSeed(relayerSeed[:])
	relayerKey := encodeBase58(relayerPriv)

	var gotAddress string
	var gotWalletID string
	var gotRPC walletRPCRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/wallets/address":
			var lookup getWalletByAddressRequest
			if err := json.NewDecoder(r.Body).Decode(&lookup); err != nil {
				t.Fatalf("decode address lookup: %v", err)
			}
			gotAddress = lookup.Address
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(walletResponse{
				ID:      "wallet-member-http",
				Address: memberAddress,
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rpc"):
			gotWalletID = strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/rpc"), "/v1/wallets/")
			if err := json.NewDecoder(r.Body).Decode(&gotRPC); err != nil {
				t.Fatalf("decode rpc body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(walletRPCResponse{
				Data: walletRPCData{Hash: "SWEEPHTTPsig123"},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewHTTPClientWithTransport(testConfig(), server.URL, server.Client().Transport)
	req := SweepRequest{
		MemberAddress:   memberAddress,
		TreasuryAddress: treasuryAddress,
		Amount:          1_000_000,
		RelayerKey:      relayerKey,
	}

	// Act
	result, err := client.SubmitSweep(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("SubmitSweep: %v", err)
	}
	if result.TxSignature != "SWEEPHTTPsig123" {
		t.Fatalf("signature = %q", result.TxSignature)
	}
	if gotAddress != memberAddress {
		t.Fatalf("lookup address = %q, want %q", gotAddress, memberAddress)
	}
	if gotWalletID != "wallet-member-http" {
		t.Fatalf("wallet id = %q", gotWalletID)
	}
	if gotRPC.Method != "signAndSendTransaction" {
		t.Fatalf("rpc method = %q", gotRPC.Method)
	}
	if gotRPC.CAIP2 != solanaMainnetCAIP2 {
		t.Fatalf("caip2 = %q", gotRPC.CAIP2)
	}
	if gotRPC.Params.Encoding != "base64" || gotRPC.Params.Transaction == "" {
		t.Fatal("expected base64 transaction payload")
	}
}

func TestHTTPClient_MemberUSDCBalance_readsPrivyBalance(t *testing.T) {
	// Arrange
	balanceSeed := sha256.Sum256([]byte("balance-member"))
	address := encodeBase58(ed25519.NewKeyFromSeed(balanceSeed[:]).Public().(ed25519.PublicKey))
	var gotWalletID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/wallets/address":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(walletResponse{ID: "wallet-balance", Address: address})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/balance"):
			gotWalletID = strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/balance"), "/v1/wallets/")
			if !strings.Contains(r.URL.RawQuery, usdcMintAddress) {
				t.Fatalf("balance query = %q", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(walletBalanceResponse{
				Balances: []walletBalanceEntry{{RawValue: "2500000"}},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewHTTPClientWithTransport(testConfig(), server.URL, server.Client().Transport)

	// Act
	balance, err := client.MemberUSDCBalance(context.Background(), address)

	// Assert
	if err != nil {
		t.Fatalf("MemberUSDCBalance: %v", err)
	}
	if balance != 2_500_000 {
		t.Fatalf("balance = %d, want 2500000", balance)
	}
	if gotWalletID != "wallet-balance" {
		t.Fatalf("wallet id = %q", gotWalletID)
	}
}
