package domain

import (
	"fmt"
	"math/big"
)

// RedeemDustMinimumMicros is the minimum USDC slice for a partial redeem ($0.10).
const RedeemDustMinimumMicros USDCMicros = 100_000

// RedeemJobStatus is persisted on redeem_jobs.status.
type RedeemJobStatus string

const (
	RedeemJobDebited RedeemJobStatus = "debited"
	RedeemJobSelling RedeemJobStatus = "selling"
	RedeemJobPaying  RedeemJobStatus = "paying"
	RedeemJobSettled RedeemJobStatus = "settled"
)

// ParseRedeemJobStatus parses a redeem_jobs.status column value.
func ParseRedeemJobStatus(raw string) (RedeemJobStatus, error) {
	switch RedeemJobStatus(raw) {
	case RedeemJobDebited, RedeemJobSelling, RedeemJobPaying, RedeemJobSettled:
		return RedeemJobStatus(raw), nil
	default:
		return "", fmt.Errorf("invalid redeem job status: %q", raw)
	}
}

// RedeemSliceInput is pure domain input for redeem payout sizing.
type RedeemSliceInput struct {
	SharesRedeemedMicros int64
	TotalSharesMicros    int64
	PotNav               USDCMicros
}

// RedeemSlice is the member's redeemed fraction of pot NAV (not a deposit refund).
type RedeemSlice struct {
	UsdcOwed             USDCMicros
	SharesRedeemedMicros int64
	TotalSharesMicros    int64
}

// ComputeRedeemSlice returns sharesRedeemed/totalShares × pot NAV.
func ComputeRedeemSlice(in RedeemSliceInput) (RedeemSlice, error) {
	if in.SharesRedeemedMicros <= 0 {
		return RedeemSlice{}, fmt.Errorf("shares redeemed must be positive")
	}
	if in.TotalSharesMicros <= 0 {
		return RedeemSlice{}, fmt.Errorf("total shares must be positive")
	}
	if in.SharesRedeemedMicros > in.TotalSharesMicros {
		return RedeemSlice{}, fmt.Errorf("shares redeemed exceeds total shares")
	}
	if in.PotNav < 0 {
		return RedeemSlice{}, fmt.Errorf("pot nav must be non-negative")
	}

	product := new(big.Rat).Mul(
		big.NewRat(in.SharesRedeemedMicros, in.TotalSharesMicros),
		big.NewRat(int64(in.PotNav), 1),
	)
	// Floor: sub-micro dust stays in the pot. Rounding a payout up pays out money
	// the redeeming member does not own, at the expense of everyone still in.
	micros, err := ratFloorToInt64(product)
	if err != nil {
		return RedeemSlice{}, fmt.Errorf("redeem slice: %w", err)
	}
	usdc := USDCMicros(micros)
	if usdc <= 0 {
		return RedeemSlice{}, fmt.Errorf("redeem slice must be positive")
	}

	return RedeemSlice{
		UsdcOwed:             usdc,
		SharesRedeemedMicros: in.SharesRedeemedMicros,
		TotalSharesMicros:    in.TotalSharesMicros,
	}, nil
}

// ShareUnitsMicrosForDollarTarget converts a USDC target into share units at current NAV per share.
func ShareUnitsMicrosForDollarTarget(target USDCMicros, navPerShare USDCMicros) (int64, error) {
	if target <= 0 {
		return 0, fmt.Errorf("dollar target must be positive")
	}
	if navPerShare <= 0 {
		return 0, fmt.Errorf("per-share usdc must be positive")
	}
	product := new(big.Rat).Mul(
		big.NewRat(int64(target), 1),
		big.NewRat(1_000_000, 1),
	)
	quotient := new(big.Rat).Quo(product, big.NewRat(int64(navPerShare), 1))
	// Floor: asking for "$25 out" must never debit shares worth more than $25.
	micros, err := ratFloorToInt64(quotient)
	if err != nil {
		return 0, fmt.Errorf("share units for dollar target: %w", err)
	}
	if micros <= 0 {
		return 0, fmt.Errorf("dollar target below minimum share increment")
	}
	return micros, nil
}
