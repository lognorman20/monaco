package app

import (
	"context"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/ratelimit"
	"github.com/monaco/monaco/apps/backend/internal/storage"
)

// ProfilePhotoService stores user avatars via backend-mediated storage upload.
// It shares its validation, re-encoding and object-key policy with cabal
// pictures through imageStore; only the row it updates is its own.
type ProfilePhotoService struct {
	store   *postgres.Store
	privy   privy.Client
	images  *imageStore
	limiter *ratelimit.Limiter
}

// NewProfilePhotoService wires profile photo dependencies.
func NewProfilePhotoService(store *postgres.Store, privyClient privy.Client, storageClient storage.Client) *ProfilePhotoService {
	return &ProfilePhotoService{
		store: store,
		privy: privyClient,
		// Blank prefix: user avatars keep their original "<userID>/<random>.<ext>" keys.
		images: newImageStore(storageClient, ""),
	}
}

// WithUploadLimiter rate-limits UploadProfilePhoto per user. Nil disables limiting.
func (s *ProfilePhotoService) WithUploadLimiter(limiter *ratelimit.Limiter) *ProfilePhotoService {
	s.limiter = limiter
	return s
}

// UploadProfilePhoto validates the image, stores it, and returns the updated profile.
func (s *ProfilePhotoService) UploadProfilePhoto(ctx context.Context, accessToken string, data []byte) (MeResult, error) {
	if !s.images.configured() {
		return MeResult{}, ErrImageUploadNotConfigured
	}
	// Reject junk before spending a session verification on it.
	if len(data) == 0 {
		return MeResult{}, ErrImageInvalid
	}
	if len(data) > maxUploadedImageBytes {
		return MeResult{}, ErrImageTooLarge
	}

	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		return MeResult{}, err
	}

	user, found, err := s.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return MeResult{}, err
	}
	if !found {
		return MeResult{}, ErrUserNotFound
	}

	if err := allowWrite(s.limiter, user.ID); err != nil {
		return MeResult{}, err
	}

	wallet, found, err := s.store.GetMemberWalletByUserID(ctx, user.ID)
	if err != nil {
		return MeResult{}, err
	}
	if !found {
		return MeResult{}, ErrUserNotFound
	}

	publicURL, err := s.images.store(ctx, user.ID, data)
	if err != nil {
		return MeResult{}, err
	}

	updated, err := s.store.UpdateUserProfilePhotoURL(ctx, user.ID, publicURL)
	if err != nil {
		return MeResult{}, err
	}

	return meResultFromUser(updated, wallet.SolanaAddress), nil
}

func meResultFromUser(user postgres.User, memberWalletAddress string) MeResult {
	displayName := ""
	if user.DisplayName.Valid {
		displayName = user.DisplayName.String
	}
	return MeResult{
		UserID:              user.ID,
		DisplayName:         displayName,
		MemberWalletAddress: memberWalletAddress,
		ProfilePhotoURL:     nullStringValue(user.ProfilePhotoURL),
		CreatedAt:           user.CreatedAt.UTC(),
	}
}
