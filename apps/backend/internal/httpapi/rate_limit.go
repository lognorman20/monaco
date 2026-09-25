package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/ratelimit"
)

// Route classes carry their own budget. Reads are not limited here: they are
// cheap, idempotent, and the app polls them on a timer.
const (
	rateClassAuth  = "auth"
	rateClassMoney = "money"
	rateClassWrite = "write"
)

// rateLimitRule is one class's per-user and per-IP token buckets.
//
// Per-user stops one account from hammering a route with a valid session; per-IP
// stops an unauthenticated flood (session opening) and a single host churning
// through many stolen tokens.
type rateLimitRule struct {
	userBurst    int
	userInterval time.Duration
	ipBurst      int
	ipInterval   time.Duration
}

var rateLimitRules = map[string]rateLimitRule{
	// Session opening is unauthenticated and calls Privy: keep it tight per IP.
	rateClassAuth: {userBurst: 10, userInterval: 6 * time.Second, ipBurst: 20, ipInterval: 3 * time.Second},
	// Money writes move real USDC and hit Solana; a human taps these seconds apart.
	rateClassMoney: {userBurst: 5, userInterval: 10 * time.Second, ipBurst: 20, ipInterval: 3 * time.Second},
	// Everything else a client writes: chat, votes, comments, proposals, joins.
	rateClassWrite: {userBurst: 20, userInterval: 2 * time.Second, ipBurst: 60, ipInterval: time.Second},
}

// RateLimiter holds the token buckets for every route class.
type RateLimiter struct {
	user map[string]*ratelimit.Limiter
	ip   map[string]*ratelimit.Limiter
	// sessions resolves a bearer token to its Privy subject so the per-user bucket
	// survives a token refresh. It runs on every limited request, ahead of the handler,
	// so it must verify locally: privy.HTTPClient checks the ES256 signature against the
	// key loaded at boot and makes no network call.
	sessions SessionVerifier
	// trustProxyHeaders enables X-Forwarded-For parsing. Only turn it on behind a
	// proxy that overwrites the header, otherwise a client spoofs its own IP key.
	trustProxyHeaders bool
}

// NewRateLimiter builds limiters for every route class. Without a verifier no bearer
// token can be tied to a user, so those callers are limited per IP only.
func NewRateLimiter(sessions SessionVerifier, trustProxyHeaders bool) *RateLimiter {
	limiter := &RateLimiter{
		user:              make(map[string]*ratelimit.Limiter, len(rateLimitRules)),
		ip:                make(map[string]*ratelimit.Limiter, len(rateLimitRules)),
		sessions:          sessions,
		trustProxyHeaders: trustProxyHeaders,
	}
	for class, rule := range rateLimitRules {
		limiter.user[class] = ratelimit.New(rule.userBurst, rule.userInterval)
		limiter.ip[class] = ratelimit.New(rule.ipBurst, rule.ipInterval)
	}
	return limiter
}

// WithClock replaces the time source on every bucket. Intended for tests.
func (l *RateLimiter) WithClock(now func() time.Time) *RateLimiter {
	for _, limiter := range l.user {
		limiter.WithClock(now)
	}
	for _, limiter := range l.ip {
		limiter.WithClock(now)
	}
	return l
}

// Middleware rejects requests over budget with 429 and a Retry-After header.
func (l *RateLimiter) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			class, limited := classifyRateLimit(r.Method, r.URL.Path)
			if !limited {
				next.ServeHTTP(w, r)
				return
			}

			ip := clientIP(r, l.trustProxyHeaders)
			if ok, retryAfter := l.ip[class].Allow(class + "|ip|" + ip); !ok {
				l.reject(w, r, class, "ip", retryAfter)
				return
			}
			if identity := l.callerIdentity(r); identity != "" {
				if ok, retryAfter := l.user[class].Allow(class + "|user|" + identity); !ok {
					l.reject(w, r, class, "user", retryAfter)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (l *RateLimiter) reject(w http.ResponseWriter, r *http.Request, class, dimension string, retryAfter time.Duration) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	ctx := r.Context()
	slog.WarnContext(ctx, "http rate limited",
		"method", r.Method,
		"path", r.URL.Path,
		"class", class,
		"dimension", dimension,
		"retry_after_s", seconds,
	)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSONError(ctx, w, http.StatusTooManyRequests, "too many requests, try again shortly")
}

// classifyRateLimit maps a request onto a route class. Every state-changing
// method is limited: a new write route is covered the day it is added.
func classifyRateLimit(method, path string) (string, bool) {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return "", false
	}
	if path == "/v1/auth/session" {
		return rateClassAuth, true
	}
	if isMoneyPath(path) {
		return rateClassMoney, true
	}
	return rateClassWrite, true
}

// moneyPathSuffixes are the write routes that move or commit USDC.
var moneyPathSuffixes = []string{
	"/fund",
	"/deposits",
	"/withdraw-to-balance",
	"/withdrawals",
	"/retry",
	"/agents/intents",
	"/agent/intents",
}

func isMoneyPath(path string) bool {
	path = strings.TrimSuffix(path, "/")
	for _, suffix := range moneyPathSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

// callerIdentity returns the key of the caller's per-user bucket, or "" when the
// request is limited per IP only.
//
// A bearer token keys on its verified Privy subject, not on the token: access tokens are
// short-lived and refreshable, so a token-derived key hands out a fresh budget on every
// refresh. A token that fails verification gets no user bucket at all, so a forged token
// can neither mint buckets nor spend the budget of the subject it names.
//
// Agent keys are long-lived and checked (with their own failure limiter) by the agent
// routes, so they key on a hash: the raw key never reaches a bucket key or a log line.
func (l *RateLimiter) callerIdentity(r *http.Request) string {
	if token, ok := bearerToken(r); ok {
		return l.verifiedSubject(r.Context(), token)
	}
	if key := strings.TrimSpace(r.Header.Get(agentKeyHeader)); key != "" {
		return "a:" + hashCredential(key)
	}
	return ""
}

func (l *RateLimiter) verifiedSubject(ctx context.Context, token string) string {
	if l.sessions == nil {
		return ""
	}
	identity, err := l.sessions.VerifySession(ctx, privy.AccessToken(token))
	if err != nil || identity.PrivyUserID == "" {
		return ""
	}
	return "u:" + identity.PrivyUserID
}

func hashCredential(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

// clientIP returns the peer address, preferring the left-most X-Forwarded-For
// entry only when the deployment says a trusted proxy sets it.
func clientIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if first != "" {
				return first
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}
