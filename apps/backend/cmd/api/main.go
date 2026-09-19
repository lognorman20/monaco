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
	"github.com/monaco/monaco/apps/backend/internal/storage"
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
	"PATCH /v1/me",
	"POST /v1/me/profile-photo",
	"GET /v1/me/balance",
	"POST /v1/me/withdrawals",
	"GET /v1/me/withdrawals/{id}",
	"GET /v1/home",
	"GET /v1/home/dashboard",
	"GET /v1/home/pnl-series",
	"GET /v1/home/missed-proposals",
	"POST /v1/groups",
	"GET /v1/groups/search",
	"GET /v1/groups/leaderboard",
	"GET /v1/groups/pnl-history",
	"GET /v1/groups/{id}/pnl-history",
	"POST /v1/groups/{id}/join",
	"POST /v1/groups/{id}/leave",
	"POST /v1/groups/{id}/withdraw-to-balance",
	"GET /v1/groups/{id}",
	"GET /v1/groups/{id}/view",
	"GET /v1/groups/{id}/activity",
	"GET /v1/groups/{id}/proposals",
	"POST /v1/groups/{id}/deposits",
	"POST /v1/groups/{id}/fund",
	"GET /v1/groups/{id}/share-units",
	"GET /v1/groups/{id}/treasury/usdc",
	"GET /v1/deposits/{id}",
	"GET /v1/transactions/{id}",
	"POST /v1/transactions/{id}/retry",
	"GET /v1/groups/{id}/treasury/tokens",
	"GET /v1/groups/{id}/cost-basis/{symbol}",
	"GET /v1/groups/{id}/assets",
	"GET /v1/assets",
	"GET /v1/assets/popular",
	"GET /v1/assets/{symbol}",
	"GET /v1/assets/{symbol}/chart",
	"POST /v1/groups/{id}/quotes",
	"POST /v1/groups/{id}/proposals",
	"GET /v1/proposals/{id}",
	"POST /v1/proposals/{id}/votes",
	"POST /v1/groups/{id}/agents/intents",
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
	platformWithdrawals := app.NewPlatformWithdrawService(store, privyClient, deposits, solanaRPC, relayer.PrivateKey())
	sessions := app.NewSessionService(store, privyClient).
		WithDisplayNameLimiter(app.NewDisplayNameUpdateLimiter())
	var storageClient storage.Client
	if cfg.SupabaseURL != "" && cfg.SupabaseServiceRoleKey != "" {
		storageClient = storage.NewSupabaseClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
		slog.Info("supabase storage ready")
	} else {
		slog.Info("supabase storage skipped", "reason", "SUPABASE_URL or SUPABASE_SERVICE_ROLE_KEY unset")
	}
	profilePhotos := app.NewProfilePhotoService(store, privyClient, storageClient).
		WithUploadLimiter(app.NewProfilePhotoUploadLimiter())
	home := app.NewHomeService(store, privyClient, pythClient, deposits, symbols)
	groups := app.NewGroupService(store, privyClient)
	governance := app.NewGovernanceService(store, privyClient)
	depositHandlers := &httpapi.DepositHandlers{Deposits: deposits}
	platformWithdrawHandlers := &httpapi.PlatformWithdrawHandlers{Withdrawals: platformWithdrawals}
	xstocksResolver := xstocks.NewHTTPResolver()
	buy := app.NewBuyService(jupiterClient, xstocksResolver)
	signer := app.NewPrivyTreasurySigner(privyClient)
	swap := app.NewSwapService(store, buy, jupiterClient, privyClient, signer, relayer.PrivateKey(), symbols)
	redeem := app.NewRedeemService(store, privyClient, pythClient, jupiterClient, swap, signer)
	governance.SetRedeemService(redeem)
	auth := &httpapi.AuthHandlers{Sessions: sessions}
	me := &httpapi.MeHandlers{Sessions: sessions, ProfilePhoto: profilePhotos}
	homeHandlers := &httpapi.HomeHandlers{Home: home}
	groupHandlers := &httpapi.GroupHandlers{Groups: groups, Governance: governance, Home: home, Redeem: redeem}
	groupsTabHandlers := &httpapi.GroupsTabHandlers{GroupsTab: app.NewGroupsTabService(home, store)}
	executeOnPass := app.NewExecuteOnPassService(swap, store)
	governance.SetBuyService(buy)
	governance.SetHomeService(home)
	governance.SetSwapService(swap)
	transactionHandlers := &httpapi.TransactionHandlers{
		Store:   store,
		Privy:   privyClient,
		XStocks: xstocksResolver,
		Swap:    swap,
		Symbols: symbols,
	}
	catalogHandlers := &httpapi.CatalogHandlers{
		Store:   store,
		Privy:   privyClient,
		Catalog: catalogSearcher,
	}
	var assetPrices pyth.AssetPriceClient
	if hermes, ok := pythClient.(*pyth.HermesClient); ok {
		assetPrices = hermes
	}
	assetsHandlers := &httpapi.AssetsHandlers{
		Store:   store,
		Privy:   privyClient,
		Catalog: catalogSearcher,
		Pyth:    assetPrices,
		Jupiter: jupiterClient,
	}
	quoteHandlers := &httpapi.QuoteHandlers{
		Store:      store,
		Privy:      privyClient,
		Buy:        buy,
		Governance: governance,
	}
	proposalHandlers := &httpapi.ProposalHandlers{
		Store:      store,
		Privy:      privyClient,
		Governance: governance,
	}
	agentIntents := app.NewAgentIntentService(store, swap, symbols)
	agentHandlers := &httpapi.AgentHandlers{Intents: agentIntents}

	addr := "127.0.0.1:8080"
	if v := os.Getenv("API_ADDR"); v != "" {
		addr = v
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", httpapi.HealthHandler)
	mux.HandleFunc("POST /v1/auth/session", auth.SessionHandler)
	mux.HandleFunc("GET /v1/me", me.MeHandler)
	mux.HandleFunc("PATCH /v1/me", me.PatchMeHandler)
	mux.HandleFunc("POST /v1/me/profile-photo", me.UploadProfilePhotoHandler)
	mux.HandleFunc("GET /v1/me/balance", depositHandlers.GetPlatformBalanceHandler)
	mux.HandleFunc("POST /v1/me/withdrawals", platformWithdrawHandlers.CreatePlatformWithdrawalHandler)
	mux.HandleFunc("GET /v1/me/withdrawals/{id}", platformWithdrawHandlers.GetPlatformWithdrawalHandler)
	mux.HandleFunc("GET /v1/home", homeHandlers.HomeHandler)
	mux.HandleFunc("GET /v1/home/dashboard", homeHandlers.HomeDashboardHandler)
	mux.HandleFunc("GET /v1/home/pnl-series", homeHandlers.HomePnLSeriesHandler)
	mux.HandleFunc("GET /v1/home/missed-proposals", homeHandlers.HomeMissedProposalsHandler)
	mux.HandleFunc("GET /v1/users/{id}/groups", homeHandlers.UserSharedGroupsHandler)
	mux.HandleFunc("POST /v1/groups", groupHandlers.CreateGroupHandler)
	mux.HandleFunc("GET /v1/groups/search", groupsTabHandlers.SearchGroupsHandler)
	mux.HandleFunc("GET /v1/groups/leaderboard", groupsTabHandlers.GroupLeaderboardHandler)
	mux.HandleFunc("GET /v1/groups/pnl-history", groupsTabHandlers.MyGroupsPnLHistoryHandler)
	mux.HandleFunc("GET /v1/groups/{id}/pnl-history", groupsTabHandlers.GroupPnLHistoryHandler)
	mux.HandleFunc("POST /v1/groups/{id}/join", groupHandlers.JoinGroupHandler)
	mux.HandleFunc("POST /v1/groups/{id}/leave", groupHandlers.LeaveGroupHandler)
	mux.HandleFunc("POST /v1/groups/{id}/withdraw-to-balance", groupHandlers.WithdrawToBalanceHandler)
	mux.HandleFunc("GET /v1/groups/{id}/join-requests", groupHandlers.ListJoinRequestsHandler)
	mux.HandleFunc("POST /v1/groups/{id}/join-requests/{requestId}/approve", groupHandlers.ApproveJoinRequestHandler)
	mux.HandleFunc("POST /v1/groups/{id}/join-requests/{requestId}/deny", groupHandlers.DenyJoinRequestHandler)
	mux.HandleFunc("GET /v1/groups/{id}", groupHandlers.GetGroupHandler)
	mux.HandleFunc("GET /v1/groups/{id}/view", groupHandlers.GetGroupViewHandler)
	mux.HandleFunc("GET /v1/groups/{id}/activity", groupHandlers.ListGroupActivityHandler)
	mux.HandleFunc("POST /v1/groups/{id}/deposits", depositHandlers.CreateDepositHandler)
	mux.HandleFunc("POST /v1/groups/{id}/fund", depositHandlers.FundGroupHandler)
	mux.HandleFunc("GET /v1/groups/{id}/share-units", depositHandlers.GetMemberShareUnitsHandler)
	mux.HandleFunc("GET /v1/groups/{id}/treasury/usdc", depositHandlers.GetTreasuryUsdcBalanceHandler)
	mux.HandleFunc("GET /v1/deposits/{id}", depositHandlers.GetDepositHandler)
	mux.HandleFunc("GET /v1/transactions/{id}", transactionHandlers.GetTransactionHandler)
	mux.HandleFunc("POST /v1/transactions/{id}/retry", transactionHandlers.RetryTransactionHandler)
	mux.HandleFunc("GET /v1/groups/{id}/treasury/tokens", transactionHandlers.GetTreasuryTokenBalancesHandler)
	mux.HandleFunc("GET /v1/groups/{id}/cost-basis/{symbol}", transactionHandlers.GetCostBasisBySymbolHandler)
	mux.HandleFunc("GET /v1/groups/{id}/assets", catalogHandlers.SearchAssetsHandler)
	mux.HandleFunc("GET /v1/assets", assetsHandlers.ListAssetsHandler)
	mux.HandleFunc("GET /v1/assets/popular", assetsHandlers.PopularAssetsHandler)
	mux.HandleFunc("GET /v1/assets/{symbol}/chart", assetsHandlers.GetAssetChartHandler)
	mux.HandleFunc("GET /v1/assets/{symbol}", assetsHandlers.GetAssetHandler)
	mux.HandleFunc("POST /v1/groups/{id}/quotes", quoteHandlers.QuoteHandler)
	mux.HandleFunc("GET /v1/groups/{id}/proposals", proposalHandlers.ListGroupProposalsHandler)
	mux.HandleFunc("POST /v1/groups/{id}/proposals", proposalHandlers.CreateProposalHandler)
	mux.HandleFunc("GET /v1/proposals/{id}", proposalHandlers.GetProposalDetailHandler)
	mux.HandleFunc("POST /v1/proposals/{id}/votes", proposalHandlers.CastVoteHandler)
	mux.HandleFunc("POST /v1/groups/{id}/agents/intents", agentHandlers.SubmitAgentIntentHandler)
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
