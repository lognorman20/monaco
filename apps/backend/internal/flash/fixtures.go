package flash

import "fmt"

// FixtureQuoteReady returns Flash /quote JSON for a funder whose token accounts and
// delegation already exist: only the offchain order signature is needed.
func FixtureQuoteReady(side, targetAsset, contraAsset, orderMessage, deadline string) []byte {
	return []byte(fmt.Sprintf(`{
  "quoteId": "quote-ready",
  "bridgeQuoteId": null,
  "orderType": "market",
  "side": "%s",
  "targetAsset": "%s",
  "contraAsset": "%s",
  "from": {"asset": "contra", "amount": "5", "notional": "5"},
  "to": {"asset": "target", "amount": "0.01486083", "notional": "4.98672"},
  "fees": {"estimatedFeeNotional": "0.007498"},
  "estimatedPriceImpact": "0.0011582",
  "recommendedSlippage": "0.15",
  "wrap": null,
  "evm": null,
  "svm": {
    "ataSetupIxs": null,
    "delegateIx": null,
    "sponsoredDelegateTx": null,
    "orderMessage": "%s",
    "nonce": "932385860354111",
    "deadline": "%s"
  },
  "setupTxs": null
}`, side, targetAsset, contraAsset, orderMessage, deadline))
}

// FixtureQuoteNeedsSetup returns Flash /quote JSON for a first trade: a Token-2022
// create-associated-token-account instruction plus an SPL Approve to the Flash program.
func FixtureQuoteNeedsSetup(funder, targetAsset, contraAsset string) []byte {
	return []byte(fmt.Sprintf(`{
  "quoteId": "quote-needs-setup",
  "side": "buy",
  "targetAsset": "%[2]s",
  "contraAsset": "%[3]s",
  "from": {"asset": "contra", "amount": "5", "notional": "5"},
  "to": {"asset": "target", "amount": "0.01486083", "notional": "4.98672"},
  "svm": {
    "ataSetupIxs": [{
      "programId": "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL",
      "accounts": [
        {"pubkey": "%[1]s", "isSigner": true, "isWritable": true},
        {"pubkey": "DK2ZeJzewxjESATTTwQwh7GNx3QxhWbbLpqcgPVGryot", "isSigner": false, "isWritable": true},
        {"pubkey": "%[1]s", "isSigner": false, "isWritable": false},
        {"pubkey": "%[2]s", "isSigner": false, "isWritable": false},
        {"pubkey": "11111111111111111111111111111111", "isSigner": false, "isWritable": false},
        {"pubkey": "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb", "isSigner": false, "isWritable": false}
      ],
      "data": "2"
    }],
    "delegateIx": {
      "programId": "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
      "accounts": [
        {"pubkey": "FGETo8T8wMcN2wCjav8VK6eh3dLk63evNDPxzLSJra8B", "isSigner": false, "isWritable": true},
        {"pubkey": "3jBWeQrnfEhw5LY53hwcoYKQJsrTUbtLbmGrNYCr1Fiq", "isSigner": false, "isWritable": false},
        {"pubkey": "%[1]s", "isSigner": true, "isWritable": false}
      ],
      "data": "4h6bzpF8MKT4"
    },
    "sponsoredDelegateTx": null,
    "orderMessage": "DFS|m=%[3]s|t=5000000|h=5UkgEbSVUhBHyPYDpDDpQY",
    "nonce": "932385860354111",
    "deadline": "1789861368"
  }
}`, funder, targetAsset, contraAsset))
}

// FixtureQuoteUnknownAsset returns Flash's 400 body for a mint it does not index.
func FixtureQuoteUnknownAsset(asset string) []byte {
	return []byte(fmt.Sprintf(`{"error":{"code":"INVALID_ARGUMENT","message":"flash quote failed: resolve to asset: asset \"%s\" not found on chain \"solana\""}}`, asset))
}

// FixtureUnauthorized returns Flash's 401 body for a missing or unknown API key.
func FixtureUnauthorized() []byte {
	return []byte(`{"error":"API key required"}`)
}

// FixtureOrderFilled returns GET /orders/{orderId} JSON for a filled market buy.
func FixtureOrderFilled(orderID, txSignature string) []byte {
	return []byte(fmt.Sprintf(`{
  "order": {
    "orderId": "%s",
    "orderType": "market",
    "side": "buy",
    "status": "ORDER_STATUS_FILLED",
    "closeReason": "REASON_FULLY_FILLED",
    "filled": {"targetAmount": "0.01486083", "contraAmount": "5", "averagePrice": "336.45", "averageNotionalPrice": "336.45"}
  },
  "fills": [{"status": "CHAIN_STATUS_FINALIZED", "transactionId": "%s", "targetAmount": "0.01486083", "contraAmount": "5"}]
}`, orderID, txSignature))
}

// FixtureOrderRejected returns GET /orders/{orderId} JSON for an order Flash closed unfilled.
func FixtureOrderRejected(orderID string) []byte {
	return []byte(fmt.Sprintf(`{
  "order": {
    "orderId": "%s",
    "status": "ORDER_STATUS_REJECTED",
    "closeReason": "REASON_UNSPECIFIED",
    "filled": null
  },
  "fills": []
}`, orderID))
}
