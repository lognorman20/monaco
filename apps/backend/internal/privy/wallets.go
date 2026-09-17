package privy

import (
	"context"
	"fmt"
)

// EnsureMemberWallet returns the user's Solana wallet, creating one only when none exists in Privy.
// privyUserID is the did:privy:... subject from a verified session.
// userID is stored as Privy external_id for stable mapping.
func (c *HTTPClient) EnsureMemberWallet(ctx context.Context, privyUserID string, userID UserID) (WalletRef, error) {
	if privyUserID == "" {
		return WalletRef{}, fmt.Errorf("%w: missing privy user id", ErrAPI)
	}
	if string(userID) == "" {
		return WalletRef{}, fmt.Errorf("%w: missing monaco user id", ErrAPI)
	}

	existing, found, err := c.findExistingMemberWallet(ctx, privyUserID, userID)
	if err != nil {
		return WalletRef{}, err
	}
	if found {
		return WalletRef{
			UserID:        userID,
			PrivyWalletID: existing.ID,
			SolanaAddress: existing.Address,
		}, nil
	}

	additionalSigners, err := c.walletAdditionalSigners()
	if err != nil {
		return WalletRef{}, err
	}

	wallet, err := c.createWallet(ctx, "member-"+string(userID), createWalletRequest{
		ChainType:         "solana",
		DisplayName:       "monaco-member",
		ExternalID:        string(userID),
		Owner:             &walletOwner{UserID: privyUserID},
		AdditionalSigners: additionalSigners,
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

func (c *HTTPClient) findExistingMemberWallet(ctx context.Context, privyUserID string, userID UserID) (walletResponse, bool, error) {
	wallets, err := c.listWallets(ctx, privyUserID)
	if err != nil {
		return walletResponse{}, false, err
	}
	if len(wallets) == 0 {
		return walletResponse{}, false, nil
	}

	monacoUserID := string(userID)
	for _, wallet := range wallets {
		if wallet.ExternalID == monacoUserID && wallet.ID != "" && wallet.Address != "" {
			return wallet, true, nil
		}
	}
	for _, wallet := range wallets {
		if wallet.ID != "" && wallet.Address != "" {
			return wallet, true, nil
		}
	}

	return walletResponse{}, false, nil
}

// ListAppSolanaWallets returns every Solana wallet in this Privy app (paginated).
func (c *HTTPClient) ListAppSolanaWallets(ctx context.Context) ([]WalletRef, error) {
	wallets, err := c.listSolanaWallets(ctx, "")
	if err != nil {
		return nil, err
	}
	var refs []WalletRef
	seen := map[string]struct{}{}
	for _, wallet := range wallets {
		if wallet.ID == "" || wallet.Address == "" {
			continue
		}
		if _, ok := seen[wallet.Address]; ok {
			continue
		}
		seen[wallet.Address] = struct{}{}
		refs = append(refs, WalletRef{
			PrivyWalletID: wallet.ID,
			SolanaAddress: wallet.Address,
		})
	}
	return refs, nil
}

func (c *HTTPClient) walletAdditionalSigners() ([]additionalSigner, error) {
	if c.privyAuthorizationPrivateKey != "" && c.privyAuthorizationKeyID == "" {
		return nil, fmt.Errorf("%w: PRIVY_AUTHORIZATION_KEY_ID is required when PRIVY_AUTHORIZATION_PRIVATE_KEY is set", ErrAPI)
	}
	if c.privyAuthorizationKeyID == "" {
		return nil, nil
	}
	return []additionalSigner{{SignerID: c.privyAuthorizationKeyID}}, nil
}
