package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestDeriveTestDatabaseURL_appendsTestSuffix(t *testing.T) {
	got, err := DeriveTestDatabaseURL("postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable")
	if err != nil {
		t.Fatalf("DeriveTestDatabaseURL: %v", err)
	}
	want := "postgres://monaco:monaco@localhost:54322/monaco_test?sslmode=disable"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDeriveTestDatabaseURL_doesNotDoubleSuffix(t *testing.T) {
	in := "postgres://monaco:monaco@localhost:54322/monaco_test?sslmode=disable"
	got, err := DeriveTestDatabaseURL(in)
	if err != nil {
		t.Fatalf("DeriveTestDatabaseURL: %v", err)
	}
	if got != in {
		t.Fatalf("got %q, want %q", got, in)
	}
}

func TestOpenTestDB_connectsToDerivedDatabase(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	// The expected name is derived from the DATABASE_URL under test, not hard-coded:
	// agents and CI jobs run against their own database (monaco_ci, a per-worktree name,
	// …) and each derives its own `_test` sibling.
	testURL, err := DeriveTestDatabaseURL(databaseURL)
	if err != nil {
		t.Fatalf("DeriveTestDatabaseURL: %v", err)
	}
	want, err := testDatabaseName(testURL)
	if err != nil {
		t.Fatalf("testDatabaseName: %v", err)
	}

	db := OpenTestDB(t)
	var name string
	if err := db.QueryRowContext(context.Background(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	if name != want {
		t.Fatalf("current_database = %q, want %q", name, want)
	}
}
