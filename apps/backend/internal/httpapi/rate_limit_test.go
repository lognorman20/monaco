package httpapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

const rateLimitTestAppID = "rate-limit-test-app"

// noNetwork fails the test if the verifier ever reaches for the network: the limiter
// runs ahead of every write and may not depend on an upstream.
type noNetwork struct{ t *testing.T }

func (n noNetwork) RoundTrip(req *http.Request) (*http.Response, error) {
	n.t.Errorf("rate limiter made a network call to %s", req.URL.Host)
	return nil, errors.New("network is off limits to the rate limiter")
}

// fakeSessions verifies a token by looking its subject up; anything else is invalid.
type fakeSessions map[string]string

func (f fakeSessions) VerifySession(_ context.Context, token privy.AccessToken) (privy.Identity, error) {
	subject, ok := f[string(token)]
	if !ok {
		return privy.Identity{}, privy.ErrInvalidToken
	}
	return privy.Identity{PrivyUserID: subject}, nil
}

// rateLimitFixture is a limiter wired to the real Privy verifier and a signing key for it.
type rateLimitFixture struct {
	t       *testing.T
	key     *ecdsa.PrivateKey
	handler http.Handler
	now     time.Time
	served  int
}

func newRateLimitFixture(t *testing.T) *rateLimitFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	f := &rateLimitFixture{t: t, key: key, now: time.Now().UTC()}
	verifier := privy.NewHTTPClientWithTransport(&config.Config{
		PrivyAppID:           rateLimitTestAppID,
		PrivyVerificationKey: &key.PublicKey,
	}, "http://privy.invalid", noNetwork{t})
	limiter := NewRateLimiter(verifier, false).WithClock(func() time.Time { return f.now })
	f.handler = Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { f.served++ }), limiter.Middleware())
	return f
}

// token signs an access token for subject. Every call returns a different token, the way
// a Privy refresh does.
func (f *rateLimitFixture) token(signer *ecdsa.PrivateKey, subject string, expiresIn time.Duration) string {
	f.t.Helper()
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		f.t.Fatalf("token id: %v", err)
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{
		Issuer:    "privy.io",
		Audience:  jwt.ClaimStrings{rateLimitTestAppID},
		Subject:   subject,
		ID:        fmt.Sprintf("%x", id),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
	}).SignedString(signer)
	if err != nil {
		f.t.Fatalf("sign token: %v", err)
	}
	return signed
}

// money sends one money write from ip with the given headers.
func (f *rateLimitFixture) money(ip string, headers map[string]string) int {
	f.t.Helper()
	req := testHTTPRequest("POST", "/v1/groups/g1/withdraw-to-balance")
	req.RemoteAddr = ip + ":4312"
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec.Code
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestRateLimiter_refreshedToken_sharesTheSubjectsBucket(t *testing.T) {
	// Arrange
	f := newRateLimitFixture(t)
	burst := rateLimitRules[rateClassMoney].userBurst
	first := f.token(f.key, "did:privy:alice", time.Hour)
	refreshed := f.token(f.key, "did:privy:alice", time.Hour)
	if first == refreshed {
		t.Fatal("fixture minted the same token twice")
	}

	// Act
	for i := 0; i < burst; i++ {
		if code := f.money("10.0.0.1", bearer(first)); code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200 inside the burst", i, code)
		}
	}
	// A different IP, so only the per-user bucket can answer 429.
	afterRefresh := f.money("10.0.0.2", bearer(refreshed))
	otherUser := f.money("10.0.0.2", bearer(f.token(f.key, "did:privy:bob", time.Hour)))

	// Assert
	if afterRefresh != http.StatusTooManyRequests {
		t.Fatalf("refreshed token = %d, want 429: a refresh must not reset the budget", afterRefresh)
	}
	if otherUser != http.StatusOK {
		t.Fatalf("another subject = %d, want 200", otherUser)
	}
}

