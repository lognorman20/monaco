package privy

import (
	"context"
	"errors"
)

// ErrVerifyNotImplemented is returned until M1-T4a implements access-token verification.
var ErrVerifyNotImplemented = errors.New("privy: verify session not implemented")

// VerifySession validates a Privy access token and returns the authenticated identity.
// M1-T4a will replace this stub with JWT verification against Privy's verification key.
func (c *HTTPClient) VerifySession(ctx context.Context, token AccessToken) (Identity, error) {
	_ = ctx
	if string(token) == "" {
		return Identity{}, ErrInvalidToken
	}
	return Identity{}, ErrVerifyNotImplemented
}
