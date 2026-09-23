package pyth

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/packages/domain"
)

// NormalizeTokenDecimals treats 0 as xStock default (8).
func NormalizeTokenDecimals(decimals int) int {
	if decimals == 0 {
		return jupiter.XStockDecimals
	}
	return decimals
}

// TokenAtomicsToDecimalUnits formats on-chain token atomics as a decimal unit string.
func TokenAtomicsToDecimalUnits(atomics int64, decimals int) (domain.ShareUnits, error) {
	decimals = NormalizeTokenDecimals(decimals)
	if atomics < 0 {
		return "", fmt.Errorf("token atomics must be non-negative")
	}
	if atomics == 0 {
		return domain.ShareUnits("0"), nil
	}
	scale := jupiter.AtomicScale(decimals)
	r := new(big.Rat).SetFrac(big.NewInt(atomics), big.NewInt(scale))
	s := strings.TrimRight(r.FloatString(decimals), "0")
	s = strings.TrimRight(s, ".")
	return domain.ShareUnits(s), nil
}

// CostBasisMarkPerUnitMicros derives USDC micros per whole token from fill totals.
func CostBasisMarkPerUnitMicros(totalUSDCMicros, tokenAtomics int64, decimals int) (int64, error) {
	decimals = NormalizeTokenDecimals(decimals)
	if totalUSDCMicros < 0 {
		return 0, fmt.Errorf("cost basis usdc must be non-negative")
	}
	if tokenAtomics <= 0 {
		return 0, fmt.Errorf("cost basis token amount must be positive")
	}
	scale := jupiter.AtomicScale(decimals)
	mark, err := domain.MulDivFloor(totalUSDCMicros, scale, tokenAtomics)
	if err != nil {
		return 0, fmt.Errorf("derive mark per unit: %w", err)
	}
	if mark <= 0 {
		return 0, fmt.Errorf("derived mark per unit must be positive")
	}
	return mark, nil
}
