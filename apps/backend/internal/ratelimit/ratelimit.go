// Package ratelimit provides an in-process, per-key token bucket for write endpoints.
//
// Buckets live in memory, so limits are per API process. Monaco runs a single API
// instance today; running several replicas would need a shared store (Postgres or
// Redis) to keep the same guarantee.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// sweepInterval bounds how often idle buckets are pruned from memory.
const sweepInterval = time.Minute

// Limiter is a keyed token bucket: each key may spend Burst requests at once and
// regains one request every Interval. It is safe for concurrent use.
type Limiter struct {
	mu        sync.Mutex
	interval  time.Duration
	burst     float64
	now       func() time.Time
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New returns a limiter that allows burst requests per key and refills one
// request every interval. burst and interval must be positive.
func New(burst int, interval time.Duration) *Limiter {
	if burst < 1 {
		panic("ratelimit: burst must be >= 1")
	}
	if interval <= 0 {
		panic("ratelimit: interval must be > 0")
	}
	return &Limiter{
		interval: interval,
		burst:    float64(burst),
		now:      time.Now,
		buckets:  make(map[string]*bucket),
	}
}

// WithClock replaces the time source. Intended for tests.
func (l *Limiter) WithClock(now func() time.Time) *Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
	return l
}

// Allow spends one request for key. When the key is over its limit it returns
// false and how long the caller should wait before one request is available.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else {
		b.tokens = l.refilled(b, now)
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	missing := 1 - b.tokens
	wait := time.Duration(math.Ceil(missing * float64(l.interval)))
	return false, wait
}

// Blocked reports whether key is over its limit without spending a request, and how
// long until one request is available. Use it with Allow when only some outcomes
// should count against the limit (for example failed key guesses, not valid calls).
func (l *Limiter) Blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		return false, 0
	}
	tokens := l.refilled(b, l.now())
	if tokens >= 1 {
		return false, 0
	}
	return true, time.Duration(math.Ceil((1 - tokens) * float64(l.interval)))
}

func (l *Limiter) refilled(b *bucket, now time.Time) float64 {
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return b.tokens
	}
	return math.Min(l.burst, b.tokens+float64(elapsed)/float64(l.interval))
}

// sweepLocked drops buckets that have refilled completely; they carry no state a
// fresh bucket would not.
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	for key, b := range l.buckets {
		if l.refilled(b, now) >= l.burst {
			delete(l.buckets, key)
		}
	}
}

// Len reports how many keys are tracked. Intended for tests.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
