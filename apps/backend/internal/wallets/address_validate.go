package wallets

import (
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/evm"
)

// ValidateAddress checks a Base EVM address for platform withdrawals.
func ValidateAddress(addr string) error {
	if _, err := evm.NormalizeAddress(addr); err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}
	return nil
}
