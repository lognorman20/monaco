package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// ErrUserNotFound means the Privy token is valid but no Monaco user row exists.
var ErrUserNotFound = errors.New("user not found")

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

// SessionResult is the authenticated user and member wallet for POST /v1/auth/session.
type SessionResult struct {
	UserID              string
	DisplayName         string
	MemberWalletAddress string
}

// MeResult is the authenticated profile for GET /v1/me.
type MeResult struct {
	UserID              string
	DisplayName         string
	MemberWalletAddress string
}

// MemberWallet is the persisted member Solana wallet for a user.
type MemberWallet struct {
	ID            string
	UserID        string
	PrivyWalletID string
	SolanaAddress string
}

// OpenSession verifies a Privy token, upserts the user, and ensures a member wallet.
func (s *SessionService) OpenSession(ctx context.Context, accessToken string) (SessionResult, error) {
	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return SessionResult{}, privy.ErrInvalidToken
		}
		return SessionResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, err := s.store.UpsertUser(ctx, identity.PrivyUserID, identity.DisplayName)
	if err != nil {
		return SessionResult{}, err
	}

	wallet, err := s.EnsureMemberWallet(ctx, identity.PrivyUserID, user.ID)
	if err != nil {
		return SessionResult{}, err
	}

	displayName := ""
	if user.DisplayName.Valid {
		displayName = user.DisplayName.String
	}

	return SessionResult{
		UserID:              user.ID,
		DisplayName:         displayName,
		MemberWalletAddress: wallet.SolanaAddress,
	}, nil
}

// GetMe returns the authenticated user's profile and member wallet address.
func (s *SessionService) GetMe(ctx context.Context, accessToken string) (MeResult, error) {
	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return MeResult{}, privy.ErrInvalidToken
		}
		return MeResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := s.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return MeResult{}, err
	}
	if !found {
		return MeResult{}, ErrUserNotFound
	}

	wallet, found, err := s.store.GetMemberWalletByUserID(ctx, user.ID)
	if err != nil {
		return MeResult{}, err
	}
	if !found {
		return MeResult{}, ErrUserNotFound
	}

	displayName := ""
	if user.DisplayName.Valid {
		displayName = user.DisplayName.String
	}

	return MeResult{
		UserID:              user.ID,
		DisplayName:         displayName,
		MemberWalletAddress: wallet.SolanaAddress,
	}, nil
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
