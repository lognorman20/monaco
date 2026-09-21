package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestDynamicVerifier_validToken_returnsIdentity(t *testing.T) {
	key, jwks, kid := testRSAJWKS(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(srv.Close)

	v := NewDynamicVerifier("env-1", srv.Client()).(*DynamicVerifier)
	v.SetJWKSURL(srv.URL)
	tok := signDynamicJWT(t, key, kid, jwt.MapClaims{
		"iss":   "app.dynamicauth.com/env-1",
		"sub":   "dyn-user-1",
		"sid":   "sess-9",
		"scope": "openid user:basic",
		"email": "a@b.co",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})

	got, err := v.VerifySession(context.Background(), AccessToken(tok))
	if err != nil {
		t.Fatalf("VerifySession: %v", err)
	}
	if got.DynamicUserID != "dyn-user-1" || got.SessionID != "sess-9" {
		t.Fatalf("identity = %+v", got)
	}
	if got.DisplayName != "a@b.co" {
		t.Fatalf("display = %q", got.DisplayName)
	}
}

func TestDynamicVerifier_httpsIssuer_accepted(t *testing.T) {
	key, jwks, kid := testRSAJWKS(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(srv.Close)

	v := NewDynamicVerifier("env-1", srv.Client()).(*DynamicVerifier)
	v.SetJWKSURL(srv.URL)
	tok := signDynamicJWT(t, key, kid, jwt.MapClaims{
		"iss":   "https://app.dynamicauth.com/env-1",
		"sub":   "dyn-user-1",
		"scope": "user:basic",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	if _, err := v.VerifySession(context.Background(), AccessToken(tok)); err != nil {
		t.Fatalf("VerifySession: %v", err)
	}
}

func TestDynamicVerifier_wrongIssuer_rejects(t *testing.T) {
	key, jwks, kid := testRSAJWKS(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(srv.Close)
	v := NewDynamicVerifier("env-1", srv.Client()).(*DynamicVerifier)
	v.SetJWKSURL(srv.URL)
	tok := signDynamicJWT(t, key, kid, jwt.MapClaims{
		"iss":   "app.dynamicauth.com/other",
		"sub":   "dyn-user-1",
		"scope": "user:basic",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	if _, err := v.VerifySession(context.Background(), AccessToken(tok)); err != ErrUnauthorized {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestDynamicVerifier_missingUserBasicScope_rejects(t *testing.T) {
	key, jwks, kid := testRSAJWKS(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(srv.Close)
	v := NewDynamicVerifier("env-1", srv.Client()).(*DynamicVerifier)
	v.SetJWKSURL(srv.URL)
	tok := signDynamicJWT(t, key, kid, jwt.MapClaims{
		"iss":   "app.dynamicauth.com/env-1",
		"sub":   "dyn-user-1",
		"scope": "openid",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	if _, err := v.VerifySession(context.Background(), AccessToken(tok)); err != ErrUnauthorized {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestDynamicVerifier_unknownKid_refetchesJWKSOnce(t *testing.T) {
	key, jwksV1, kid1 := testRSAJWKS(t)
	key2, jwksV2, kid2 := testRSAJWKS(t)
	_ = key
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			_, _ = w.Write(jwksV1)
			return
		}
		_, _ = w.Write(jwksV2)
	}))
	t.Cleanup(srv.Close)
	v := NewDynamicVerifier("env-1", srv.Client()).(*DynamicVerifier)
	v.SetJWKSURL(srv.URL)

	tokOld := signDynamicJWT(t, key, kid1, jwt.MapClaims{
		"iss":   "app.dynamicauth.com/env-1",
		"sub":   "dyn-user-1",
		"scope": "user:basic",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	if _, err := v.VerifySession(context.Background(), AccessToken(tokOld)); err != nil {
		t.Fatalf("first token: %v", err)
	}

	tokNew := signDynamicJWT(t, key2, kid2, jwt.MapClaims{
		"iss":   "app.dynamicauth.com/env-1",
		"sub":   "dyn-user-2",
		"scope": "user:basic",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	if _, err := v.VerifySession(context.Background(), AccessToken(tokNew)); err != nil {
		t.Fatalf("rotated kid: %v", err)
	}
	if hits.Load() < 2 {
		t.Fatalf("jwks fetches = %d, want at least 2 (refetch unknown kid)", hits.Load())
	}
}

func TestDynamicVerifier_expired_rejects(t *testing.T) {
	key, jwks, kid := testRSAJWKS(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(srv.Close)
	v := NewDynamicVerifier("env-1", srv.Client()).(*DynamicVerifier)
	v.SetJWKSURL(srv.URL)
	tok := signDynamicJWT(t, key, kid, jwt.MapClaims{
		"iss":   "app.dynamicauth.com/env-1",
		"sub":   "dyn-user-1",
		"scope": "user:basic",
		"exp":   time.Now().Add(-time.Minute).Unix(),
		"iat":   time.Now().Add(-time.Hour).Unix(),
	})
	if _, err := v.VerifySession(context.Background(), AccessToken(tok)); err != ErrUnauthorized {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func testRSAJWKS(t *testing.T) (*rsa.PrivateKey, []byte, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "kid-" + base64.RawURLEncoding.EncodeToString(key.N.Bytes()[:8])
	n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1})
	doc, err := json.Marshal(jwksDoc{Keys: []jwkKey{{Kty: "RSA", Kid: kid, N: n, E: e, Alg: "RS256"}}})
	if err != nil {
		t.Fatal(err)
	}
	return key, doc, kid
}

func signDynamicJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
