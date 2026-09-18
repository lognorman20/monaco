// Command faker-seed seeds #153 demo data straight into the local database (used by `just faker`).
//
//	go run ./cmd/faker-seed -profile scale
//	go run ./cmd/faker-seed -profile mixed -group-id <uuid>
//	go run ./cmd/faker-seed -profile all -group-id <uuid>
//
// It refuses non-local DATABASE_URLs, applies migrations, and never calls Privy, Solana RPC, or
// Jupiter. PYTH_API_KEY (optional) anchors seeded cost bases near live marks.
// Unlike POST /v1/dev/faker it has no session: whoever runs it owns the local database, so the
// mixed profile only checks that the group exists and is real.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/faker"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "faker-seed:", err)
		os.Exit(1)
	}
}

func run() error {
	profile := flag.String("profile", "", `profile to seed: "mixed", "scale", or "all"`)
	groupID := flag.String("group-id", "", "real club id for mixed/all (your own club)")
	flag.Parse()

	p := strings.ToLower(strings.TrimSpace(*profile))
	switch p {
	case "scale":
	case "mixed", "all":
		if strings.TrimSpace(*groupID) == "" {
			return fmt.Errorf("-group-id is required for profile %s", p)
		}
	default:
		return fmt.Errorf(`-profile must be "mixed", "scale", or "all"`)
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if !config.IsLocalDatabaseURL(databaseURL) {
		return fmt.Errorf("DATABASE_URL must point at local Postgres (localhost/127.0.0.1); refusing to seed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := postgres.ApplyFromEnv(ctx, databaseURL, postgres.MigrationsDir()); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	var marks faker.MarkSource
	if key := strings.TrimSpace(os.Getenv("PYTH_API_KEY")); key != "" {
		marks = faker.PythMarkSource(pyth.NewHermesClient(key))
	}
	seeder := faker.NewSeeder(postgres.NewStore(db), marks)

	out := map[string]any{"profile": p}
	if p == "mixed" || p == "all" {
		mixed, err := seeder.SeedMixed(ctx, strings.TrimSpace(*groupID))
		if err != nil {
			return err
		}
		out["mixed"] = mixed
	}
	if p == "scale" || p == "all" {
		scale, err := seeder.SeedScale(ctx)
		if err != nil {
			return err
		}
		out["scale"] = scale
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
