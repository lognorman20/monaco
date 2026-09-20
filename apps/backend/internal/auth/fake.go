package auth

import (
	"context"
	"sync"
)

type fakeVerifier struct {
	mu     sync.Mutex
	tokens map[AccessToken]Identity
}

// NewFakeVerifier returns an in-memory auth verifier for tests.
func NewFakeVerifier() Verifier {
	return &fakeVerifier{tokens: make(map[AccessToken]Identity)}
}

// RegisterToken maps an access token to an identity for tests.
func RegisterToken(v Verifier, token AccessToken, identity Identity) {
	f, ok := v.(*fakeVerifier)
	if !ok {
		panic("auth: RegisterToken requires NewFakeVerifier")
	}
	f.mu.Lock()
	f.tokens[token] = identity
	f.mu.Unlock()
}

func (f *fakeVerifier) VerifySession(ctx context.Context, token AccessToken) (Identity, error) {
	_ = ctx
	f.mu.Lock()
	identity, ok := f.tokens[token]
	f.mu.Unlock()
	if !ok || identity.DynamicUserID == "" {
		return Identity{}, ErrUnauthorized
	}
	return identity, nil
}
