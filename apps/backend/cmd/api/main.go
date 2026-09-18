package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/httpapi"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/solana/balance"
	"github.com/monaco/monaco/apps/backend/internal/worker"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// bootResult holds API wiring produced at startup.
type bootResult struct {
	Server            *http.Server
	Config            *config.Config
	Relayer           *config.Relayer
	DB                *sql.DB
	stopPoller        context.CancelFunc
	stopExecutePoller context.CancelFunc
}

var apiRoutes = []string{
	"GET /health",
	"POST /v1/auth/session",
	"GET /v1/me",
	"GET /v1/home",
	"POST /v1/groups",
	"POST /v1/groups/{id}/join",
	"GET /v1/groups/{id}",
	"GET /v1/groups/{id}/view",
	"GET /v1/groups/{id}/activity",
	"GET /v1/groups/{id}/proposals",
	"POST /v1/groups/{id}/deposits",
	"GET /v1/groups/{id}/share-units",
	"GET /v1/groups/{id}/treasury/usdc",
	"GET /v1/deposits/{id}",
	"GET /v1/transactions/{id}",
	"POST /v1/transactions/{id}/retry",
	"GET /v1/groups/{id}/treasury/tokens",
	"GET /v1/groups/{id}/cost-basis/{symbol}",
	"GET /v1/groups/{id}/assets",
	"POST /v1/groups/{id}/quotes",
	"POST /v1/groups/{id}/proposals",
	"GET /v1/proposals/{id}",
	"POST /v1/proposals/{id}/votes",
}

