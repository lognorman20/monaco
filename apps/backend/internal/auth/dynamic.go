package auth

import (
	"context"
	"errors"
)

// ErrNotConfigured means Dynamic JWT verification is not wired yet.
var ErrNotConfigured = errors.New("dynamic auth verifier not configured")

type dynamicVerifier struct{}

// NewDynamicVerifier is a stub until M6-T2 implements JWKS verification.
func NewDynamicVerifier(environmentID string, httpClient interface{}) Verifier {
	_ = environmentID
	_ = httpClient
	return &dynamicVerifier{}
}

func (d *dynamicVerifier) VerifySession(ctx context.Context, token AccessToken) (Identity, error) {
	_ = ctx
	_ = token
	return Identity{}, ErrNotConfigured
}
