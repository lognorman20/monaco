package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"

	"github.com/monaco/monaco/apps/backend/internal/imageupload"
	"github.com/monaco/monaco/apps/backend/internal/storage"
)

// Every picture a client uploads — a member's profile photo, a cabal's picture —
// goes through these limits. They are shared on purpose: two different ceilings
// for the same kind of upload is a bug waiting to be found by a user.
const (
	// maxUploadedImageBytes is the largest encoded upload accepted.
	maxUploadedImageBytes = 2 << 20
	// maxUploadedImageEdge bounds each side of the source image. With
	// maxUploadedImagePixels it stops a small file that decodes to gigabytes.
	maxUploadedImageEdge = 4096
	// maxUploadedImagePixels bounds width*height of the source image.
	maxUploadedImagePixels = maxUploadedImageEdge * maxUploadedImageEdge
	// storedImageEdge is the longest side of the image actually stored. Marks and
	// avatars are drawn at most at 64pt, so 512px covers a 3x screen with room to
	// spare and keeps every object small enough to load on a phone network.
	storedImageEdge = 512
)

// groupPictureKeyPrefix namespaces cabal pictures inside the avatars bucket, so
// a group id can never collide with a user id.
const groupPictureKeyPrefix = "groups"

// Errors shared by every image upload path.
var (
	// ErrImageTooLarge means the upload is over maxUploadedImageBytes.
	ErrImageTooLarge = errors.New("uploaded image exceeds size limit")
	// ErrImageInvalid means the bytes are not an image this server will store:
	// wrong type, malformed, or dimensions over the cap.
	ErrImageInvalid = errors.New("uploaded image must be jpeg, png, or webp")
	// ErrImageUploadNotConfigured means no object storage is wired up.
	ErrImageUploadNotConfigured = errors.New("image upload is not configured")
)

// Profile photo errors kept under their original names so existing callers and
// their HTTP copy do not change. They are the same conditions.
var (
	ErrProfilePhotoTooLarge      = ErrImageTooLarge
	ErrProfilePhotoInvalid       = ErrImageInvalid
	ErrProfilePhotoNotConfigured = ErrImageUploadNotConfigured
)

// uploadedImageLimits is the one policy every upload is measured against.
func uploadedImageLimits() imageupload.Limits {
	return imageupload.Limits{
		MaxBytes:   maxUploadedImageBytes,
		MaxEdge:    maxUploadedImageEdge,
		MaxPixels:  maxUploadedImagePixels,
		TargetEdge: storedImageEdge,
	}
}

// imageStore validates an uploaded image and puts it in object storage under a
// fresh, unguessable key.
//
// Writing a new key per upload rather than overwriting one is deliberate: the
// app and every CDN in front of it cache by URL, so a replaced picture appears
// immediately instead of waiting out someone else's cache lifetime.
type imageStore struct {
	client storage.Client
	// prefix namespaces keys by owner kind. Blank keeps the original user-avatar
	// layout, "<userID>/<random>.<ext>", so existing objects stay where they are.
	prefix string
}

func newImageStore(client storage.Client, prefix string) *imageStore {
	return &imageStore{client: client, prefix: prefix}
}

// configured reports whether object storage is wired up.
func (s *imageStore) configured() bool {
	return s != nil && s.client != nil
}

// store validates data, re-encodes it and uploads it for ownerID, returning the
// public URL. Validation errors come back as the ErrImage* sentinels.
func (s *imageStore) store(ctx context.Context, ownerID string, data []byte) (string, error) {
	if !s.configured() {
		return "", ErrImageUploadNotConfigured
	}

	prepared, err := imageupload.Prepare(data, uploadedImageLimits())
	if err != nil {
		return "", asImageError(err)
	}

	key, err := s.objectKey(ownerID, prepared.Extension)
	if err != nil {
		return "", err
	}

	publicURL, err := s.client.Upload(ctx, key, prepared.ContentType, prepared.Data)
	if err != nil {
		return "", fmt.Errorf("upload image: %w", err)
	}
	return publicURL, nil
}

// objectKey returns a fresh key for ownerID. The random suffix is what makes a
// key unguessable, so nobody can probe storage for another owner's picture.
func (s *imageStore) objectKey(ownerID, extension string) (string, error) {
	if ownerID == "" {
		return "", fmt.Errorf("owner id is required")
	}
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate object key: %w", err)
	}
	name := hex.EncodeToString(suffix[:]) + "." + extension
	if s.prefix == "" {
		return path.Join(ownerID, name), nil
	}
	return path.Join(s.prefix, ownerID, name), nil
}

// asImageError maps the image pipeline's errors onto the app's sentinels. Every
// rejection that is the client's fault collapses to "too large" or "invalid",
// because a caller cannot act on a finer distinction than that.
func asImageError(err error) error {
	switch {
	case errors.Is(err, imageupload.ErrTooLarge):
		return ErrImageTooLarge
	case errors.Is(err, imageupload.ErrEmpty),
		errors.Is(err, imageupload.ErrUnsupportedFormat),
		errors.Is(err, imageupload.ErrMalformed),
		errors.Is(err, imageupload.ErrTooManyPixels):
		return ErrImageInvalid
	default:
		return err
	}
}
