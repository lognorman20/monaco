package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	if err := postgres.ApplyFromEnv(ctx, databaseURL, postgres.MigrationsDir()); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}
}
