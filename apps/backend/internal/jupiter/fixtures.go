package jupiter

import "fmt"

// FixtureJupiterNoRoute returns Jupiter v2 JSON with an empty route plan.
func FixtureJupiterNoRoute(outputMint string) []byte {
	return []byte(fmt.Sprintf(`{
  "inputMint": "%s",
  "outputMint": "%s",
  "inAmount": "1000000",
  "outAmount": "0",
  "routePlan": [],
  "requestId": "req-no-route"
}`, USDCMint, outputMint))
}

// FixtureJupiterSuccessResponse returns Jupiter v2 JSON with a single-hop route.
func FixtureJupiterSuccessResponse(outputMint string) []byte {
	return []byte(fmt.Sprintf(`{
  "inputMint": "%s",
  "outputMint": "%s",
  "inAmount": "1000000",
  "outAmount": "500000",
  "transaction": "dGVzdC11bnNpZ25lZC10eA==",
  "routePlan": [
    {
      "swapInfo": {
        "ammKey": "AvBSC1KmFNceHpD6jyyXBV6gMXFxZ8BJJ3HVUN8kCurJ",
        "label": "Meteora DLMM",
        "inputMint": "%s",
        "outputMint": "%s",
        "inAmount": "1000000",
        "outAmount": "500000"
      },
      "percent": 100,
      "bps": 10000
    }
  ],
  "requestId": "req-success"
}`, USDCMint, outputMint, USDCMint, outputMint))
}

// FixtureJupiterExecuteSuccess returns Jupiter /execute success JSON.
func FixtureJupiterExecuteSuccess(signature string) []byte {
	if signature == "" {
		signature = "test-swap-signature"
	}
	return []byte(fmt.Sprintf(`{
  "status": "Success",
  "code": 0,
  "signature": "%s",
  "inputAmountResult": "1000000",
  "outputAmountResult": "500000",
  "totalOutputAmount": "500000"
}`, signature))
}

// FixtureJupiterExecuteFailure returns Jupiter /execute terminal failure JSON.
func FixtureJupiterExecuteFailure(signature string) []byte {
	if signature == "" {
		signature = "test-failed-signature"
	}
	return []byte(fmt.Sprintf(`{
  "status": "Failed",
  "code": -1000,
  "signature": "%s",
  "error": "failed to land"
}`, signature))
}

// FixtureJupiterExecutePendingSuccessCode returns Success with non-zero code.
func FixtureJupiterExecutePendingSuccessCode() []byte {
	return []byte(`{
  "status": "Success",
  "code": -1,
  "signature": "pending-signature",
  "error": "missing cached order"
}`)
}
