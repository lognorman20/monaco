package postgres

import (
	"context"
	"testing"
)

func TestSession_samePrivyUserTwice_doesNotDuplicateUsersRow(t *testing.T) {
	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)
	privyUserID := iso.UniquePrivyID("dup")

	first, err := store.UpsertUser(ctx, privyUserID, "Alfred")
	if err != nil {
		t.Fatalf("first UpsertUser: %v", err)
	}
	iso.TrackUser(first.ID)

	second, err := store.UpsertUser(ctx, privyUserID, "Alfred")
	if err != nil {
		t.Fatalf("second UpsertUser: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected same user id, got first=%s second=%s", first.ID, second.ID)
	}

	var rowCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE privy_user_id = $1", privyUserID).Scan(&rowCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected 1 users row, got %d", rowCount)
	}
}
