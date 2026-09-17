package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHTTPSolanaRPC_constructsForCluster(t *testing.T) {
	// Arrange
	// Act
	rpc := NewHTTPSolanaRPC("mainnet-beta")

	// Assert
	if rpc == nil {
		t.Fatal("expected rpc client")
	}
	if rpc.endpoint != "https://api.mainnet-beta.solana.com" {
		t.Fatalf("endpoint = %q", rpc.endpoint)
	}
}

func TestHTTPSolanaRPC_IsConfirmed_returnsTrueForFinalizedStatus(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"value": []map[string]any{
					{
						"err":                 nil,
						"confirmationStatus": "finalized",
					},
				},
			},
		})
	}))
	defer server.Close()

	rpc := NewHTTPSolanaRPC("mainnet-beta")
	rpc.endpoint = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	confirmed, err := rpc.IsConfirmed(ctx, "sig-finalized")

	// Assert
	if err != nil {
		t.Fatalf("IsConfirmed: %v", err)
	}
	if !confirmed {
		t.Fatal("expected finalized signature to be confirmed")
	}
}

func TestHTTPSolanaRPC_GetBalance_returnsLamports(t *testing.T) {
	// Arrange
	const wantBalance uint64 = 600_000_000
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"value": wantBalance,
			},
		})
	}))
	defer server.Close()

	rpc := NewHTTPSolanaRPC("mainnet-beta")
	rpc.endpoint = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	got, err := rpc.GetBalance(ctx, "Relayer11111111111111111111111111111111111")

	// Assert
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if got != wantBalance {
		t.Fatalf("balance = %d, want %d", got, wantBalance)
	}
}
