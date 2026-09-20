package dex

import (
	"encoding/json"
	"math/big"
)

// Quote is a DEX aggregator quote for a token swap.
type Quote struct {
	TokenIn      string
	TokenOut     string
	AmountIn     *big.Int
	AmountOut    *big.Int
	Routable     bool
	RouteSummary json.RawMessage
}

// SwapCall is calldata to execute a quoted swap.
type SwapCall struct {
	Router       string
	Data         []byte
	AmountOutMin *big.Int
}
