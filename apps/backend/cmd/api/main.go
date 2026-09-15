package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/httpapi"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// bootResult holds API wiring produced at startup.
type bootResult struct {
	Server  *http.Server
	Config  *config.Config
	Relayer *config.Relayer
}

// boot loads config, registers the relayer fee payer, applies migrations, and builds the HTTP server.
func boot(ctx context.Context) (*bootResult, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	relayer, err := config.LoadRelayer(cfg)
	if err != nil {
		return nil, err
	}

	if err := postgres.ApplyFromEnv(ctx, cfg.DatabaseURL, postgres.MigrationsDir()); err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	addr := "127.0.0.1:8080"
	if v := os.Getenv("API_ADDR"); v != "" {
		addr = v
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", httpapi.HealthHandler)

	return &bootResult{
		Server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		Config:  cfg,
		Relayer: relayer,
	}, nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := boot(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("listening on http://%s", result.Server.Addr)
	if err := result.Server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
