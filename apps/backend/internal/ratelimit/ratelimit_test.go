package ratelimit

import (
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestLimiter(burst int, interval time.Duration) (*Limiter, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
	return New(burst, interval).WithClock(clock.Now), clock
}

func TestAllow_spendsBurstThenRejectsWithRetryAfter(t *testing.T) {
	limiter, _ := newTestLimiter(3, 10*time.Second)

	for i := 0; i < 3; i++ {
		if ok, _ := limiter.Allow("user-a"); !ok {
			t.Fatalf("request %d rejected, want allowed within burst", i+1)
		}
	}

	ok, retryAfter := limiter.Allow("user-a")
	if ok {
		t.Fatal("4th request allowed, want rejected after burst")
	}
	if retryAfter != 10*time.Second {
		t.Fatalf("retryAfter = %v, want 10s", retryAfter)
	}
}

func TestAllow_refillsOneTokenPerInterval(t *testing.T) {
	limiter, clock := newTestLimiter(2, 10*time.Second)
	limiter.Allow("user-a")
	limiter.Allow("user-a")

	clock.Advance(4 * time.Second)
	ok, retryAfter := limiter.Allow("user-a")
	if ok {
		t.Fatal("allowed after partial refill, want rejected")
	}
	if retryAfter != 6*time.Second {
		t.Fatalf("retryAfter = %v, want 6s remaining", retryAfter)
	}

	clock.Advance(6 * time.Second)
	if ok, _ := limiter.Allow("user-a"); !ok {
		t.Fatal("rejected after full interval, want allowed")
	}
	if ok, _ := limiter.Allow("user-a"); ok {
		t.Fatal("second request after one refill allowed, want rejected")
	}
}

func TestAllow_refillNeverExceedsBurst(t *testing.T) {
	limiter, clock := newTestLimiter(2, time.Second)
	limiter.Allow("user-a")

	clock.Advance(time.Hour)
	allowed := 0
	for i := 0; i < 5; i++ {
		if ok, _ := limiter.Allow("user-a"); ok {
			allowed++
		}
	}
	if allowed != 2 {
		t.Fatalf("allowed = %d after long idle, want burst of 2", allowed)
	}
}

func TestAllow_keysAreIndependent(t *testing.T) {
	limiter, _ := newTestLimiter(1, time.Minute)

	if ok, _ := limiter.Allow("user-a"); !ok {
		t.Fatal("user-a first request rejected")
	}
	if ok, _ := limiter.Allow("user-a"); ok {
		t.Fatal("user-a second request allowed, want rejected")
	}
	if ok, _ := limiter.Allow("user-b"); !ok {
		t.Fatal("user-b rejected because of user-a, want independent buckets")
	}
}

func TestAllow_sweepsIdleBuckets(t *testing.T) {
	limiter, clock := newTestLimiter(1, time.Second)
	limiter.Allow("user-a")
	limiter.Allow("user-b")
	if limiter.Len() != 2 {
		t.Fatalf("len = %d, want 2", limiter.Len())
	}

	clock.Advance(2 * time.Minute)
	limiter.Allow("user-c")
	if limiter.Len() != 1 {
		t.Fatalf("len = %d after sweep, want only the fresh key", limiter.Len())
	}
}

func TestAllow_concurrentCallersNeverExceedBurst(t *testing.T) {
	limiter, _ := newTestLimiter(5, time.Hour)

	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := limiter.Allow("user-a"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 5 {
		t.Fatalf("allowed = %d under concurrency, want exactly 5", allowed)
	}
}

func TestBlocked_reportsWithoutSpending(t *testing.T) {
	limiter, clock := newTestLimiter(2, 10*time.Second)

	if over, _ := limiter.Blocked("group-a"); over {
		t.Fatal("unknown key blocked, want open")
	}
	limiter.Allow("group-a")
	for i := 0; i < 5; i++ {
		if over, _ := limiter.Blocked("group-a"); over {
			t.Fatal("blocked with one request left; Blocked must not spend")
		}
	}

	limiter.Allow("group-a")
	over, wait := limiter.Blocked("group-a")
	if !over || wait != 10*time.Second {
		t.Fatalf("Blocked = (%v, %v), want (true, 10s) once the burst is spent", over, wait)
	}

	clock.Advance(10 * time.Second)
	if over, _ := limiter.Blocked("group-a"); over {
		t.Fatal("still blocked after one interval, want open")
	}
}
