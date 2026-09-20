package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/faker"
	"github.com/monaco/monaco/apps/backend/internal/flash"
	"github.com/monaco/monaco/apps/backend/internal/httpapi"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pricechain"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/solana/balance"
	"github.com/monaco/monaco/apps/backend/internal/storage"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
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
	stopRedeemPoller  context.CancelFunc
	// workers tracks the poller goroutines so shutdown can wait for an in-flight tick.
	workers *sync.WaitGroup
}

var apiRoutes = []string{
	"GET /health",
	"GET /metrics",
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
	"GET /v1/groups/{id}/messages",
	"POST /v1/groups/{id}/messages",
	"GET /v1/proposals/{id}",
	"POST /v1/proposals/{id}/votes",
	"POST /v1/groups/{id}/agents/intents",
	"GET /v1/proposals/{id}/comments",
	"POST /v1/proposals/{id}/comments",
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
	var hermes *pyth.HermesClient
	if cfg.PythAPIKey != "" {
		hermes = pyth.NewHermesClientWithBaseURL(cfg.PythHermesBaseURL, cfg.PythAPIKey)
		slog.Info("pyth client ready")
	} else {
		slog.Info("pyth client skipped", "reason", "PYTH_API_KEY unset")
	}
	jupiterPriceClient := jupiter.NewHTTPPriceClient(cfg.JupiterAPIKey)
	if cfg.JupiterAPIKey != "" {
		slog.Info("jupiter price client ready")
	} else {
		slog.Info("jupiter price client ready", "reason", "JUPITER_API_KEY unset, using unauthenticated rate limit")
	}
	// Pot valuation and charts price through one chain: Pyth, then Jupiter, then cost basis.
	var pythSource pricechain.PythSource
	var pythCharts pyth.AssetPriceClient
	if hermes != nil {
		pythSource, pythCharts = hermes, hermes
	}
	priceChain := pricechain.New(pythSource, jupiterPriceClient, pythCharts, pricechain.DefaultConfig())
	var pythClient pyth.Client = priceChain
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
	sweepWake := worker.NewPollerWake()
	depositHandlers := &httpapi.DepositHandlers{
		Deposits:        deposits,
		NotifySweepPoll: sweepWake.Notify,
	}
	platformWithdrawHandlers := &httpapi.PlatformWithdrawHandlers{Withdrawals: platformWithdrawals}
	xstocksResolver := xstocks.NewHTTPResolver()
	buy := app.NewBuyService(jupiterClient, xstocksResolver)
	signer := app.NewPrivyTreasurySigner(privyClient)
	swap := app.NewSwapService(store, buy, jupiterClient, privyClient, signer, relayer.PrivateKey(), symbols)
	if cfg.SwapProvider == swapprovider.NameFlash {
		swap.SetSwapProvider(flash.NewSwapProvider(
			flash.NewHTTPClient(cfg.FlashAPIKey),
			signer,
			app.NewPrivyFlashSetupSubmitter(privyClient, relayer.PrivateKey()),
			flash.ProviderConfig{MaxSlippage: cfg.FlashMaxSlippage, SponsorAddress: relayer.PublicKey()},
		))
	}
	slog.Info("swap provider ready", "provider", swap.SwapProviderName())
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
	agentKeyGuard := httpapi.NewAgentKeyGuard(trustProxyHeaders())
	catalogHandlers := &httpapi.CatalogHandlers{
		Store:    store,
		Privy:    privyClient,
		Catalog:  catalogSearcher,
		KeyGuard: agentKeyGuard,
	}
	assetsHandlers := &httpapi.AssetsHandlers{
		Store:   store,
		Privy:   privyClient,
		Catalog: catalogSearcher,
		Pyth:    priceChain,
		Jupiter: jupiterClient,
		Price:   jupiterPriceClient,
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
	agentHandlers := &httpapi.AgentHandlers{Intents: agentIntents, KeyGuard: agentKeyGuard}

	addr := "127.0.0.1:8080"
	if v := os.Getenv("API_ADDR"); v != "" {
		addr = v
	}

	groupChat := app.NewGroupChatService(store, privyClient)
	groupMessageHandlers := &httpapi.GroupMessageHandlers{Chat: groupChat}
	fakerHandlers := &httpapi.DevFakerHandlers{
		Enabled:     config.FakerEnabled(),
		DatabaseURL: cfg.DatabaseURL,
		Store:       store,
		Privy:       privyClient,
		Seeder:      faker.NewSeeder(store, faker.PythMarkSource(pythClient)),
	}
	if fakerHandlers.Enabled {
		if config.IsLocalDatabaseURL(cfg.DatabaseURL) {
			slog.Warn("faker seed endpoint enabled", "route", "POST /v1/dev/faker")
		} else {
			slog.Error("FAKER_ENABLED set but DATABASE_URL is not local; faker endpoint will refuse requests")
		}
	}

	mux := http.NewServeMux()
	health := &httpapi.HealthHandlers{Checks: healthChecks(db, solanaRPC, relayer.PublicKey(), jupiterPriceClient)}
	mux.HandleFunc("GET /health", health.HealthHandler)
	mux.Handle("GET /metrics", metricsHandler())
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
	mux.HandleFunc("GET /v1/groups/{id}/messages", groupMessageHandlers.ListGroupMessagesHandler)
	mux.HandleFunc("POST /v1/groups/{id}/messages", groupMessageHandlers.PostGroupMessageHandler)
	mux.HandleFunc("GET /v1/proposals/{id}", proposalHandlers.GetProposalDetailHandler)
	mux.HandleFunc("POST /v1/proposals/{id}/votes", proposalHandlers.CastVoteHandler)
	mux.HandleFunc("POST /v1/groups/{id}/agents/intents", agentHandlers.SubmitAgentIntentHandler)
	mux.HandleFunc("GET /v1/proposals/{id}/comments", proposalHandlers.ListProposalCommentsHandler)
	mux.HandleFunc("POST /v1/proposals/{id}/comments", proposalHandlers.CreateProposalCommentHandler)
	routes := registerDevFakerRoute(mux, fakerHandlers, apiRoutes)
	logRoutesReady(routes)

	poller := worker.NewSweepPoller(store, privyClient, solanaRPC, deposits, relayer.PrivateKey(), nil)
	pollerCtx, stopPoller := context.WithCancel(context.Background())
	workers := &sync.WaitGroup{}
	workers.Add(3)
	go func() {
		defer workers.Done()
		worker.Run(pollerCtx, poller, worker.DefaultPollInterval, sweepWake)
	}()
	slog.Info("sweep poller started")

	executePoller := worker.NewProposalExecutePoller(store, executeOnPass, nil)
	executeCtx, stopExecutePoller := context.WithCancel(context.Background())
	go func() {
		defer workers.Done()
		worker.RunProposalExecutePoller(executeCtx, executePoller, worker.DefaultProposalExecuteInterval)
	}()
	slog.Info("proposal execute poller started")

	redeemPoller := worker.NewRedeemRecoveryPoller(store, redeem, nil)
	redeemCtx, stopRedeemPoller := context.WithCancel(context.Background())
	go func() {
		defer workers.Done()
		worker.RunRedeemRecoveryPoller(redeemCtx, redeemPoller, worker.DefaultRedeemRecoveryInterval)
	}()

	return &bootResult{
		Server:            newHTTPServer(addr, platformHandler(mux)),
		Config:            cfg,
		Relayer:           relayer,
		DB:                db,
		stopPoller:        stopPoller,
		stopExecutePoller: stopExecutePoller,
		stopRedeemPoller:  stopRedeemPoller,
		workers:           workers,
	}, nil
}

func main() {
	closeLog, err := setupLogging()
	if err != nil {
		fmt.Fprintln(os.Stderr, "logging setup failed:", err)
		os.Exit(1)
	}
	defer closeLog()

	flushTelemetry, err := setupTelemetry()
	if err != nil {
		slog.Error("telemetry setup failed", "err", err)
		closeLog()
		os.Exit(1)
	}
	defer flushTelemetry()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := boot(ctx)
	if err != nil {
		slog.Error("boot failed", "err", err)
		flushTelemetry()
		closeLog()
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
			flushTelemetry()
			closeLog()
			os.Exit(1)
		}
	case sig := <-stop:
		slog.Info("shutdown signal received", "signal", sig.String())
	}

	// Drain HTTP first: in-flight cash outs and trades finish against live pollers and a
	// live database. Only then stop the pollers and wait for them before closing the pool.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer shutdownCancel()

	if err := result.Server.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown failed", "err", err)
	}

	result.stopPoller()
	result.stopExecutePoller()
	result.stopRedeemPoller()
	if waitWorkers(result.workers, workerStopTimeout) {
		slog.Info("pollers stopped")
	} else {
		slog.Error("pollers did not stop in time", "timeout", workerStopTimeout)
	}
	if err := result.DB.Close(); err != nil {
		slog.Warn("database close failed", "err", err)
	}
	slog.Info("shutdown complete")
}

// registerDevFakerRoute adds POST /v1/dev/faker only when FAKER_ENABLED is set (#153), so
// production muxes never expose the seed route. Returns the route list for startup logging.
func registerDevFakerRoute(mux *http.ServeMux, h *httpapi.DevFakerHandlers, routes []string) []string {
	if h == nil || !h.Enabled {
		return routes
	}
	mux.HandleFunc("POST /v1/dev/faker", h.FakerHandler)
	return append(append([]string(nil), routes...), "POST /v1/dev/faker")
}
