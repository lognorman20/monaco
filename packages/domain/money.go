package domain

import (
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

func ratRoundToInt64(r *big.Rat) int64 {
	if r == nil || r.Sign() == 0 {
		return 0
	}
	num := r.Num()
	den := r.Denom()
	twiceNum := new(big.Int).Lsh(num, 1)
	twiceNum.Add(twiceNum, den)
	twiceDen := new(big.Int).Lsh(den, 1)
	rounded := twiceNum.Div(twiceNum, twiceDen)
	return rounded.Int64()
}

func multiplyDecimalByMicros(units string, markPerUnit USDCMicros) (USDCMicros, error) {
	u, ok := new(big.Rat).SetString(units)
	if !ok || u.Sign() < 0 {
		return 0, fmt.Errorf("invalid units: %q", units)
	}
	product := new(big.Rat).Mul(u, big.NewRat(int64(markPerUnit), 1))
	return USDCMicros(ratRoundToInt64(product)), nil
}

func divideMicrosByShares(numerator USDCMicros, shares ShareUnits) (USDCMicros, error) {
	den, ok := new(big.Rat).SetString(string(shares))
	if !ok || den.Sign() <= 0 {
		return 0, fmt.Errorf("invalid share units: %q", shares)
	}
	quotient := new(big.Rat).Quo(big.NewRat(int64(numerator), 1), den)
	return USDCMicros(ratRoundToInt64(quotient)), nil
}
