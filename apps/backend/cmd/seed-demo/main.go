// seed-demo backfills local Postgres with demo cabals for app walkthroughs.
//
// Prerequisites: just reset db (or fresh clone), just run backend, sign in once
// via the mobile app or POST /v1/auth/session so a users row exists.
//
// Usage: just seed demo [--user-id UUID] [--privy-user-id DID] [--if-empty=false]
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/seeddemo"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	var (
		userID      = flag.String("user-id", "", "Monaco users.id to attach joined demo cabals (default: most recent user)")
		privyUserID = flag.String("privy-user-id", "", "Resolve user by privy_user_id instead of --user-id")
		ifEmpty     = flag.Bool("if-empty", true, "Skip when demo cabal names already exist")
	)
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := postgres.ApplyFromEnv(ctx, cfg.DatabaseURL, postgres.MigrationsDir()); err != nil {
		fmt.Fprintf(os.Stderr, "migrations: %v\n", err)
		os.Exit(1)
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "database ping: %v\n", err)
		os.Exit(1)
	}

	runner := &seeddemo.Runner{
		DB:    db,
		Store: postgres.NewStore(db),
		Privy: privy.NewHTTPClient(cfg),
		Now:   time.Now,
	}

	result, err := runner.Run(ctx, seeddemo.Options{
		UserID:      *userID,
		PrivyUserID: *privyUserID,
		IfEmpty:     *ifEmpty,
	})
	if err != nil {
		if err == seeddemo.ErrSignInFirst {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "seed demo: %v\n", err)
		os.Exit(1)
	}

	if result.Skipped {
		fmt.Println(result.SkipReason)
		return
	}

	fmt.Printf("seed demo ok for user %s\n", result.UserID)
	for name, id := range result.GroupIDs {
		fmt.Printf("  %s: %s\n", name, id)
	}
}
