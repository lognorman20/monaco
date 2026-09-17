package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const ensureSchemaMigrationsSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`

// Apply runs pending SQL migrations from migrationsDir against db.
func Apply(ctx context.Context, db *sql.DB, migrationsDir string) error {
	slog.Info("postgres migrations start", "dir", migrationsDir)

	if _, err := db.ExecContext(ctx, ensureSchemaMigrationsSQL); err != nil {
		slog.Debug("postgres migrations ensure table failed", "err", err)
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		slog.Debug("postgres migrations read dir failed", "dir", migrationsDir, "err", err)
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	appliedCount := 0
	for _, name := range files {
		applied, err := isApplied(ctx, db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		path := filepath.Join(migrationsDir, name)
		sqlBytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			slog.Debug("postgres migration apply failed", "version", name, "err", err)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
			_ = tx.Rollback()
			slog.Debug("postgres migration record failed", "version", name, "err", err)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			slog.Debug("postgres migration commit failed", "version", name, "err", err)
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		slog.Info("postgres migration applied", "version", name)
		appliedCount++
	}

	slog.Info("postgres migrations complete", "applied", appliedCount, "total", len(files))
	return nil
}

// ApplyFromEnv connects with databaseURL and applies migrations from migrationsDir.
func ApplyFromEnv(ctx context.Context, databaseURL, migrationsDir string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	return Apply(ctx, db, migrationsDir)
}

func isApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, "SELECT 1 FROM schema_migrations WHERE version = $1 LIMIT 1", version).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lookup migration %s: %w", version, err)
	}
	return true, nil
}

// MigrationsDir resolves the repo migrations path.
func MigrationsDir() string {
	if dir := os.Getenv("MIGRATIONS_DIR"); dir != "" {
		return dir
	}

	wd, err := os.Getwd()
	if err != nil {
		return filepath.Join("..", "..", "supabase", "migrations")
	}

	dir := wd
	for {
		candidate := filepath.Join(dir, "supabase", "migrations")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return filepath.Join("..", "..", "supabase", "migrations")
}
