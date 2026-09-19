package privy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseSPLTokenBalances_skipsZeroAndParsesMintAmount(t *testing.T) {
	t.Parallel()

	body := []byte(`{
  "jsonrpc": "2.0",
  "result": {
    "value": [
      {
        "account": {
          "data": {
            "parsed": {
              "info": {
                "mint": "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
                "tokenAmount": { "amount": "1500000" }
              }
            }
          }
        }
      },
      {
        "account": {
          "data": {
            "parsed": {
              "info": {
                "mint": "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp",
                "tokenAmount": { "amount": "0" }
              }
            }
          }
        }
      },
      {
        "account": {
          "data": {
            "parsed": {
              "info": {
                "mint": "XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB",
                "tokenAmount": { "amount": "4476500000" }
              }
            }
          }
        }
      }
    ]
  }
}`)

	balances, err := parseSPLTokenBalances(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(balances) != 2 {
		t.Fatalf("len = %d, want 2", len(balances))
	}
	if balances[0].Mint != "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" || balances[0].Amount != 1_500_000 {
		t.Fatalf("usdc balance = %+v", balances[0])
	}
	if balances[1].Amount != 4_476_500_000 {
		t.Fatalf("xstock balance = %+v", balances[1])
	}
}

// Privy's indexed wallet balance lags chain state by seconds after a swap. A stale treasury
// balance inflates pot NAV and lets redeem broadcast a payout the treasury cannot cover, which
// fails simulation with Custom:1, so treasury reads must come from Solana RPC.
func TestHTTPClient_TreasuryUSDCBalance_readsChainNotPrivyIndex(t *testing.T) {
	var rpcCalls int
	var indexCalls int
	var gotCommitment string

	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rpcCalls++
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc body: %v", err)
		}
		if req.Method != "getTokenAccountsByOwner" {
			t.Fatalf("rpc method = %q, want getTokenAccountsByOwner", req.Method)
		}
		if len(req.Params) == 3 {
			if opts, ok := req.Params[2].(map[string]any); ok {
				gotCommitment, _ = opts["commitment"].(string)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"value":[{"account":{"data":{"parsed":{"info":{"mint":"` +
			usdcMintAddress + `","tokenAmount":{"amount":"1000000"}}}}}}]}}`))
	}))
	defer rpc.Close()

	privyAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/balance") {
			indexCalls++
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(walletBalanceResponse{
			Balances: []walletBalanceEntry{{RawValue: "2000000"}},
		})
	}))
	defer privyAPI.Close()

	cfg := testConfig()
	cfg.SolanaRPCURL = rpc.URL
	client := NewHTTPClientWithTransport(cfg, privyAPI.URL, privyAPI.Client().Transport)

	balance, err := client.TreasuryUSDCBalance(context.Background(), "treasury-address")
	if err != nil {
		t.Fatalf("TreasuryUSDCBalance: %v", err)
	}
	if balance != 1_000_000 {
		t.Fatalf("balance = %d, want 1000000 (chain), not the stale indexed 2000000", balance)
	}
	if rpcCalls == 0 {
		t.Fatal("expected the treasury balance to come from Solana RPC")
	}
	if indexCalls != 0 {
		t.Fatalf("privy indexed balance endpoint called %d times, want 0", indexCalls)
	}
	if gotCommitment != "confirmed" {
		t.Fatalf("commitment = %q, want confirmed", gotCommitment)
	}
}
