package privy

import (
	"context"
	"fmt"
)

// EnsureTreasury creates a server-controlled Solana treasury wallet for a group.
func (c *HTTPClient) EnsureTreasury(ctx context.Context, groupID GroupID) (TreasuryRef, error) {
	if string(groupID) == "" {
		return TreasuryRef{}, fmt.Errorf("%w: missing group id", ErrAPI)
	}

	wallet, err := c.createWallet(ctx, "treasury-"+string(groupID), createWalletRequest{
		ChainType:   "solana",
		DisplayName: "monaco-treasury",
		ExternalID:  string(groupID),
	})
	if err != nil {
		return TreasuryRef{}, err
	}

	return TreasuryRef{
		GroupID:       groupID,
		PrivyWalletID: wallet.ID,
		SolanaAddress: wallet.Address,
	}, nil
}
