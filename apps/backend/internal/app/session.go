package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/ratelimit"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// ErrUserNotFound means the token is valid but no Monaco user row exists.
var ErrUserNotFound = errors.New("user not found")

// SessionService orchestrates auth session flows.
type SessionService struct {
	store              *postgres.Store
	auth               auth.Verifier
	wallets            wallets.Client
	displayNameLimiter *ratelimit.Limiter
}

// NewSessionService wires session dependencies.
func NewSessionService(store *postgres.Store, verifier auth.Verifier, walletClient wallets.Client) *SessionService {
	return &SessionService{
		store:   store,
		auth:    verifier,
		wallets: walletClient,
	}
}

// WithDisplayNameLimiter rate-limits SetDisplayName per user. Nil disables limiting.
func (s *SessionService) WithDisplayNameLimiter(limiter *ratelimit.Limiter) *SessionService {
	s.displayNameLimiter = limiter
	return s
}

// SessionResult is the authenticated profile for POST /v1/auth/session. Same shape as MeResult.
type SessionResult = MeResult

// MeResult is the authenticated profile for GET/PATCH /v1/me and profile photo upload.
type MeResult struct {
	UserID              string
	DisplayName         string
	MemberWalletAddress string
	ProfilePhotoURL     string
	CreatedAt           time.Time
}

// MemberWallet is the persisted member wallet for a user.
type MemberWallet struct {
	ID       string
	UserID   string
	WalletID string
	Address  string
}

func (s *SessionService) verify(ctx context.Context, accessToken string) (auth.Identity, error) {
	identity, err := s.auth.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return auth.Identity{}, auth.ErrUnauthorized
		}
		return auth.Identity{}, fmt.Errorf("verify session: %w", err)
	}
	return identity, nil
}

// OpenSession verifies a Dynamic token, upserts the user, and ensures a member wallet.
func (s *SessionService) OpenSession(ctx context.Context, accessToken string) (SessionResult, error) {
	logSessionOpenStart()

	identity, err := s.verify(ctx, accessToken)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logSessionBranchWarn("session open rejected", "invalid token")
			return SessionResult{}, auth.ErrUnauthorized
		}
		logSessionBranchError("session open verify failed", err)
		return SessionResult{}, err
	}

	user, err := s.store.UpsertUser(ctx, identity.DynamicUserID, identity.DisplayName)
	if err != nil {
		logSessionBranchError("session open upsert user failed", err)
		return SessionResult{}, err
	}

	wallet, err := s.EnsureMemberWallet(ctx, identity.DynamicUserID, user.ID)
	if err != nil {
		logSessionBranchError("session open ensure wallet failed", err, "user_id", user.ID)
		return SessionResult{}, err
	}

	logSessionOpenSuccess(user.ID)
	return meResultFromUser(user, wallet.Address), nil
}

// GetMe returns the authenticated user's profile and member wallet address.
func (s *SessionService) GetMe(ctx context.Context, accessToken string) (MeResult, error) {
	logSessionGetMeStart()

	identity, err := s.verify(ctx, accessToken)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logSessionBranchWarn("session get me rejected", "invalid token")
			return MeResult{}, auth.ErrUnauthorized
		}
		logSessionBranchError("session get me verify failed", err)
		return MeResult{}, err
	}

	user, found, err := s.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
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

	logSessionGetMeSuccess(user.ID)
	return meResultFromUser(user, wallet.Address), nil
}

// SetDisplayName validates and persists a user-chosen display name for PATCH /v1/me.
func (s *SessionService) SetDisplayName(ctx context.Context, accessToken string, displayName string) (MeResult, error) {
	identity, err := s.verify(ctx, accessToken)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return MeResult{}, auth.ErrUnauthorized
		}
		return MeResult{}, err
	}

	user, found, err := s.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
	if err != nil {
		return MeResult{}, err
	}
	if !found {
		return MeResult{}, ErrUserNotFound
	}

	if err := allowWrite(s.displayNameLimiter, user.ID); err != nil {
		return MeResult{}, err
	}

	normalized, err := NormalizeDisplayName(displayName)
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

	updated, found, err := s.store.UpdateUserDisplayName(ctx, user.ID, normalized)
	if err != nil {
		return MeResult{}, err
	}
	if !found {
		return MeResult{}, ErrUserNotFound
	}

	return meResultFromUser(updated, wallet.Address), nil
}

// EnsureMemberWallet provisions a member wallet once per user and returns the persisted row.
func (s *SessionService) EnsureMemberWallet(ctx context.Context, dynamicUserID string, userID string) (MemberWallet, error) {
	if userID == "" {
		return MemberWallet{}, fmt.Errorf("user_id is required")
	}

	existing, found, err := s.store.GetMemberWalletByUserID(ctx, userID)
	if err != nil {
		return MemberWallet{}, err
	}
	if found {
		logSessionEnsureWalletExisting(userID)
		return memberWalletFromStore(existing), nil
	}

	ref, err := s.wallets.EnsureMemberWallet(ctx, dynamicUserID, wallets.UserID(userID))
	if err != nil {
		return MemberWallet{}, fmt.Errorf("ensure member wallet: %w", err)
	}

	inserted, err := s.store.InsertMemberWallet(ctx, userID, ref.WalletID, ref.Address)
	if err != nil {
		return MemberWallet{}, err
	}

	logSessionEnsureWalletCreated(userID)
	return memberWalletFromStore(inserted), nil
}

func memberWalletFromStore(wallet postgres.MemberWallet) MemberWallet {
	return MemberWallet{
		ID:       wallet.ID,
		UserID:   wallet.UserID,
		WalletID: wallet.WalletID,
		Address:  wallet.Address,
	}
}
