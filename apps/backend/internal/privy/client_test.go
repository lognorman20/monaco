package privy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		PrivyAppID:              "test-app-id",
		PrivyAppSecret:          "test-app-secret",
		PrivyAuthorizationKeyID: "j2ygtljjgxmn5tzao5vjov1t",
	}
}

func TestHTTPClient_EnsureMemberWallet_createsSolanaWallet(t *testing.T) {
	// Arrange
	var gotAuth string
	var gotAppID string
	var gotBody createWalletRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/wallets" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotAppID = r.Header.Get("privy-app-id")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(createWalletResponse{
			ID:      "wallet-member-1",
			Address: "So11111111111111111111111111111111111111112",
		})
	}))
	defer server.Close()

	client := NewHTTPClientWithTransport(testConfig(), server.URL, server.Client().Transport)
	ctx := context.Background()

	// Act
	ref, err := client.EnsureMemberWallet(ctx, "did:privy:test-user", UserID("user-uuid-1"))

	// Assert
	if err != nil {
		t.Fatalf("EnsureMemberWallet: %v", err)
	}
	if ref.PrivyWalletID != "wallet-member-1" {
		t.Fatalf("PrivyWalletID = %q", ref.PrivyWalletID)
	}
	if ref.SolanaAddress != "So11111111111111111111111111111111111111112" {
		t.Fatalf("SolanaAddress = %q", ref.SolanaAddress)
	}
	if ref.UserID != UserID("user-uuid-1") {
		t.Fatalf("UserID = %q", ref.UserID)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotAppID != "test-app-id" {
		t.Fatalf("privy-app-id = %q", gotAppID)
	}
	if gotBody.ChainType != "solana" {
		t.Fatalf("chain_type = %q", gotBody.ChainType)
	}
	if gotBody.Owner == nil || gotBody.Owner.UserID != "did:privy:test-user" {
		t.Fatalf("owner = %#v", gotBody.Owner)
	}
	if gotBody.ExternalID != "user-uuid-1" {
		t.Fatalf("external_id = %q", gotBody.ExternalID)
	}
	if len(gotBody.AdditionalSigners) != 1 {
		t.Fatalf("additional_signers = %#v, want one signer", gotBody.AdditionalSigners)
	}
	if gotBody.AdditionalSigners[0].SignerID != "j2ygtljjgxmn5tzao5vjov1t" {
		t.Fatalf("signer_id = %q", gotBody.AdditionalSigners[0].SignerID)
	}
}

func TestHTTPClient_EnsureMemberWallet_returnsErrorWhenPrivateKeySetWithoutKeyID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request when KEY_ID missing with private key set")
	}))
	defer server.Close()

	client := NewHTTPClientWithTransport(&config.Config{
		PrivyAppID:                   "test-app-id",
		PrivyAppSecret:               "test-app-secret",
		PrivyAuthorizationPrivateKey: "wallet-auth:test-authorization-key",
		PrivyAuthorizationKeyID:      "",
		SolanaCluster:                "mainnet-beta",
	}, server.URL, server.Client().Transport)

	_, err := client.EnsureMemberWallet(context.Background(), "did:privy:test-user", UserID("user-uuid-2"))
	if err == nil {
		t.Fatal("expected error when authorization private key is set without KEY_ID")
	}
}

func TestHTTPClient_EnsureMemberWallet_omitsAdditionalSignersWhenKeyIDUnset(t *testing.T) {
	// Arrange
	var gotBody createWalletRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(createWalletResponse{
			ID:      "wallet-member-2",
			Address: "So11111111111111111111111111111111111111112",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		PrivyAppID:     "test-app-id",
		PrivyAppSecret: "test-app-secret",
	}
	client := NewHTTPClientWithTransport(cfg, server.URL, server.Client().Transport)

	// Act
	_, err := client.EnsureMemberWallet(context.Background(), "did:privy:test-user", UserID("user-uuid-2"))

	// Assert
	if err != nil {
		t.Fatalf("EnsureMemberWallet: %v", err)
	}
	if len(gotBody.AdditionalSigners) != 0 {
		t.Fatalf("additional_signers = %#v, want empty", gotBody.AdditionalSigners)
	}
}

func TestHTTPClient_EnsureTreasury_createsServerWallet(t *testing.T) {
	// Arrange
	var gotBody createWalletRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(createWalletResponse{
			ID:      "wallet-treasury-1",
			Address: "So22222222222222222222222222222222222222222",
		})
	}))
	defer server.Close()

	client := NewHTTPClientWithTransport(testConfig(), server.URL, server.Client().Transport)

	// Act
	ref, err := client.EnsureTreasury(context.Background(), GroupID("group-uuid-1"))

	// Assert
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	if ref.PrivyWalletID != "wallet-treasury-1" {
		t.Fatalf("PrivyWalletID = %q", ref.PrivyWalletID)
	}
	if ref.GroupID != GroupID("group-uuid-1") {
		t.Fatalf("GroupID = %q", ref.GroupID)
	}
	if gotBody.Owner != nil {
		t.Fatalf("expected no owner for treasury, got %#v", gotBody.Owner)
	}
	if gotBody.ExternalID != "group-uuid-1" {
		t.Fatalf("external_id = %q", gotBody.ExternalID)
	}
}

func TestHTTPClient_VerifySession_emptyTokenReturnsInvalid(t *testing.T) {
	// Arrange
	client := NewHTTPClient(testConfig())

	// Act
	_, err := client.VerifySession(context.Background(), AccessToken(""))

	// Assert
	if err != ErrInvalidToken {
		t.Fatalf("err = %v, want %v", err, ErrInvalidToken)
	}
}
