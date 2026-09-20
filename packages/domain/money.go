package domain

import (
	"errors"
	"fmt"
	"math/big"
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

// ErrAmountOverflow means a money computation does not fit in int64 micros.
// It is returned instead of a silently wrapped (often negative) amount.
var ErrAmountOverflow = errors.New("amount overflows int64 micros")

// ratFloorToInt64 truncates a rational toward zero (floor for non-negative values).
// Every money amount derived from a ratio is floored rather than rounded to the
// nearest unit: the fraction of a micro always stays in the pot, so a payout can
// never exceed what the member actually owns.
func ratFloorToInt64(r *big.Rat) (int64, error) {
	if r == nil || r.Sign() == 0 {
		return 0, nil
	}
	floored := new(big.Int).Quo(r.Num(), r.Denom())
	if !floored.IsInt64() {
		return 0, ErrAmountOverflow
	}
	return floored.Int64(), nil
}

func multiplyDecimalByMicros(units string, markPerUnit USDCMicros) (USDCMicros, error) {
	u, ok := new(big.Rat).SetString(units)
	if !ok || u.Sign() < 0 {
		return 0, fmt.Errorf("invalid units: %q", units)
	}
	product := new(big.Rat).Mul(u, big.NewRat(int64(markPerUnit), 1))
	micros, err := ratFloorToInt64(product)
	if err != nil {
		return 0, fmt.Errorf("mark value for %q units: %w", units, err)
	}
	return USDCMicros(micros), nil
}

func divideMicrosByShares(numerator USDCMicros, shares ShareUnits) (USDCMicros, error) {
	den, ok := new(big.Rat).SetString(string(shares))
	if !ok || den.Sign() <= 0 {
		return 0, fmt.Errorf("invalid share units: %q", shares)
	}
	quotient := new(big.Rat).Quo(big.NewRat(int64(numerator), 1), den)
	micros, err := ratFloorToInt64(quotient)
	if err != nil {
		return 0, fmt.Errorf("per-share price: %w", err)
	}
	return USDCMicros(micros), nil
}

// AddMicros returns a+b and errors when the sum overflows int64 micros.
func AddMicros(a, b USDCMicros) (USDCMicros, error) {
	sum := new(big.Int).Add(big.NewInt(int64(a)), big.NewInt(int64(b)))
	if !sum.IsInt64() {
		return 0, ErrAmountOverflow
	}
	return USDCMicros(sum.Int64()), nil
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
