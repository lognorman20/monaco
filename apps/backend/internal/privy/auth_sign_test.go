package privy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAuthorizationPrivateKey(t *testing.T) string {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	return "wallet-auth:" + base64.StdEncoding.EncodeToString(pkcs8)
}

func TestSignAuthorizationPayload_producesStableSignature(t *testing.T) {
	// Arrange
	authKey := testAuthorizationPrivateKey(t)
	body := map[string]any{
		"method": "signAndSendTransaction",
		"caip2":  solanaMainnetCAIP2,
		"params": map[string]any{
			"transaction": "AQID",
			"encoding":    "base64",
		},
	}
	payload := buildAuthorizationSignaturePayload(
		"POST",
		"https://api.privy.io/v1/wallets/wallet-1/rpc",
		body,
		map[string]string{"privy-app-id": "test-app-id"},
	)

	// Act
	signature, err := signAuthorizationPayload(authKey, payload)
	if err != nil {
		t.Fatalf("signAuthorizationPayload: %v", err)
	}
	valid, err := verifyAuthorizationSignature(authKey, signature, payload)
	if err != nil {
		t.Fatalf("verifyAuthorizationSignature: %v", err)
	}

	// Assert
	if signature == "" {
		t.Fatal("expected non-empty signature")
	}
	if !valid {
		t.Fatal("expected signature to verify against payload")
	}
}

func TestHTTPClient_signAndSend_includesAuthorizationSignatureHeader(t *testing.T) {
	// Arrange
	authKey := testAuthorizationPrivateKey(t)
	cfg := testConfig()
	cfg.PrivyAuthorizationPrivateKey = authKey

	var gotSignature string
	var gotRPC walletRPCRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rpc") {
			gotSignature = r.Header.Get(authorizationSignatureHeader)
			if err := json.NewDecoder(r.Body).Decode(&gotRPC); err != nil {
				t.Fatalf("decode rpc body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(walletRPCResponse{
				Data: walletRPCData{Hash: "SWEEPHTTPsig123"},
			})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewHTTPClientWithTransport(cfg, server.URL, server.Client().Transport)

	// Act
	hash, err := client.signAndSendSolanaTransaction(context.Background(), "wallet-member-http", "AQID")
	if err != nil {
		t.Fatalf("signAndSendSolanaTransaction: %v", err)
	}

	// Assert
	if hash != "SWEEPHTTPsig123" {
		t.Fatalf("hash = %q", hash)
	}
	if gotSignature == "" {
		t.Fatal("expected privy-authorization-signature header")
	}
	if gotRPC.Method != "signAndSendTransaction" {
		t.Fatalf("rpc method = %q", gotRPC.Method)
	}

	bodyMap, err := bodyMapForAuthorization(mustMarshal(t, gotRPC))
	if err != nil {
		t.Fatalf("bodyMapForAuthorization: %v", err)
	}
	payload := buildAuthorizationSignaturePayload(
		"POST",
		authorizationSignatureURL(server.URL, "/v1/wallets/wallet-member-http/rpc"),
		bodyMap,
		map[string]string{"privy-app-id": cfg.PrivyAppID},
	)
	valid, err := verifyAuthorizationSignature(authKey, gotSignature, payload)
	if err != nil {
		t.Fatalf("verifyAuthorizationSignature: %v", err)
	}
	if !valid {
		t.Fatalf("authorization signature did not verify")
	}
}

func TestHTTPClient_signAndSend_missingAuthorizationKey_errorsBeforeHTTP(t *testing.T) {
	// Arrange
	rpcCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/rpc") {
			rpcCalled = true
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewHTTPClientWithTransport(testConfig(), server.URL, server.Client().Transport)

	// Act
	_, err := client.signAndSendSolanaTransaction(context.Background(), "wallet-member-http", "AQID")

	// Assert
	if err == nil {
		t.Fatal("expected error for missing authorization key")
	}
	if rpcCalled {
		t.Fatal("wallet rpc should not be called without authorization key")
	}
}

func mustMarshal(t *testing.T, value any) []byte {
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return payload
}
