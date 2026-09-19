package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// ErrUserNotFound means the Privy token is valid but no Monaco user row exists.
var ErrUserNotFound = errors.New("user not found")

// ErrInvalidDisplayName means displayName failed validation.
var ErrInvalidDisplayName = errors.New("invalid display name")

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
	logSessionOpenStart()

	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logSessionBranchWarn("session open rejected", "invalid token")
			return SessionResult{}, privy.ErrInvalidToken
		}
		logSessionBranchError("session open verify failed", err)
		return SessionResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, err := s.store.UpsertUser(ctx, identity.PrivyUserID, identity.DisplayName)
	if err != nil {
		logSessionBranchError("session open upsert user failed", err)
		return SessionResult{}, err
	}

	wallet, err := s.EnsureMemberWallet(ctx, identity.PrivyUserID, user.ID)
	if err != nil {
		logSessionBranchError("session open ensure wallet failed", err, "user_id", user.ID)
		return SessionResult{}, err
	}

	displayName := ""
	if user.DisplayName.Valid {
		displayName = user.DisplayName.String
	}

	logSessionOpenSuccess(user.ID)
	return SessionResult{
		UserID:              user.ID,
		DisplayName:         displayName,
		MemberWalletAddress: wallet.SolanaAddress,
	}, nil
}

// GetMe returns the authenticated user's profile and member wallet address.
func (s *SessionService) GetMe(ctx context.Context, accessToken string) (MeResult, error) {
	logSessionGetMeStart()

	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logSessionBranchWarn("session get me rejected", "invalid token")
			return MeResult{}, privy.ErrInvalidToken
		}
		logSessionBranchError("session get me verify failed", err)
		return MeResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := s.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		logSessionBranchError("session get me lookup user failed", err)
		return MeResult{}, err
	}
	if !found {
		logSessionBranchWarn("session get me rejected", "user not found")
		return MeResult{}, ErrUserNotFound
	}

	wallet, found, err := s.store.GetMemberWalletByUserID(ctx, user.ID)
	if err != nil {
		logSessionBranchError("session get me lookup wallet failed", err, "user_id", user.ID)
		return MeResult{}, err
	}
	if !found {
		logSessionBranchWarn("session get me rejected", "wallet not found", "user_id", user.ID)
		return MeResult{}, ErrUserNotFound
	}

	displayName := ""
	if user.DisplayName.Valid {
		displayName = user.DisplayName.String
	}

	logSessionGetMeSuccess(user.ID)
	return MeResult{
		UserID:              user.ID,
		DisplayName:         displayName,
		MemberWalletAddress: wallet.SolanaAddress,
	}, nil
}

// SetDisplayName validates and persists a user-chosen display name.
func (s *SessionService) SetDisplayName(ctx context.Context, accessToken string, displayName string) (MeResult, error) {
	trimmed := strings.TrimSpace(displayName)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > 32 {
		return MeResult{}, ErrInvalidDisplayName
	}

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

	updated, err := s.store.UpdateUserDisplayName(ctx, user.ID, trimmed)
	if err != nil {
		return MeResult{}, err
	}

	wallet, found, err := s.store.GetMemberWalletByUserID(ctx, user.ID)
	if err != nil {
		return MeResult{}, err
	}
	if !found {
		return MeResult{}, ErrUserNotFound
	}

	displayNameOut := ""
	if updated.DisplayName.Valid {
		displayNameOut = updated.DisplayName.String
	}

	return MeResult{
		UserID:              updated.ID,
		DisplayName:         displayNameOut,
		MemberWalletAddress: wallet.SolanaAddress,
	}, nil
}

// EnsureMemberWallet provisions a member wallet once per user and returns the persisted row.
func (s *SessionService) EnsureMemberWallet(ctx context.Context, privyUserID string, userID string) (MemberWallet, error) {
	if userID == "" {
		logSessionBranchWarn("session ensure wallet rejected", "user id required")
		return MemberWallet{}, fmt.Errorf("user_id is required")
	}

	existing, found, err := s.store.GetMemberWalletByUserID(ctx, userID)
	if err != nil {
		logSessionBranchError("session ensure wallet lookup failed", err, "user_id", userID)
		return MemberWallet{}, err
	}
	if found {
		logSessionEnsureWalletExisting(userID)
		return memberWalletFromStore(existing), nil
	}

	ref, err := s.privy.EnsureMemberWallet(ctx, privyUserID, privy.UserID(userID))
	if err != nil {
		logSessionBranchError("session ensure wallet privy failed", err, "user_id", userID)
		return MemberWallet{}, fmt.Errorf("privy ensure member wallet: %w", err)
	}

	inserted, err := s.store.InsertMemberWallet(ctx, userID, ref.PrivyWalletID, ref.SolanaAddress)
	if err != nil {
		logSessionBranchError("session ensure wallet insert failed", err, "user_id", userID)
		return MemberWallet{}, err
	}

	logSessionEnsureWalletCreated(userID)
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
