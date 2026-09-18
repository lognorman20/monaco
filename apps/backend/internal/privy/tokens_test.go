package privy

import "testing"

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
