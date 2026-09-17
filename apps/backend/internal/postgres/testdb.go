package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const duplicateDatabaseSQLState = "42P04"

var testDBNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

var (
	ensureTestDBOnce sync.Once
	ensuredTestURL   string
	ensureTestDBErr  error
)

type testFataler interface {
	Helper()
	Fatalf(format string, args ...any)
	Cleanup(fn func())
}

// DeriveTestDatabaseURL maps DATABASE_URL onto a sibling `{dbname}_test` database.
// Callers do not set a second env var; tests always rewrite the app DSN.
func DeriveTestDatabaseURL(databaseURL string) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" {
		return "", fmt.Errorf("DATABASE_URL has no database name")
	}
	if slash := strings.IndexByte(name, '/'); slash >= 0 {
		name = name[:slash]
	}
	if !strings.HasSuffix(name, "_test") {
		name += "_test"
	}
	if !testDBNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid test database name %q", name)
	}
	u.Path = "/" + name
	return u.String(), nil
}

func testDatabaseName(databaseURL string) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", err
	}
	name := strings.TrimPrefix(u.Path, "/")
	if slash := strings.IndexByte(name, '/'); slash >= 0 {
		name = name[:slash]
	}
	if name == "" {
		return "", fmt.Errorf("DATABASE_URL has no database name")
	}
	return name, nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func ensureTestDatabase(ctx context.Context, databaseURL string) (string, error) {
	testURL, err := DeriveTestDatabaseURL(databaseURL)
	if err != nil {
		return "", err
	}
	testName, err := testDatabaseName(testURL)
	if err != nil {
		return "", err
	}

	admin, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return "", fmt.Errorf("open admin db: %w", err)
	}
	defer admin.Close()
	if err := admin.PingContext(ctx); err != nil {
		return "", fmt.Errorf("ping admin db: %w", err)
	}

	var exists bool
	if err := admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, testName).Scan(&exists); err != nil {
		return "", fmt.Errorf("lookup test database: %w", err)
	}
	if !exists {
		_, err := admin.ExecContext(ctx, "CREATE DATABASE "+quoteIdent(testName))
		if err != nil {
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != duplicateDatabaseSQLState {
				return "", fmt.Errorf("create test database %s: %w", testName, err)
			}
		}
	}

	testDB, err := sql.Open("pgx", testURL)
	if err != nil {
		return "", fmt.Errorf("open test db: %w", err)
	}
	defer testDB.Close()
	if err := testDB.PingContext(ctx); err != nil {
		return "", fmt.Errorf("ping test db: %w", err)
	}
	if err := Apply(ctx, testDB, MigrationsDir()); err != nil {
		return "", fmt.Errorf("migrate test db: %w", err)
	}
	return testURL, nil
}

func ensuredTestDatabaseURL(ctx context.Context, databaseURL string) (string, error) {
	ensureTestDBOnce.Do(func() {
		ensuredTestURL, ensureTestDBErr = ensureTestDatabase(ctx, databaseURL)
	})
	return ensuredTestURL, ensureTestDBErr
}

// OpenTestDB connects to the derived `{dbname}_test` database, never the app DATABASE_URL path.
func OpenTestDB(t testFataler) *sql.DB {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Fatalf("DATABASE_URL is not set")
	}
	ctx := context.Background()
	testURL, err := ensuredTestDatabaseURL(ctx, databaseURL)
	if err != nil {
		t.Fatalf("ensure test database: %v", err)
	}
	db, err := sql.Open("pgx", testURL)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
