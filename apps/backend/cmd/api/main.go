package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/httpapi"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		if err := postgres.ApplyFromEnv(ctx, databaseURL, postgres.MigrationsDir()); err != nil {
			log.Fatalf("apply migrations: %v", err)
		}
	}

	addr := "127.0.0.1:8080"
	if v := os.Getenv("API_ADDR"); v != "" {
		addr = v
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", httpapi.HealthHandler)

	log.Printf("listening on http://%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
