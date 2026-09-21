package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/ratelimit"
	"github.com/monaco/monaco/apps/backend/internal/storage"
)

// ErrNotGroupCreator means the caller is in the cabal but does not own it. It is
// deliberately distinct from ErrGroupNotFound: a member may see the cabal, so
// hiding its existence would only confuse them.
var ErrNotGroupCreator = errors.New("only the cabal's creator can change its picture")

// Cabal picture writes are per Monaco user and per API process, matching the
// profile photo limiter: a picture change is a rare, deliberate act.
const (
	groupPictureWriteBurst    = 3
	groupPictureWriteInterval = 20 * time.Second // 3/minute sustained
)

// NewGroupPictureWriteLimiter returns the production limiter for the cabal
// picture write endpoints.
func NewGroupPictureWriteLimiter() *ratelimit.Limiter {
	return ratelimit.New(groupPictureWriteBurst, groupPictureWriteInterval)
}

// GroupPictureResult is the cabal's picture after a write.
type GroupPictureResult struct {
	GroupID string
	// PictureURL is blank when the cabal has no picture.
	PictureURL string
}

// GroupPictureService sets and clears a cabal's picture. It shares the image
// pipeline and the storage client with ProfilePhotoService; only the permission
// check and the object key prefix differ.
type GroupPictureService struct {
	store   *postgres.Store
	auth    auth.Verifier
	images  *imageStore
	limiter *ratelimit.Limiter
}

// NewGroupPictureService wires cabal picture dependencies.
func NewGroupPictureService(store *postgres.Store, verifier auth.Verifier, storageClient storage.Client) *GroupPictureService {
	return &GroupPictureService{
		store:  store,
		auth:   verifier,
		images: newImageStore(storageClient, groupPictureKeyPrefix),
	}
}

// WithWriteLimiter rate-limits picture writes per user. Nil disables limiting.
func (s *GroupPictureService) WithWriteLimiter(limiter *ratelimit.Limiter) *GroupPictureService {
	s.limiter = limiter
	return s
}

// SetGroupPicture validates data, stores it, and points the cabal at it.
//
// The order matters: the caller is authorised before any byte reaches storage,
// and the rate limit is spent before the upload, so a flood of large images
// cannot be used to push traffic at the storage backend.
func (s *GroupPictureService) SetGroupPicture(ctx context.Context, accessToken, groupID string, data []byte) (GroupPictureResult, error) {
	if !s.images.configured() {
		return GroupPictureResult{}, ErrImageUploadNotConfigured
	}
	// Reject junk before spending a session verification on it. The full check
	// (format, dimensions, decode) runs in imageStore.store.
	if len(data) == 0 {
		return GroupPictureResult{}, ErrImageInvalid
	}
	if len(data) > maxUploadedImageBytes {
		return GroupPictureResult{}, ErrImageTooLarge
	}

	userID, err := s.authorizeCreator(ctx, accessToken, groupID)
	if err != nil {
		return GroupPictureResult{}, err
	}
	if err := allowWrite(s.limiter, userID); err != nil {
		return GroupPictureResult{}, err
	}

	publicURL, err := s.images.store(ctx, groupID, data)
	if err != nil {
		return GroupPictureResult{}, err
	}

	updated, found, err := s.store.SetGroupPictureURL(ctx, groupID, publicURL)
	if err != nil {
		return GroupPictureResult{}, err
	}
	if !found {
		return GroupPictureResult{}, ErrGroupNotFound
	}
	return groupPictureResult(updated), nil
}

// RemoveGroupPicture clears the cabal's picture, so it falls back to its initials.
func (s *GroupPictureService) RemoveGroupPicture(ctx context.Context, accessToken, groupID string) (GroupPictureResult, error) {
	userID, err := s.authorizeCreator(ctx, accessToken, groupID)
	if err != nil {
		return GroupPictureResult{}, err
	}
	if err := allowWrite(s.limiter, userID); err != nil {
		return GroupPictureResult{}, err
	}

	updated, found, err := s.store.SetGroupPictureURL(ctx, groupID, "")
	if err != nil {
		return GroupPictureResult{}, err
	}
	if !found {
		return GroupPictureResult{}, ErrGroupNotFound
	}
	return groupPictureResult(updated), nil
}

// authorizeCreator resolves the caller and requires that they are a member of
// groupID and the user who created it. It returns the caller's user id.
//
// Membership is checked before ownership so a stranger learns nothing about a
// cabal they cannot see: they get the same "not found" a missing id gives.
func (s *GroupPictureService) authorizeCreator(ctx context.Context, accessToken, groupID string) (string, error) {
	if strings.TrimSpace(groupID) == "" {
		return "", ErrGroupNotFound
	}

	identity, err := s.auth.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		return "", err
	}
	user, found, err := s.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrUserNotFound
	}

	member, err := s.store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		return "", err
	}
	if !member {
		return "", ErrGroupNotFound
	}

	group, found, err := s.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrGroupNotFound
	}
	// A faker club (#153) is a read-only spectator fixture; nobody edits it.
	if group.IsFaker {
		return "", ErrFakerGroupReadOnly
	}
	if group.CreatorUserID != user.ID {
		return "", ErrNotGroupCreator
	}
	return user.ID, nil
}

func groupPictureResult(group postgres.Group) GroupPictureResult {
	return GroupPictureResult{
		GroupID:    group.ID,
		PictureURL: nullStringValue(group.PictureURL),
	}
}

// nullStringValue unwraps a nullable column into the blank-means-absent form the
// app's result structs use; httpapi turns blank back into JSON null.
func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return strings.TrimSpace(value.String)
}
