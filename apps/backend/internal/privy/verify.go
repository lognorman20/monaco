package privy

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type privyClaims struct {
	jwt.RegisteredClaims
	SessionID string `json:"sid,omitempty"`
}

// VerifySession validates a Privy ES256 access token and returns the authenticated identity.
func (c *HTTPClient) VerifySession(ctx context.Context, token AccessToken) (Identity, error) {
	_ = ctx
	if strings.TrimSpace(string(token)) == "" {
		logVerifySession(false, "")
		return Identity{}, ErrInvalidToken
	}

	if err := c.VerifierReady(); err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrAPI, err)
	}
	publicKey := c.verificationKey

	claims := &privyClaims{}
	_, err := jwt.ParseWithClaims(
		string(token),
		claims,
		func(t *jwt.Token) (any, error) {
			if t.Method.Alg() != jwt.SigningMethodES256.Alg() {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return publicKey, nil
		},
		jwt.WithIssuer("privy.io"),
		jwt.WithAudience(c.appID),
		jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg()}),
	)
	if err != nil {
		logVerifySession(false, "")
		return Identity{}, ErrInvalidToken
	}
	if claims.Subject == "" {
		logVerifySession(false, "")
		return Identity{}, ErrInvalidToken
	}

	logVerifySession(true, claims.Subject)
	return Identity{
		PrivyUserID: claims.Subject,
		SessionID:   claims.SessionID,
	}, nil
}

// VerifierReady reports whether access tokens can be verified. The key is parsed once by
// config.Load, so this only fails for a client built without it; GET /health surfaces it.
func (c *HTTPClient) VerifierReady() error {
	if c == nil || c.verificationKey == nil {
		return fmt.Errorf("privy verification key is not loaded")
	}
	return nil
}
