package privy

import (
	"context"
	"fmt"
)

// SubmitSweep submits a server-signed USDC sweep with the relayer as SOL fee payer.
func (c *HTTPClient) SubmitSweep(ctx context.Context, req SweepRequest) (SweepResult, error) {
	_ = ctx
	if req.RelayerKey == "" {
		return SweepResult{}, fmt.Errorf("%w: relayer key required", ErrAPI)
	}
	return SweepResult{}, fmt.Errorf("%w: SubmitSweep not wired to Privy HTTP yet", ErrAPI)
}

// MemberUSDCBalance returns member wallet USDC balance via Privy + RPC.
func (c *HTTPClient) MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error) {
	_ = ctx
	_ = memberAddress
	return 0, fmt.Errorf("%w: MemberUSDCBalance not wired to Privy HTTP yet", ErrAPI)
}

// TreasuryUSDCBalance returns treasury wallet USDC balance via Privy + RPC.
func (c *HTTPClient) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	_ = ctx
	_ = treasuryAddress
	return 0, fmt.Errorf("%w: TreasuryUSDCBalance not wired to Privy HTTP yet", ErrAPI)
}

// BuildSweepRequest validates sweep inputs and attaches relayer fee payer key.
func BuildSweepRequest(memberAddress, treasuryAddress string, amount int64, relayerKey string) (SweepRequest, error) {
	if memberAddress == "" || treasuryAddress == "" {
		return SweepRequest{}, fmt.Errorf("member and treasury addresses are required")
	}
	if amount <= 0 {
		return SweepRequest{}, fmt.Errorf("amount must be positive")
	}
	if relayerKey == "" {
		return SweepRequest{}, fmt.Errorf("relayer key is required")
	}
	return SweepRequest{
		MemberAddress:   memberAddress,
		TreasuryAddress: treasuryAddress,
		Amount:          amount,
		RelayerKey:      relayerKey,
	}, nil
}
