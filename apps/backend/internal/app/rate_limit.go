package app

import (
	"sync"
	"time"
)

// KeyedRateLimiter is an in-memory token bucket per key (for example, a user id).
// Each key starts with Burst tokens and regains one token every Refill interval.
// State lives in process memory: limits reset on restart and are not shared across instances.
type KeyedRateLimiter struct {
	burst  float64
	refill time.Duration
	now    func() time.Time

	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// maxIdleBuckets bounds memory: once exceeded, full (idle) buckets are dropped on the next Allow.
const maxIdleBuckets = 10_000

// NewKeyedRateLimiter returns a limiter allowing burst events per key, refilling one token per refill.
func NewKeyedRateLimiter(burst int, refill time.Duration, now func() time.Time) *KeyedRateLimiter {
	if burst < 1 {
		burst = 1
	}
	if refill <= 0 {
		refill = time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &KeyedRateLimiter{
		burst:   float64(burst),
		refill:  refill,
		now:     now,
		buckets: make(map[string]*tokenBucket),
	}
}

// Allow consumes one token for key. When none is available it returns false and how long
// until the next token.
func (l *KeyedRateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if len(l.buckets) > maxIdleBuckets {
		l.pruneFullLocked(now)
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	l.refillLocked(b, now)

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	missing := 1 - b.tokens
	wait := time.Duration(missing * float64(l.refill))
	return false, wait
}

func (l *KeyedRateLimiter) refillLocked(b *tokenBucket, now time.Time) {
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return
	}
	b.tokens += float64(elapsed) / float64(l.refill)
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
}

func (l *KeyedRateLimiter) pruneFullLocked(now time.Time) {
	for key, b := range l.buckets {
		l.refillLocked(b, now)
		if b.tokens >= l.burst {
			delete(l.buckets, key)
		}
	}
}
