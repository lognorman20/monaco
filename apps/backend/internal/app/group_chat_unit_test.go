package app

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestKeyedRateLimiter_burstThenBlocksUntilRefill(t *testing.T) {
	// Arrange
	clock := &fakeClock{t: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	limiter := NewKeyedRateLimiter(3, time.Second, clock.now)

	// Act + Assert: burst of 3 passes, 4th blocks with a positive wait.
	for i := 0; i < 3; i++ {
		if ok, _ := limiter.Allow("user-a"); !ok {
			t.Fatalf("call %d blocked, want allowed within burst", i+1)
		}
	}
	ok, wait := limiter.Allow("user-a")
	if ok {
		t.Fatal("4th call allowed, want blocked after burst")
	}
	if wait <= 0 || wait > time.Second {
		t.Fatalf("wait = %s, want (0, 1s]", wait)
	}

	// One refill interval later exactly one more call passes.
	clock.advance(time.Second)
	if ok, _ := limiter.Allow("user-a"); !ok {
		t.Fatal("call after refill blocked, want allowed")
	}
	if ok, _ := limiter.Allow("user-a"); ok {
		t.Fatal("second call after single refill allowed, want blocked")
	}
}

func TestKeyedRateLimiter_keysAreIndependent(t *testing.T) {
	// Arrange
	clock := &fakeClock{t: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	limiter := NewKeyedRateLimiter(1, time.Minute, clock.now)

	// Act
	okA, _ := limiter.Allow("user-a")
	blockedA, _ := limiter.Allow("user-a")
	okB, _ := limiter.Allow("user-b")

	// Assert
	if !okA || blockedA || !okB {
		t.Fatalf("okA=%v blockedA=%v okB=%v, want true false true", okA, blockedA, okB)
	}
}

func TestKeyedRateLimiter_refillCapsAtBurst(t *testing.T) {
	// Arrange
	clock := &fakeClock{t: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	limiter := NewKeyedRateLimiter(2, time.Second, clock.now)
	limiter.Allow("user-a")

	// Act: a long idle period must not bank more than burst tokens.
	clock.advance(time.Hour)
	first, _ := limiter.Allow("user-a")
	second, _ := limiter.Allow("user-a")
	third, _ := limiter.Allow("user-a")

	// Assert
	if !first || !second || third {
		t.Fatalf("got %v %v %v, want true true false", first, second, third)
	}
}

func TestGroupMessageCursor_roundTripPreservesMicroseconds(t *testing.T) {
	// Arrange
	want := postgres.GroupMessageCursor{
		CreatedAt: time.Date(2026, 9, 18, 15, 4, 5, 123456000, time.UTC),
		ID:        "3f2b8a4e-9c1d-4e6f-8a7b-0c1d2e3f4a5b",
	}

	// Act
	got, err := decodeGroupMessageCursor(encodeGroupMessageCursor(want))

	// Assert
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestGroupMessageCursor_rejectsMalformedInput(t *testing.T) {
	cases := map[string]string{
		"not base64":        "%%%",
		"missing separator": base64.RawURLEncoding.EncodeToString([]byte("2026-09-18T15:04:05Z")),
		"bad uuid":          base64.RawURLEncoding.EncodeToString([]byte("2026-09-18T15:04:05Z|not-a-uuid")),
		"bad timestamp":     base64.RawURLEncoding.EncodeToString([]byte("yesterday|3f2b8a4e-9c1d-4e6f-8a7b-0c1d2e3f4a5b")),
		"sql injection":     base64.RawURLEncoding.EncodeToString([]byte("2026-09-18T15:04:05Z|'; DROP TABLE users;--")),
	}
	for name, cursor := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			_, err := decodeGroupMessageCursor(cursor)

			// Assert
			if !errors.Is(err, ErrInvalidMessageCursor) {
				t.Fatalf("err = %v, want ErrInvalidMessageCursor", err)
			}
		})
	}
}
