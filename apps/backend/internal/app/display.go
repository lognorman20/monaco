package app

import (
	"fmt"
	"math"
	"strings"
)

func formatPercentReturnDecimal(ratio float64) *string {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return nil
	}
	formatted := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", ratio), "0"), ".")
	if formatted == "" || formatted == "-0" {
		zero := "0"
		return &zero
	}
	return &formatted
}

func formatSignedDollarPnL(micros int64) string {
	sign := "+"
	amount := micros
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	return fmt.Sprintf("%s%.2f", sign, float64(amount)/1_000_000.0)
}
