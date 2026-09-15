package privy

import (
	"context"
	"fmt"
	"os"
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
		return Identity{}, ErrInvalidToken
	}

	verificationKeyPEM, err := c.verificationKeyPEM()
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrAPI, err)
	}

	publicKey, err := jwt.ParseECPublicKeyFromPEM([]byte(verificationKeyPEM))
	if err != nil {
		return Identity{}, fmt.Errorf("%w: invalid verification key: %v", ErrAPI, err)
	}

	claims := &privyClaims{}
	_, err = jwt.ParseWithClaims(
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
		return Identity{}, ErrInvalidToken
	}
	if claims.Subject == "" {
		return Identity{}, ErrInvalidToken
	}

	return Identity{
		PrivyUserID: claims.Subject,
		SessionID:   claims.SessionID,
	}, nil
}

func (c *HTTPClient) verificationKeyPEM() (string, error) {
	key := strings.TrimSpace(os.Getenv("PRIVY_VERIFICATION_KEY"))
	if key == "" {
		return "", fmt.Errorf("PRIVY_VERIFICATION_KEY is required")
	}
	return key, nil
}
