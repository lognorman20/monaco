package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is not set")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatalf("ping db: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func resetTables(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx := context.Background()
	_, err := db.ExecContext(ctx, "TRUNCATE users, member_wallets, groups, treasuries RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("reset tables: %v", err)
	}
}
