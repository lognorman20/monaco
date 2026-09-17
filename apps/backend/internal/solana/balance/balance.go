// Package balance checks on-chain SOL balances for fee payer readiness.
package balance

import (
	"context"
	"fmt"
	"strings"
)

// FeePayerMinLamports is the minimum relayer balance threshold (0.001 SOL).
// Startup requires balance strictly greater than this value.
const FeePayerMinLamports uint64 = 1_000_000

// BalanceReader fetches lamport balance for a base58 Solana address.
type BalanceReader interface {
	GetBalance(ctx context.Context, address string) (uint64, error)
}

// MustHaveSOL returns an error when balance is not strictly greater than minLamports.
func MustHaveSOL(ctx context.Context, reader BalanceReader, pubkey string, minLamports uint64) error {
	if reader == nil {
		return fmt.Errorf("fee payer SOL balance check: balance reader is required")
	}
	pubkey = strings.TrimSpace(pubkey)
	if pubkey == "" {
		return fmt.Errorf("fee payer SOL balance check: pubkey is required")
	}

	balance, err := reader.GetBalance(ctx, pubkey)
	if err != nil {
		return fmt.Errorf("fee payer SOL balance check: pubkey=%s: %w", pubkey, err)
	}
	if balance <= minLamports {
		return fmt.Errorf(
			"fee payer SOL balance too low: pubkey=%s balance_lamports=%d required_lamports>%d (>0.001 SOL)",
			pubkey,
			balance,
			minLamports,
		)
	}
	return nil
}
