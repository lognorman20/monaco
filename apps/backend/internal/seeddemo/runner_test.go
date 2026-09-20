package seeddemo_test

import (
	"context"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/seeddemo"
)

func TestRunner_ifEmpty_skipsWhenDemoGroupsExist(t *testing.T) {
	ctx := context.Background()
	db := postgres.OpenTestDB(t)
	iso := postgres.PrepareTestDB(t, db)
	store := postgres.NewStore(db)

	user, err := store.UpsertUser(ctx, iso.UniquePrivyID("seed-skip"), "Skip User")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.InsertGroupTx(ctx, tx, seeddemo.GroupNames[0], user.ID); err != nil {
		t.Fatalf("InsertGroupTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	runner := &seeddemo.Runner{
		DB:    db,
		Store: store,
		Privy: privy.NewFakeClient(),
	}
	result, err := runner.Run(ctx, seeddemo.Options{IfEmpty: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Skipped {
		t.Fatalf("result = %+v, want skipped", result)
	}
}

func TestResolveUser_signInFirstWhenNoUsers(t *testing.T) {
	ctx := context.Background()
	db := postgres.OpenTestDB(t)
	_ = postgres.PrepareTestDB(t, db)

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Skip("shared test DB has users from other packages")
	}

	_, err := seeddemo.ResolveUser(ctx, db, "", "")
	if err == nil {
		t.Fatal("expected error when no users exist")
	}
	if err != seeddemo.ErrSignInFirst {
		t.Fatalf("err = %v, want ErrSignInFirst", err)
	}
}

func TestRunner_seedsAllDemoGroups(t *testing.T) {
	ctx := context.Background()
	db := postgres.OpenTestDB(t)
	iso := postgres.PrepareTestDB(t, db)
	store := postgres.NewStore(db)

	user, err := store.UpsertUser(ctx, iso.UniquePrivyID("exists"), "Exists")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.InsertGroupTx(ctx, tx, "Not a demo cabal", user.ID); err != nil {
		t.Fatalf("InsertGroupTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	runner := &seeddemo.Runner{
		DB:    db,
		Store: store,
		Privy: privy.NewFakeClient(),
		Now:   func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	}
	result, err := runner.Run(ctx, seeddemo.Options{UserID: user.ID, IfEmpty: false})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Skipped {
		t.Fatalf("unexpected skip: %+v", result)
	}
	if len(result.GroupIDs) != len(seeddemo.GroupNames) {
		t.Fatalf("group ids = %d, want %d", len(result.GroupIDs), len(seeddemo.GroupNames))
	}
}
