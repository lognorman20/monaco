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

func TestHTTPSolanaRPC_GetSPLTokenBalance_sumsMatchingMintAccounts(t *testing.T) {
	const owner = "Relayer11111111111111111111111111111111111"
	const mint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"value": []map[string]any{
					{
						"account": map[string]any{
							"data": map[string]any{
								"parsed": map[string]any{
									"info": map[string]any{
										"mint": mint,
										"tokenAmount": map[string]any{
											"amount": "1500000",
										},
									},
								},
							},
						},
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

	got, err := rpc.GetSPLTokenBalance(ctx, owner, mint)
	if err != nil {
		t.Fatalf("GetSPLTokenBalance: %v", err)
	}
	if got != 1_500_000 {
		t.Fatalf("balance = %d, want %d", got, 1_500_000)
	}
}

func TestHTTPSolanaRPC_GetSPLTokenBalance_returnsZeroWhenNoAccounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"value": []any{},
			},
		})
	}))
	defer server.Close()

	rpc := NewHTTPSolanaRPC("mainnet-beta")
	rpc.endpoint = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := rpc.GetSPLTokenBalance(ctx, "Relayer11111111111111111111111111111111111", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	if err != nil {
		t.Fatalf("GetSPLTokenBalance: %v", err)
	}
	if got != 0 {
		t.Fatalf("balance = %d, want 0", got)
	}
}
