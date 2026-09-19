package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/ratelimit"
)

// ErrUserNotFound means the Privy token is valid but no Monaco user row exists.
var ErrUserNotFound = errors.New("user not found")

// SessionService orchestrates auth session flows.
type SessionService struct {
	store              *postgres.Store
	privy              privy.Client
	displayNameLimiter *ratelimit.Limiter
}

// NewSessionService wires session dependencies.
func NewSessionService(store *postgres.Store, privyClient privy.Client) *SessionService {
	return &SessionService{
		store: store,
		privy: privyClient,
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

	logSessionOpenSuccess(user.ID)
	return meResultFromUser(user, wallet.SolanaAddress), nil
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

	logSessionGetMeSuccess(user.ID)
	return meResultFromUser(user, wallet.SolanaAddress), nil
}

// SetDisplayName validates and persists a user-chosen display name for PATCH /v1/me.
//
// The session is verified before the name is validated so unauthenticated callers
// always get ErrInvalidToken. Every authenticated attempt, valid or not, spends one
// request from the per-user limiter.
func (s *SessionService) SetDisplayName(ctx context.Context, accessToken string, displayName string) (MeResult, error) {
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

	return meResultFromUser(updated, wallet.SolanaAddress), nil
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
