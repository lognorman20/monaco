package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const jwksPathFmt = "https://app.dynamicauth.com/api/v0/sdk/%s/.well-known/jwks"

type DynamicVerifier struct {
	environmentID string
	http          *http.Client
	jwksURL       string

	mu   sync.Mutex
	keys map[string]*rsa.PublicKey
}

type dynamicClaims struct {
	jwt.RegisteredClaims
	Scope     string `json:"scope"`
	SID       string `json:"sid"`
	Email     string `json:"email"`
	GivenName string `json:"given_name"`
}

type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
}

// NewDynamicVerifier verifies Dynamic JWTs via JWKS RS256.
func NewDynamicVerifier(environmentID string, httpClient *http.Client) Verifier {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &DynamicVerifier{
		environmentID: strings.TrimSpace(environmentID),
		http:          httpClient,
		jwksURL:       fmt.Sprintf(jwksPathFmt, strings.TrimSpace(environmentID)),
		keys:          make(map[string]*rsa.PublicKey),
	}
}

// SetJWKSURL overrides the JWKS URL (tests).
func (d *DynamicVerifier) SetJWKSURL(url string) {
	d.mu.Lock()
	d.jwksURL = url
	d.mu.Unlock()
}

func (d *DynamicVerifier) VerifySession(ctx context.Context, token AccessToken) (Identity, error) {
	raw := strings.TrimSpace(string(token))
	if raw == "" || d.environmentID == "" {
		return Identity{}, ErrUnauthorized
	}

	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}))
	claims := &dynamicClaims{}
	parsed, err := parser.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		key, err := d.keyForKid(ctx, kid, false)
		if err != nil {
			return nil, err
		}
		if key == nil {
			key, err = d.keyForKid(ctx, kid, true)
			if err != nil {
				return nil, err
			}
		}
		if key == nil {
			return nil, fmt.Errorf("unknown kid")
		}
		return key, nil
	})
	if err != nil || parsed == nil || !parsed.Valid {
		slog.Warn("dynamic jwt parse failed", "err", errString(err))
		return Identity{}, ErrUnauthorized
	}

	wantISS := "app.dynamicauth.com/" + d.environmentID
	iss := strings.TrimPrefix(strings.TrimSpace(claims.Issuer), "https://")
	if iss != wantISS {
		slog.Warn("dynamic jwt issuer mismatch", "iss", claims.Issuer, "want", wantISS)
		return Identity{}, ErrUnauthorized
	}
	now := time.Now()
	if claims.ExpiresAt == nil || !claims.ExpiresAt.After(now) {
		slog.Warn("dynamic jwt expired or missing exp")
		return Identity{}, ErrUnauthorized
	}
	if claims.IssuedAt != nil && claims.IssuedAt.Time.After(now.Add(5*time.Minute)) {
		return Identity{}, ErrUnauthorized
	}
	if !scopeHasUserBasic(claims.Scope) {
		slog.Warn("dynamic jwt missing user:basic scope", "scope", claims.Scope)
		return Identity{}, ErrUnauthorized
	}
	sub := strings.TrimSpace(claims.Subject)
	if sub == "" {
		return Identity{}, ErrUnauthorized
	}
	display := strings.TrimSpace(claims.GivenName)
	if display == "" {
		display = strings.TrimSpace(claims.Email)
	}
	return Identity{
		DynamicUserID: sub,
		SessionID:     strings.TrimSpace(claims.SID),
		DisplayName:   display,
	}, nil
}

func scopeHasUserBasic(scope string) bool {
	for _, part := range strings.Fields(scope) {
		if part == "user:basic" {
			return true
		}
	}
	return false
}

func (d *DynamicVerifier) keyForKid(ctx context.Context, kid string, refetch bool) (*rsa.PublicKey, error) {
	d.mu.Lock()
	key := d.keys[kid]
	d.mu.Unlock()
	if key != nil && !refetch {
		return key, nil
	}
	if err := d.refreshJWKS(ctx); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.keys[kid], nil
}

func (d *DynamicVerifier) refreshJWKS(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var doc jwksDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	next := make(map[string]*rsa.PublicKey)
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := rsaPublicFromJWK(k.N, k.E)
		if err != nil {
			continue
		}
		next[k.Kid] = pub
	}
	d.mu.Lock()
	d.keys = next
	d.mu.Unlock()
	return nil
}

func rsaPublicFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}
	if e == 0 {
		return nil, fmt.Errorf("invalid exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

func errString(err error) string {
	if err == nil {
		return "invalid"
	}
	return err.Error()
}
