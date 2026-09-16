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
