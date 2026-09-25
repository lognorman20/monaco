package domain

import (
	"fmt"
	"math"
	"math/big"
	"strings"
)

// USDCMicros is a USDC amount in micro-units (1 USDC = 1_000_000 micros).
type USDCMicros int64

// ShareUnits is a decimal string count of internal claim tickets.
type ShareUnits string

// IsZero reports whether u is zero.
func (u USDCMicros) IsZero() bool {
	return u == 0
}

// ParseShareUnits parses a non-negative decimal share amount.
func ParseShareUnits(s string) (ShareUnits, error) {
	if s == "" {
		return ShareUnits("0"), nil
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Sign() < 0 {
		return "", fmt.Errorf("invalid share units: %q", s)
	}
	return ShareUnits(s), nil
}

// IsZero reports whether no shares are outstanding.
func (s ShareUnits) IsZero() bool {
	if s == "" || s == "0" {
		return true
	}
	r, ok := new(big.Rat).SetString(string(s))
	return ok && r.Sign() == 0
}

// ratRoundToInt64 rounds r to the nearest integer, halves toward +infinity.
// It errors when the rounded value does not fit in int64: big.Int.Int64 is
// undefined out of range, so an unchecked conversion would wrap money amounts.
func ratRoundToInt64(r *big.Rat) (int64, error) {
	if r == nil || r.Sign() == 0 {
		return 0, nil
	}
	num := r.Num()
	den := r.Denom()
	twiceNum := new(big.Int).Lsh(num, 1)
	twiceNum.Add(twiceNum, den)
	twiceDen := new(big.Int).Lsh(den, 1)
	rounded := twiceNum.Div(twiceNum, twiceDen)
	if !rounded.IsInt64() {
		return 0, fmt.Errorf("rounded amount overflows int64")
	}
	return rounded.Int64(), nil
}

// addUSDCMicros returns a+b for non-negative amounts, erroring instead of wrapping.
func addUSDCMicros(a, b USDCMicros) (USDCMicros, error) {
	if a < 0 || b < 0 {
		return 0, fmt.Errorf("usdc amounts must be non-negative")
	}
	if a > math.MaxInt64-b {
		return 0, fmt.Errorf("usdc sum overflows int64")
	}
	return a + b, nil
}

func multiplyDecimalByMicros(units string, markPerUnit USDCMicros) (USDCMicros, error) {
	u, ok := new(big.Rat).SetString(units)
	if !ok || u.Sign() < 0 {
		return 0, fmt.Errorf("invalid units: %q", units)
	}
	if markPerUnit < 0 {
		return 0, fmt.Errorf("mark per unit must be non-negative")
	}
	product := new(big.Rat).Mul(u, big.NewRat(int64(markPerUnit), 1))
	value, err := ratRoundToInt64(product)
	if err != nil {
		return 0, fmt.Errorf("units %q at mark %d: %w", units, markPerUnit, err)
	}
	return USDCMicros(value), nil
}

func divideMicrosByShares(numerator USDCMicros, shares ShareUnits) (USDCMicros, error) {
	den, ok := new(big.Rat).SetString(string(shares))
	if !ok || den.Sign() <= 0 {
		return 0, fmt.Errorf("invalid share units: %q", shares)
	}
	quotient := new(big.Rat).Quo(big.NewRat(int64(numerator), 1), den)
	value, err := ratRoundToInt64(quotient)
	if err != nil {
		return 0, fmt.Errorf("%d micros over %q shares: %w", numerator, shares, err)
	}
	return USDCMicros(value), nil
}

// MulDivFloor returns floor(a*b/c) for non-negative a and b and positive c,
// computed in arbitrary precision so micros × atomics products cannot wrap.
// It errors when the inputs are out of range or the result exceeds int64.
func MulDivFloor(a, b, c int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, fmt.Errorf("mul-div operands must be non-negative")
	}
	if c <= 0 {
		return 0, fmt.Errorf("mul-div divisor must be positive")
	}
	product := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	product.Quo(product, big.NewInt(c))
	if !product.IsInt64() {
		return 0, fmt.Errorf("mul-div result overflows int64")
	}
	return product.Int64(), nil
}

// USDCDecimals is the number of decimal places in one USDC.
const USDCDecimals = 6

// ParseUSDDecimal parses a positive dollar amount such as "10.50" into USDC micros.
func ParseUSDDecimal(s string) (int64, error) {
	micros, err := ParseTokenDecimal(s, USDCDecimals)
	if err != nil {
		return 0, fmt.Errorf("usd: %w", err)
	}
	return micros, nil
}

// ParseTokenDecimal parses a positive decimal amount such as "0.25" into atomic units of a
// token with the given decimals. It accepts plain digits with at most one point and at most
// decimals fractional digits, and never rounds: an amount the token cannot represent is an
// error, not a smaller trade.
func ParseTokenDecimal(s string, decimals int) (int64, error) {
	if decimals < 0 {
		return 0, fmt.Errorf("decimals must be non-negative")
	}
	whole, frac, hasPoint := strings.Cut(s, ".")
	if whole == "" && frac == "" || hasPoint && frac == "" || !allDigits(whole) || !allDigits(frac) {
		return 0, fmt.Errorf("invalid amount %q: use digits with an optional decimal point, like 10.50", s)
	}
	if len(frac) > decimals {
		return 0, fmt.Errorf("amount %q has more than %d decimal places", s, decimals)
	}
	atomic, ok := new(big.Int).SetString(whole+frac+strings.Repeat("0", decimals-len(frac)), 10)
	if !ok {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	if !atomic.IsInt64() {
		return 0, fmt.Errorf("amount %q is too large", s)
	}
	if atomic.Sign() <= 0 {
		return 0, fmt.Errorf("amount %q must be positive", s)
	}
	return atomic.Int64(), nil
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