// boot loads config, registers the relayer fee payer, applies migrations, and builds the HTTP server.
func boot(ctx context.Context) (*bootResult, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	logConfigLoaded(cfg)

	relayer, err := config.LoadRelayer(cfg)
	if err != nil {
		return nil, err
	}
	slog.Info("relayer loaded", "pubkey", relayer.PublicKey())

	solanaRPC := worker.NewHTTPSolanaRPC(cfg.SolanaCluster)
	if err := balance.MustHaveSOL(ctx, solanaRPC, relayer.PublicKey(), balance.FeePayerMinLamports); err != nil {
		return nil, err
	}
	slog.Info("relayer SOL balance ok", "pubkey", relayer.PublicKey(), "min_lamports", balance.FeePayerMinLamports)

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
	slog.Info("database connected")

	store := postgres.NewStore(db)
	privyClient := privy.NewHTTPClient(cfg)
	var pythClient pyth.Client
	if cfg.PythAPIKey != "" {
		pythClient, err = pyth.NewHermesClientFromConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("pyth client: %w", err)
		}
		slog.Info("pyth client ready")
	} else {
		slog.Info("pyth client skipped", "reason", "PYTH_API_KEY unset")
	}
	catalogSearcher := xstocks.NewHTTPCatalogSearcher()
	jupiterClient := jupiter.NewHTTPClientWithPayer(relayer.PublicKey())
	catalogRoutability := xstocks.NewCachedRoutabilityProber(
		app.NewJupiterCatalogRoutabilityProber(jupiterClient),
		xstocks.NewRoutabilityCache(xstocks.DefaultRoutabilityCacheTTL),
	)
	catalogSearcher.SetRoutabilityProber(catalogRoutability)
	symbols := app.NewSymbolResolver(catalogSearcher)
	deposits := app.NewDepositService(store, privyClient, pythClient, symbols)
	sessions := app.NewSessionService(store, privyClient)
	home := app.NewHomeService(store, privyClient, pythClient, deposits, symbols)
	groups := app.NewGroupService(store, privyClient)
	governance := app.NewGovernanceService(store, privyClient)
	auth := &httpapi.AuthHandlers{Sessions: sessions}
	me := &httpapi.MeHandlers{Sessions: sessions}
	homeHandlers := &httpapi.HomeHandlers{Home: home}
	groupHandlers := &httpapi.GroupHandlers{Groups: groups, Governance: governance, Home: home}
	depositHandlers := &httpapi.DepositHandlers{Deposits: deposits}
	xstocksResolver := xstocks.NewHTTPResolver()
	buy := app.NewBuyService(jupiterClient, xstocksResolver)
	signer := app.NewPrivyTreasurySigner(privyClient)
	swap := app.NewSwapService(store, buy, jupiterClient, privyClient, signer, relayer.PrivateKey(), symbols)
	executeOnPass := app.NewExecuteOnPassService(swap, store)
	governance.SetBuyService(buy)
	transactionHandlers := &httpapi.TransactionHandlers{
		Store:    store,
		Privy:    privyClient,
		XStocks:  xstocksResolver,
		Swap:     swap,
		Symbols:  symbols,
	}
	catalogHandlers := &httpapi.CatalogHandlers{
		Store:   store,
		Privy:   privyClient,
		Catalog: catalogSearcher,
	}
	quoteHandlers := &httpapi.QuoteHandlers{
		Store: store,
		Privy: privyClient,
		Buy:   buy,
	}
	proposalHandlers := &httpapi.ProposalHandlers{
		Store:      store,
		Privy:      privyClient,
		Governance: governance,
	}

	addr := "127.0.0.1:8080"
	if v := os.Getenv("API_ADDR"); v != "" {
		addr = v
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", httpapi.HealthHandler)
	mux.HandleFunc("POST /v1/auth/session", auth.SessionHandler)
	mux.HandleFunc("GET /v1/me", me.MeHandler)
	mux.HandleFunc("GET /v1/home", homeHandlers.HomeHandler)
	mux.HandleFunc("POST /v1/groups", groupHandlers.CreateGroupHandler)
	mux.HandleFunc("POST /v1/groups/{id}/join", groupHandlers.JoinGroupHandler)
	mux.HandleFunc("POST /v1/groups/{id}/leave", groupHandlers.LeaveGroupHandler)
	mux.HandleFunc("GET /v1/groups/{id}/join-requests", groupHandlers.ListJoinRequestsHandler)
	mux.HandleFunc("POST /v1/groups/{id}/join-requests/{requestId}/approve", groupHandlers.ApproveJoinRequestHandler)
	mux.HandleFunc("POST /v1/groups/{id}/join-requests/{requestId}/deny", groupHandlers.DenyJoinRequestHandler)
	mux.HandleFunc("GET /v1/groups/{id}", groupHandlers.GetGroupHandler)
	mux.HandleFunc("GET /v1/groups/{id}/view", groupHandlers.GetGroupViewHandler)
	mux.HandleFunc("GET /v1/groups/{id}/activity", groupHandlers.ListGroupActivityHandler)
	mux.HandleFunc("POST /v1/groups/{id}/deposits", depositHandlers.CreateDepositHandler)
	mux.HandleFunc("GET /v1/groups/{id}/share-units", depositHandlers.GetMemberShareUnitsHandler)
	mux.HandleFunc("GET /v1/groups/{id}/treasury/usdc", depositHandlers.GetTreasuryUsdcBalanceHandler)
	mux.HandleFunc("GET /v1/deposits/{id}", depositHandlers.GetDepositHandler)
	mux.HandleFunc("GET /v1/transactions/{id}", transactionHandlers.GetTransactionHandler)
	mux.HandleFunc("POST /v1/transactions/{id}/retry", transactionHandlers.RetryTransactionHandler)
	mux.HandleFunc("GET /v1/groups/{id}/treasury/tokens", transactionHandlers.GetTreasuryTokenBalancesHandler)
	mux.HandleFunc("GET /v1/groups/{id}/cost-basis/{symbol}", transactionHandlers.GetCostBasisBySymbolHandler)
	mux.HandleFunc("GET /v1/groups/{id}/assets", catalogHandlers.SearchAssetsHandler)
	mux.HandleFunc("POST /v1/groups/{id}/quotes", quoteHandlers.QuoteHandler)
	mux.HandleFunc("GET /v1/groups/{id}/proposals", proposalHandlers.ListGroupProposalsHandler)
	mux.HandleFunc("POST /v1/groups/{id}/proposals", proposalHandlers.CreateProposalHandler)
	mux.HandleFunc("GET /v1/proposals/{id}", proposalHandlers.GetProposalDetailHandler)
	mux.HandleFunc("POST /v1/proposals/{id}/votes", proposalHandlers.CastVoteHandler)
	logRoutesReady(apiRoutes)

	poller := worker.NewSweepPoller(store, privyClient, solanaRPC, deposits, relayer.PrivateKey(), nil)
	pollerCtx, stopPoller := context.WithCancel(context.Background())
	go worker.Run(pollerCtx, poller, worker.DefaultPollInterval)
	slog.Info("sweep poller started")

	executePoller := worker.NewProposalExecutePoller(store, executeOnPass, nil)
	executeCtx, stopExecutePoller := context.WithCancel(context.Background())
	go worker.RunProposalExecutePoller(executeCtx, executePoller, worker.DefaultProposalExecuteInterval)
	slog.Info("proposal execute poller started")

	return &bootResult{
		Server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		Config:            cfg,
		Relayer:           relayer,
		DB:                db,
		stopPoller:        stopPoller,
		stopExecutePoller: stopExecutePoller,
	}, nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := boot(ctx)
	if err != nil {
		slog.Error("boot failed", "err", err)
		os.Exit(1)
	}

	slog.Info("listening", "addr", result.Server.Addr, "url", "http://"+result.Server.Addr)

	serverErr := make(chan error, 1)
	go func() {
		if err := result.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	case sig := <-stop:
		slog.Info("shutdown signal received", "signal", sig.String())
	}

	result.stopPoller()
	slog.Info("sweep poller stopped")
	result.stopExecutePoller()
	slog.Info("proposal execute poller stopped")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := result.Server.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown failed", "err", err)
	}
	if err := result.DB.Close(); err != nil {
		slog.Warn("database close failed", "err", err)
	}
	slog.Info("shutdown complete")
}
