package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/httpapi"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// bootResult holds API wiring produced at startup.
type bootResult struct {
	Server  *http.Server
	Config  *config.Config
	Relayer *config.Relayer
	DB      *sql.DB
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

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	store := postgres.NewStore(db)
	privyClient := privy.NewHTTPClient(cfg)
	sessions := app.NewSessionService(store, privyClient)
	groups := app.NewGroupService(store, privyClient)
	auth := &httpapi.AuthHandlers{Sessions: sessions}
	groupHandlers := &httpapi.GroupHandlers{Groups: groups}

	addr := "127.0.0.1:8080"
	if v := os.Getenv("API_ADDR"); v != "" {
		addr = v
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", httpapi.HealthHandler)
	mux.HandleFunc("POST /v1/auth/session", auth.SessionHandler)
	mux.HandleFunc("POST /v1/groups", groupHandlers.CreateGroupHandler)

	return &bootResult{
		Server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		Config:  cfg,
		Relayer: relayer,
		DB:      db,
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
