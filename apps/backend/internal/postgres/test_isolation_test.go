package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestParallelIsolation_uniquePrivyIDsDoNotConflict(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
		t.Skip("DATABASE_URL is not set")
	}

	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprintf("lane-%d", i), func(t *testing.T) {
			t.Parallel()
			db := OpenTestDB(t)
			iso := PrepareTestDB(t, db)
			ctx := context.Background()
			store := NewStore(db)

			privyID := iso.UniqueDynamicID("lane")
			user, err := store.UpsertUser(ctx, privyID, "Lane")
			if err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}
			iso.TrackUser(user.ID)

			var count int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE dynamic_user_id = $1`, privyID).Scan(&count); err != nil {
				t.Fatalf("count users: %v", err)
			}
			if count != 1 {
				t.Fatalf("expected 1 user row, got %d", count)
			}
		})
	}
}

func TestTestIsolation_cleanupRemovesTrackedRows(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
		t.Skip("DATABASE_URL is not set")
	}

	db := OpenTestDB(t)
	iso := PrepareTestDB(t, db)
	ctx := context.Background()
	store := NewStore(db)

	privyID := iso.UniqueDynamicID("cleanup")
	user, err := store.UpsertUser(ctx, privyID, "Cleanup")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)

	if err := iso.cleanup(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = $1`, user.ID).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected user deleted after cleanup, count=%d", count)
	}
}
