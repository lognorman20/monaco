package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/ratelimit"
)

// Profile write limits are per Monaco user and per API process.
const (
	displayNameUpdateBurst    = 5
	displayNameUpdateInterval = 12 * time.Second // 5/minute sustained

	profilePhotoUploadBurst    = 3
	profilePhotoUploadInterval = 20 * time.Second // 3/minute sustained
)

// NewDisplayNameUpdateLimiter returns the production limiter for PATCH /v1/me.
func NewDisplayNameUpdateLimiter() *ratelimit.Limiter {
	return ratelimit.New(displayNameUpdateBurst, displayNameUpdateInterval)
}

// NewProfilePhotoUploadLimiter returns the production limiter for POST /v1/me/profile-photo.
func NewProfilePhotoUploadLimiter() *ratelimit.Limiter {
	return ratelimit.New(profilePhotoUploadBurst, profilePhotoUploadInterval)
}

// ErrRateLimited means the caller exceeded a per-user write limit. Wrapped by *RateLimitError.
var ErrRateLimited = errors.New("rate limited")

// RateLimitError carries how long the caller should wait before retrying.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited: retry after %s", e.RetryAfter)
}

// Is lets callers match with errors.Is(err, ErrRateLimited).
func (e *RateLimitError) Is(target error) bool {
	return target == ErrRateLimited
}

// allowWrite spends one request from limiter for userID. A nil limiter never limits.
func allowWrite(limiter *ratelimit.Limiter, userID string) error {
	if limiter == nil {
		return nil
	}
	if ok, retryAfter := limiter.Allow(userID); !ok {
		return &RateLimitError{RetryAfter: retryAfter}
	}
	return nil
}
