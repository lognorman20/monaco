package app

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

func TestGroupSearchCursor_roundTrips(t *testing.T) {
	// Arrange
	want := postgres.GroupSearchCursor{MatchRank: 1, SortName: "weekend 50%_club", GroupID: "550e8400-e29b-41d4-a716-446655440001"}

	// Act
	got, err := decodeGroupSearchCursor(encodeGroupSearchCursor(want))

	// Assert
	if err != nil || got != want {
		t.Fatalf("round trip = %+v, %v; want %+v", got, err, want)
	}
}

func TestDecodeGroupSearchCursor_rejectsForgedOrMalformedCursors(t *testing.T) {
	forged := func(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }
	cases := map[string]string{
		"not base64":   "%%%",
		"not json":     forged("hello"),
		"bad rank":     forged(`{"r":7,"n":"a","i":"550e8400-e29b-41d4-a716-446655440001"}`),
		"bad group id": forged(`{"r":0,"n":"a","i":"'; drop table groups;--"}`),
		"oversized":    string(make([]byte, 600)),
	}
	for name, cursor := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeGroupSearchCursor(cursor); !errors.Is(err, ErrInvalidGroupSearchCursor) {
				t.Fatalf("err = %v, want ErrInvalidGroupSearchCursor", err)
			}
		})
	}
}

func TestIsWellFormedGroupID(t *testing.T) {
	if !IsWellFormedGroupID("550E8400-e29b-41d4-a716-446655440001") {
		t.Fatal("uuid rejected")
	}
	for _, bad := range []string{"", "abc", "550e8400e29b41d4a716446655440001", "550e8400-e29b-41d4-a716-44665544000z"} {
		if IsWellFormedGroupID(bad) {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestClampGroupsTabLimit(t *testing.T) {
	cases := map[int]int{0: GroupsTabDefaultLimit, 1: 1, 20: 20, GroupsTabMaxLimit: GroupsTabMaxLimit, 500: GroupsTabMaxLimit}
	for in, want := range cases {
		if got := ClampGroupsTabLimit(in); got != want {
			t.Fatalf("ClampGroupsTabLimit(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestGroupValuationCache_expiresAfterTTL(t *testing.T) {
	// Arrange
	cache := newGroupValuationCache(30 * time.Second)
	valuedAt := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	cache.put("g", groupValuation{PotNavMicros: 1, ValuedAt: valuedAt})

	// Act
	_, freshHit := cache.get("g", valuedAt.Add(29*time.Second), time.Time{})
	_, staleHit := cache.get("g", valuedAt.Add(31*time.Second), time.Time{})

	// Assert
	if !freshHit || staleHit {
		t.Fatalf("fresh hit = %v, stale hit = %v; want true, false", freshHit, staleHit)
	}
}

func TestGroupValuationCache_missesWhenOlderThanLatestSnapshot(t *testing.T) {
	// Arrange: a fund landed (new snapshot) after the cached valuation.
	cache := newGroupValuationCache(30 * time.Second)
	valuedAt := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	cache.put("g", groupValuation{PotNavMicros: 1, ValuedAt: valuedAt})

	// Act
	_, hit := cache.get("g", valuedAt.Add(5*time.Second), valuedAt.Add(time.Second))

	// Assert
	if hit {
		t.Fatal("expected a miss for a valuation older than the latest snapshot")
	}
}

func TestGroupValuationCache_neverStoresDegradedValuations(t *testing.T) {
	cache := newGroupValuationCache(30 * time.Second)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	cache.put("g", groupValuation{PotNavMicros: 1, ValuedAt: now, Degraded: true})
	if _, hit := cache.get("g", now, time.Time{}); hit {
		t.Fatal("degraded valuation was cached")
	}
}
