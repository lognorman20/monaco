package app

import (
	"context"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// SessionService orchestrates auth session flows.
type SessionService struct {
	store *postgres.Store
	privy privy.Client
}

// NewSessionService wires session dependencies.
func NewSessionService(store *postgres.Store, privyClient privy.Client) *SessionService {
	return &SessionService{
		store: store,
		privy: privyClient,
	}
}

// MemberWallet is the persisted member Solana wallet for a user.
type MemberWallet struct {
	ID            string
	UserID        string
	PrivyWalletID string
	SolanaAddress string
}

// EnsureMemberWallet provisions a member wallet once per user and returns the persisted row.
func (s *SessionService) EnsureMemberWallet(ctx context.Context, privyUserID string, userID string) (MemberWallet, error) {
	if userID == "" {
		return MemberWallet{}, fmt.Errorf("user_id is required")
	}

	existing, found, err := s.store.GetMemberWalletByUserID(ctx, userID)
	if err != nil {
		return MemberWallet{}, err
	}
	if found {
		return memberWalletFromStore(existing), nil
	}

	ref, err := s.privy.EnsureMemberWallet(ctx, privyUserID, privy.UserID(userID))
	if err != nil {
		return MemberWallet{}, fmt.Errorf("privy ensure member wallet: %w", err)
	}

	inserted, err := s.store.InsertMemberWallet(ctx, userID, ref.PrivyWalletID, ref.SolanaAddress)
	if err != nil {
		return MemberWallet{}, err
	}

	return memberWalletFromStore(inserted), nil
}

func memberWalletFromStore(wallet postgres.MemberWallet) MemberWallet {
	return MemberWallet{
		ID:            wallet.ID,
		UserID:        wallet.UserID,
		PrivyWalletID: wallet.PrivyWalletID,
		SolanaAddress: wallet.SolanaAddress,
	}
}
