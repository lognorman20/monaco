package privy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestSession_validPrivyToken_upsertsUserAndReturnsSession(t *testing.T) {
	// Arrange
	client, privateKey := testVerifyClient(t)
	token := signPrivyAccessToken(t, privateKey, "test-app-id", privyTokenOptions{
		userID:    "did:privy:alfred",
		sessionID: "session-valid-1",
	})

	// Act
	identity, err := client.VerifySession(context.Background(), AccessToken(token))

	// Assert
	if err != nil {
		t.Fatalf("VerifySession valid token: %v", err)
	}
	if identity.PrivyUserID != "did:privy:alfred" {
		t.Fatalf("PrivyUserID = %q", identity.PrivyUserID)
	}
	if identity.SessionID != "session-valid-1" {
		t.Fatalf("SessionID = %q", identity.SessionID)
	}
}

func TestSession_invalidPrivyToken_returns401(t *testing.T) {
	// Arrange
	client, _ := testVerifyClient(t)

	// Act
	_, err := client.VerifySession(context.Background(), AccessToken("not-a-valid-privy-jwt"))

	// Assert
	if err != ErrInvalidToken {
		t.Fatalf("err = %v, want %v", err, ErrInvalidToken)
	}
}

func TestSession_expiredPrivyToken_returns401(t *testing.T) {
	// Arrange
	client, privateKey := testVerifyClient(t)
	token := signPrivyAccessToken(t, privateKey, "test-app-id", privyTokenOptions{
		userID:    "did:privy:alfred",
		sessionID: "session-expired-1",
		expired:   true,
	})

	// Act
	_, err := client.VerifySession(context.Background(), AccessToken(token))

	// Assert
	if err != ErrInvalidToken {
		t.Fatalf("err = %v, want %v", err, ErrInvalidToken)
	}
}

func TestVerifySession_clientWithoutVerificationKey_reportsNotReady(t *testing.T) {
	// Arrange
	client := NewHTTPClient(testConfig())

	// Act
	readyErr := client.VerifierReady()
	_, err := client.VerifySession(context.Background(), AccessToken("any-token"))

	// Assert
	if readyErr == nil {
		t.Fatal("VerifierReady = nil, want an error so /health reports the verifier down")
	}
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("err = %v, want ErrAPI", err)
	}
}

type privyTokenOptions struct {
	userID    string
	sessionID string
	expired   bool
}

func testVerifyClient(t *testing.T) (*HTTPClient, *ecdsa.PrivateKey) {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	cfg := testConfig()
	cfg.PrivyVerificationKey = &privateKey.PublicKey
	client := NewHTTPClient(cfg)
	return client, privateKey
}

func signPrivyAccessToken(t *testing.T, privateKey *ecdsa.PrivateKey, appID string, opts privyTokenOptions) string {
	t.Helper()

	expiresAt := time.Now().Add(time.Hour)
	if opts.expired {
		expiresAt = time.Now().Add(-time.Hour)
	}

	claims := privyClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   opts.userID,
			Audience:  jwt.ClaimStrings{appID},
			Issuer:    "privy.io",
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		SessionID: opts.sessionID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}
