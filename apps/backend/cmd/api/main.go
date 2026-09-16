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
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/worker"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// bootResult holds API wiring produced at startup.
type bootResult struct {
	Server     *http.Server
	Config     *config.Config
	Relayer    *config.Relayer
	DB         *sql.DB
	stopPoller context.CancelFunc
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
	deposits := app.NewDepositService(store, privyClient)
	auth := &httpapi.AuthHandlers{Sessions: sessions}
	me := &httpapi.MeHandlers{Sessions: sessions}
	groupHandlers := &httpapi.GroupHandlers{Groups: groups}
	depositHandlers := &httpapi.DepositHandlers{Deposits: deposits}
	jupiterClient := jupiter.NewHTTPClient()
	xstocksResolver := xstocks.NewHTTPResolver()
	buy := app.NewBuyService(jupiterClient, xstocksResolver)
	treasurySigner := app.NewPrivyTreasurySigner(privyClient)
	swap := app.NewSwapService(store, buy, jupiterClient, privyClient, treasurySigner)
	devBuy := app.NewDevBuyService(swap, store, privyClient)
	devBuyHandlers := &httpapi.DevBuyHandlers{DevBuy: devBuy}
	transactionHandlers := &httpapi.TransactionHandlers{
		Store:   store,
		Privy:   privyClient,
		XStocks: xstocksResolver,
	}

	addr := "127.0.0.1:8080"
	if v := os.Getenv("API_ADDR"); v != "" {
		addr = v
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", httpapi.HealthHandler)
	mux.HandleFunc("POST /v1/auth/session", auth.SessionHandler)
	mux.HandleFunc("GET /v1/me", me.MeHandler)
	mux.HandleFunc("POST /v1/groups", groupHandlers.CreateGroupHandler)
	mux.HandleFunc("GET /v1/groups/{id}", groupHandlers.GetGroupHandler)
	mux.HandleFunc("POST /v1/groups/{id}/deposits", depositHandlers.CreateDepositHandler)
	mux.HandleFunc("GET /v1/groups/{id}/share-units", depositHandlers.GetMemberShareUnitsHandler)
	mux.HandleFunc("GET /v1/groups/{id}/treasury/usdc", depositHandlers.GetTreasuryUsdcBalanceHandler)
	mux.HandleFunc("GET /v1/deposits/{id}", depositHandlers.GetDepositHandler)
	mux.HandleFunc("POST /v1/dev/groups/{id}/buy", devBuyHandlers.DevBuyHandler)
	mux.HandleFunc("GET /v1/transactions/{id}", transactionHandlers.GetTransactionHandler)
	mux.HandleFunc("GET /v1/groups/{id}/treasury/tokens", transactionHandlers.GetTreasuryTokenBalancesHandler)
	mux.HandleFunc("GET /v1/groups/{id}/cost-basis/{symbol}", transactionHandlers.GetCostBasisBySymbolHandler)

	solanaRPC := worker.NewHTTPSolanaRPC(cfg.SolanaCluster)
	poller := worker.NewSweepPoller(store, privyClient, solanaRPC, deposits, relayer.PrivateKey(), nil)
	pollerCtx, stopPoller := context.WithCancel(context.Background())
	go worker.Run(pollerCtx, poller, worker.DefaultPollInterval)

	return &bootResult{
		Server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		Config:     cfg,
		Relayer:    relayer,
		DB:         db,
		stopPoller: stopPoller,
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
