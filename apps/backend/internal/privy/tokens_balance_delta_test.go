package privy

import "testing"

func TestParseTokenBalanceDelta_postMinusPre(t *testing.T) {
	body := []byte(`{
	  "result": {
	    "meta": {
	      "preTokenBalances": [],
	      "postTokenBalances": [
	        {"owner": "owner1", "mint": "mintA", "uiTokenAmount": {"amount": "99800000000"}}
	      ]
	    }
	  }
	}`)
	delta, err := parseTokenBalanceDelta(body, "owner1", "mintA")
	if err != nil {
		t.Fatalf("parseTokenBalanceDelta: %v", err)
	}
	if delta != 99800000000 {
		t.Fatalf("delta = %d, want 99800000000", delta)
	}
}
