package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/storage"
)

const maxProfilePhotoBytes = 2 << 20

var (
	ErrProfilePhotoTooLarge   = errors.New("profile photo exceeds size limit")
	ErrProfilePhotoInvalid    = errors.New("profile photo must be jpeg, png, or webp")
	ErrProfilePhotoNotConfigured = errors.New("profile photo upload is not configured")
)

type profileImageFormat struct {
	contentType string
	extension   string
}

// ProfilePhotoService stores user avatars via backend-mediated storage upload.
type ProfilePhotoService struct {
	store   *postgres.Store
	privy   privy.Client
	storage storage.Client
}

// NewProfilePhotoService wires profile photo dependencies.
func NewProfilePhotoService(store *postgres.Store, privyClient privy.Client, storageClient storage.Client) *ProfilePhotoService {
	return &ProfilePhotoService{
		store:   store,
		privy:   privyClient,
		storage: storageClient,
	}
}

// UploadProfilePhoto validates the image, stores it, and returns the updated profile.
func (s *ProfilePhotoService) UploadProfilePhoto(ctx context.Context, accessToken string, data []byte) (MeResult, error) {
	if s.storage == nil {
		return MeResult{}, ErrProfilePhotoNotConfigured
	}
	if len(data) == 0 {
		return MeResult{}, ErrProfilePhotoInvalid
	}
	if len(data) > maxProfilePhotoBytes {
		return MeResult{}, ErrProfilePhotoTooLarge
	}

	format, ok := detectProfileImageFormat(data)
	if !ok {
		return MeResult{}, ErrProfilePhotoInvalid
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

	wallet, found, err := s.store.GetMemberWalletByUserID(ctx, user.ID)
	if err != nil {
		return MeResult{}, err
	}
	if !found {
		return MeResult{}, ErrUserNotFound
	}

	objectKey, err := newAvatarObjectKey(user.ID, format.extension)
	if err != nil {
		return MeResult{}, err
	}
	publicURL, err := s.storage.Upload(ctx, objectKey, format.contentType, data)
	if err != nil {
		return MeResult{}, fmt.Errorf("upload profile photo: %w", err)
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
	profilePhotoURL := ""
	if user.ProfilePhotoURL.Valid {
		profilePhotoURL = user.ProfilePhotoURL.String
	}
	return MeResult{
		UserID:              user.ID,
		DisplayName:         displayName,
		MemberWalletAddress: memberWalletAddress,
		ProfilePhotoURL:     profilePhotoURL,
	}
}

func detectProfileImageFormat(data []byte) (profileImageFormat, bool) {
	switch {
	case len(data) >= 3 && bytes.Equal(data[:3], []byte{0xFF, 0xD8, 0xFF}):
		return profileImageFormat{contentType: "image/jpeg", extension: "jpg"}, true
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}):
		return profileImageFormat{contentType: "image/png", extension: "png"}, true
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return profileImageFormat{contentType: "image/webp", extension: "webp"}, true
	default:
		return profileImageFormat{}, false
	}
}

func newAvatarObjectKey(userID, extension string) (string, error) {
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate object key: %w", err)
	}
	return fmt.Sprintf("%s/%s.%s", userID, hex.EncodeToString(suffix[:]), extension), nil
}
