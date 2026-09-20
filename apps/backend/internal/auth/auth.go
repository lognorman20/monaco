package auth

import (
	"context"
	"errors"
)

// AccessToken is a Dynamic user access token from mobile login.
type AccessToken string

// Identity is the verified Dynamic session subject.
type Identity struct {
	DynamicUserID string
	SessionID     string
	DisplayName   string
}

// ErrUnauthorized means the access token is invalid or expired.
var ErrUnauthorized = errors.New("unauthorized")

// Verifier validates Dynamic access tokens.
type Verifier interface {
	VerifySession(ctx context.Context, token AccessToken) (Identity, error)
}
