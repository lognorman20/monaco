package app

import (
	"fmt"
	"math/big"

	"github.com/monaco/monaco/apps/backend/internal/dex"
)

func dexAmountOutMicros(q dex.Quote) (int64, error) {
	if q.AmountOut == nil {
		return 0, fmt.Errorf("missing amount out")
	}
	if !q.AmountOut.IsInt64() {
		return 0, fmt.Errorf("amount out overflow")
	}
	return q.AmountOut.Int64(), nil
}

func bigIntFromDecimalString(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return big.NewInt(0)
	}
	return n
}
