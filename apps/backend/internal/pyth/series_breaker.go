package pyth

import (
	"sync"
	"time"
)

// defaultSeriesBreakerCooldown is how long the one-call history source is left
// alone after it fails. Short enough that a blip heals within a demo, long enough
// that an outage costs one timeout rather than one per chart load.
const defaultSeriesBreakerCooldown = 60 * time.Second

// seriesBreaker is an open circuit for the history source. Charts have a working
// fallback, so a single failure is enough to switch over — there is nothing to
// gain from spending more timeouts to confirm it.
type seriesBreaker struct {
	cooldown  time.Duration
	mu        sync.Mutex
	openUntil time.Time
}

func newSeriesBreaker(cooldown time.Duration) *seriesBreaker {
	if cooldown <= 0 {
		cooldown = defaultSeriesBreakerCooldown
	}
	return &seriesBreaker{cooldown: cooldown}
}

func (b *seriesBreaker) allows(now time.Time) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.openUntil.IsZero() || now.After(b.openUntil)
}

func (b *seriesBreaker) trip(now time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.openUntil = now.Add(b.cooldown)
	b.mu.Unlock()
}

func (b *seriesBreaker) reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.openUntil = time.Time{}
	b.mu.Unlock()
}