func TestRateLimiter_forgedToken_doesNotTouchTheNamedSubjectsBucket(t *testing.T) {
	// Arrange
	f := newRateLimitFixture(t)
	attacker, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate attacker key: %v", err)
	}
	burst := rateLimitRules[rateClassMoney].userBurst
	forged := f.token(attacker, "did:privy:alice", time.Hour)

	// Act: well past alice's budget, from the attacker's own address.
	for i := 0; i < burst*2; i++ {
		if code := f.money("203.0.113.9", bearer(forged)); code != http.StatusOK {
			t.Fatalf("forged request %d = %d, want 200 from the limiter (the handler rejects it)", i, code)
		}
	}

	// Assert: alice still has her whole burst.
	genuine := f.token(f.key, "did:privy:alice", time.Hour)
	for i := 0; i < burst; i++ {
		if code := f.money("10.0.0.1", bearer(genuine)); code != http.StatusOK {
			t.Fatalf("alice request %d = %d, want 200: a forged token drained her bucket", i, code)
		}
	}
	if code := f.money("10.0.0.1", bearer(genuine)); code != http.StatusTooManyRequests {
		t.Fatalf("alice over budget = %d, want 429", code)
	}
}

func TestRateLimiter_unverifiableToken_isLimitedByIPOnly(t *testing.T) {
	cases := []struct {
		name  string
		token func(f *rateLimitFixture, i int) string
	}{
		{"garbage, a new one per request", func(_ *rateLimitFixture, i int) string { return fmt.Sprintf("garbage-%d", i) }},
		{"garbage, the same one every time", func(*rateLimitFixture, int) string { return "garbage" }},
		{"expired", func(f *rateLimitFixture, _ int) string { return f.token(f.key, "did:privy:alice", -time.Minute) }},
		{"wrong algorithm", func(f *rateLimitFixture, _ int) string {
			signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
				Issuer: "privy.io", Audience: jwt.ClaimStrings{rateLimitTestAppID}, Subject: "did:privy:alice",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			}).SignedString([]byte("shared-secret"))
			if err != nil {
				f.t.Fatalf("sign hs256: %v", err)
			}
			return signed
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			f := newRateLimitFixture(t)
			ipBurst := rateLimitRules[rateClassMoney].ipBurst

			// Act / Assert: the whole IP burst passes, well past the per-user burst...
			for i := 0; i < ipBurst; i++ {
				if code := f.money("203.0.113.9", bearer(tc.token(f, i))); code != http.StatusOK {
					t.Fatalf("request %d = %d, want 200 inside the IP burst", i, code)
				}
			}
			// ...then the IP bucket closes, whatever token comes next...
			if code := f.money("203.0.113.9", bearer(tc.token(f, ipBurst))); code != http.StatusTooManyRequests {
				t.Fatalf("over the IP budget = %d, want 429", code)
			}
			// ...and the same token from elsewhere is not held back by any user bucket.
			if code := f.money("198.51.100.4", bearer(tc.token(f, 0))); code != http.StatusOK {
				t.Fatalf("same token, other IP = %d, want 200", code)
			}
		})
	}
}

func TestRateLimiter_noVerifier_limitsBearerCallersByIPOnly(t *testing.T) {
	// Arrange
	limiter := NewRateLimiter(nil, false)
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), limiter.Middleware())
	ipBurst := rateLimitRules[rateClassMoney].ipBurst
	send := func() int {
		req := testHTTPRequest("POST", "/v1/groups/g1/withdraw-to-balance")
		req.Header.Set("Authorization", "Bearer anything")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// Act / Assert
	for i := 0; i < ipBurst; i++ {
		if code := send(); code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200 inside the IP burst", i, code)
		}
	}
	if code := send(); code != http.StatusTooManyRequests {
		t.Fatalf("over the IP budget = %d, want 429", code)
	}
}

func TestRateLimiter_agentKey_keepsOneBucketPerKeyAcrossIPs(t *testing.T) {
	// Arrange
	f := newRateLimitFixture(t)
	burst := rateLimitRules[rateClassMoney].userBurst
	key := map[string]string{agentKeyHeader: "mk_agent_one"}

	// Act
	for i := 0; i < burst; i++ {
		if code := f.money("10.0.0.1", key); code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200 inside the burst", i, code)
		}
	}
	sameKeyElsewhere := f.money("10.0.0.2", key)
	otherKey := f.money("10.0.0.2", map[string]string{agentKeyHeader: "mk_agent_two"})

	// Assert
	if sameKeyElsewhere != http.StatusTooManyRequests {
		t.Fatalf("same agent key, other IP = %d, want 429", sameKeyElsewhere)
	}
	if otherKey != http.StatusOK {
		t.Fatalf("another agent key = %d, want 200", otherKey)
	}
}
