package privy

import (
	"context"
	"fmt"
)

// EnsureMemberWallet creates a Solana wallet owned by the Privy user.
// privyUserID is the did:privy:... subject from a verified session.
// userID is stored as Privy external_id for stable mapping.
func (c *HTTPClient) EnsureMemberWallet(ctx context.Context, privyUserID string, userID UserID) (WalletRef, error) {
	if privyUserID == "" {
		return WalletRef{}, fmt.Errorf("%w: missing privy user id", ErrAPI)
	}
	if string(userID) == "" {
		return WalletRef{}, fmt.Errorf("%w: missing monaco user id", ErrAPI)
	}

	wallet, err := c.createWallet(ctx, "member-"+string(userID), createWalletRequest{
		ChainType:   "solana",
		DisplayName: "monaco-member",
		ExternalID:  string(userID),
		Owner:       &walletOwner{UserID: privyUserID},
	})
	if err != nil {
		return WalletRef{}, err
	}

	return WalletRef{
		UserID:        userID,
		PrivyWalletID: wallet.ID,
		SolanaAddress: wallet.Address,
	}, nil
}
