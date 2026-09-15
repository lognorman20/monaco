package postgres

import (
	"context"
	"testing"
)

func TestMigrationRunner_appliesInitialMigrationOnEmptyDb(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS schema_migrations"); err != nil {
		t.Fatalf("drop schema_migrations: %v", err)
	}

	// Act
	if err := Apply(ctx, db, MigrationsDir()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Assert
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one schema_migrations row")
	}
}

func TestMigrationRunner_isIdempotentOnSecondRun(t *testing.T) {
	// Arrange
	ctx := context.Background()
	db := integrationDB(t)

	// Act
	if err := Apply(ctx, db, MigrationsDir()); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	var firstCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&firstCount); err != nil {
		t.Fatalf("count after first run: %v", err)
	}
	if err := Apply(ctx, db, MigrationsDir()); err != nil {
		t.Fatalf("second Apply: %v", err)
	}

	// Assert
	var secondCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&secondCount); err != nil {
		t.Fatalf("count after second run: %v", err)
	}
	if firstCount != secondCount {
		t.Fatalf("migration count changed: first=%d second=%d", firstCount, secondCount)
	}
}
