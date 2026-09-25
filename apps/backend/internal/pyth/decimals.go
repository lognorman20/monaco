package pyth

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/monaco/monaco/packages/domain"
)

func atomicScale(decimals int) int64 {
	scale := int64(1)
	for i := 0; i < decimals; i++ {
		scale *= 10
	}
	return scale
}

// NormalizeTokenDecimals treats 0 as xStock default (8).
func NormalizeTokenDecimals(decimals int) int {
	if decimals == 0 {
		return 8
	}
	return decimals
}

// EffectiveUiMultiplier resolves nil catalog/mint multipliers. Pre-IPO nil stays unresolved;
// listed stocks and xStocks treat nil as 1 (no scaled-ui extension).
func EffectiveUiMultiplier(mult *big.Rat, kind string) (*big.Rat, bool) {
	if mult != nil {
		if mult.Sign() <= 0 {
			return nil, false
		}
		return mult, true
	}
	if kind == "pre_ipo" {
		return nil, false
	}
	return big.NewRat(1, 1), true
}

// UiMultiplierDecimalString formats a multiplier for JSON (e.g. "5", "1.4861347").
func UiMultiplierDecimalString(mult *big.Rat) string {
	if mult == nil {
		return ""
	}
	if mult.IsInt() {
		return mult.Num().String()
	}
	s := strings.TrimRight(mult.FloatString(10), "0")
	return strings.TrimRight(s, ".")
}

// TokenAtomicsToDecimalUnits formats on-chain token atomics as a decimal unit string.
func TokenAtomicsToDecimalUnits(atomics int64, decimals int) (domain.ShareUnits, error) {
	return TokenAtomicsToScaledDecimalUnits(atomics, decimals, big.NewRat(1, 1), "stock")
}

// TokenAtomicsToScaledDecimalUnits formats raw atomics as scaled decimal units (raw × multiplier / 10^decimals).
func TokenAtomicsToScaledDecimalUnits(atomics int64, decimals int, uiMultiplier *big.Rat, kind string) (domain.ShareUnits, error) {
	decimals = NormalizeTokenDecimals(decimals)
	if atomics < 0 {
		return "", fmt.Errorf("token atomics must be non-negative")
	}
	mult, ok := EffectiveUiMultiplier(uiMultiplier, kind)
	if !ok {
		return "", fmt.Errorf("ui multiplier unresolved")
	}
	if atomics == 0 {
		return domain.ShareUnits("0"), nil
	}
	scale := atomicScale(decimals)
	num := new(big.Int).SetInt64(atomics)
	num.Mul(num, mult.Num())
	den := new(big.Int).SetInt64(scale)
	den.Mul(den, mult.Denom())
	r := new(big.Rat).SetFrac(num, den)
	s := strings.TrimRight(r.FloatString(decimals), "0")
	s = strings.TrimRight(s, ".")
	return domain.ShareUnits(s), nil
}

// CostBasisMarkPerUnitMicros derives USDC micros per scaled unit from fill totals.
func CostBasisMarkPerUnitMicros(totalUSDCMicros, tokenAtomics int64, decimals int, uiMultiplier *big.Rat, kind string) (int64, error) {
	decimals = NormalizeTokenDecimals(decimals)
	if totalUSDCMicros < 0 {
		return 0, fmt.Errorf("cost basis usdc must be non-negative")
	}
	if tokenAtomics <= 0 {
		return 0, fmt.Errorf("cost basis token amount must be positive")
	}
	mult, ok := EffectiveUiMultiplier(uiMultiplier, kind)
	if !ok {
		return 0, fmt.Errorf("ui multiplier unresolved")
	}
	scale := atomicScale(decimals)
	num := new(big.Int).SetInt64(totalUSDCMicros)
	num.Mul(num, big.NewInt(scale))
	num.Mul(num, mult.Denom())
	den := new(big.Int).SetInt64(tokenAtomics)
	den.Mul(den, mult.Num())
	if den.Sign() <= 0 {
		return 0, fmt.Errorf("scaled token amount must be positive")
	}
	markInt := new(big.Int).Quo(num, den)
	if !markInt.IsInt64() || markInt.Int64() <= 0 {
		return 0, fmt.Errorf("derived mark per unit must be positive")
	}
	return markInt.Int64(), nil
}

// HoldingValueUSDCMicros values raw atomics at a mark per scaled unit:
// raw × multiplier × mark / 10^decimals, rounded down to USDC micros.
func HoldingValueUSDCMicros(rawAtomics, markUsdcMicros int64, decimals int, uiMultiplier *big.Rat, kind string) (int64, error) {
	if rawAtomics < 0 {
		return 0, fmt.Errorf("token atomics must be non-negative")
	}
	if rawAtomics == 0 {
		return 0, nil
	}
	if markUsdcMicros < 0 {
		return 0, fmt.Errorf("mark must be non-negative")
	}
	mult, ok := EffectiveUiMultiplier(uiMultiplier, kind)
	if !ok {
		return 0, fmt.Errorf("ui multiplier unresolved")
	}
	decimals = NormalizeTokenDecimals(decimals)
	scale := atomicScale(decimals)

	num := new(big.Int).SetInt64(rawAtomics)
	num.Mul(num, mult.Num())
	num.Mul(num, big.NewInt(markUsdcMicros))
	den := new(big.Int).SetInt64(scale)
	den.Mul(den, mult.Denom())
	value := new(big.Int).Quo(num, den)
	if !value.IsInt64() {
		return 0, fmt.Errorf("holding value overflow")
	}
	return value.Int64(), nil
}

// RawAtomicsForOneScaledUnit returns ceil(10^decimals / multiplier) in raw atomics.
func RawAtomicsForOneScaledUnit(decimals int, uiMultiplier *big.Rat) (int64, error) {
	decimals = NormalizeTokenDecimals(decimals)
	mult, ok := EffectiveUiMultiplier(uiMultiplier, "stock")
	if !ok || mult.Sign() <= 0 {
		return 0, fmt.Errorf("ui multiplier unresolved")
	}
	scale := new(big.Int).SetInt64(atomicScale(decimals))
	num := new(big.Int).Mul(scale, mult.Denom())
	den := mult.Num()
	if den.Sign() <= 0 {
		return 0, fmt.Errorf("invalid multiplier")
	}
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	if r.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, fmt.Errorf("scaled unit raw amount overflow")
	}
	return q.Int64(), nil
}
