package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/chainlink"
	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/faker"
	"github.com/monaco/monaco/apps/backend/internal/httpapi"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/signer"
	"github.com/monaco/monaco/apps/backend/internal/storage"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/apps/backend/internal/worker"
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
	slog.Info("relayer loaded", "address", relayer.Address())

	chain := evm.NewJSONRPCClient(cfg.BaseRPCURL)
	bal, err := chain.ETHBalance(ctx, relayer.Address())
	if err != nil {
		return nil, fmt.Errorf("relayer ETH balance: %w", err)
	}
	if bal.Cmp(evm.FeePayerMinWei) < 0 {
		return nil, fmt.Errorf("relayer ETH below minimum on Base")
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
	slog.Info("database connected")

	store := postgres.NewStore(db)
	signerHTTP := signer.NewHTTPClient(cfg.SignerURL, cfg.SignerSharedSecret)
	if _, err := signerHTTP.Health(ctx); err != nil {
		return nil, fmt.Errorf("signer health: %w", err)
	}
	sharesKey, err := parseSharesKey(cfg.WalletSharesKey)
	if err != nil {
		return nil, err
	}
	walletClient := wallets.NewSignerClient(signerHTTP, chain, wallets.AdaptPostgresStore(store), sharesKey, relayer.Address())
	authVerifier := auth.NewDynamicVerifier(cfg.DynamicEnvironmentID, http.DefaultClient)
	catalog := b20.NewPinnedCatalog()
	dexClient := dex.NewKyberClient(http.DefaultClient, cfg.KyberClientID)
	marksClient := chainlink.NewClient(chain, catalog, time.Now)
	var hermes *pyth.HermesClient
	if cfg.PythAPIKey != "" {
		hermes, err = pyth.NewHermesClientFromConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("pyth hermes: %w", err)
		}
		slog.Info("pyth hermes ready")
	} else {
		slog.Info("pyth hermes skipped", "reason", "PYTH_API_KEY unset")
	}
	symbols := app.NewSymbolResolver(catalog)
	deposits := app.NewDepositService(store, authVerifier, walletClient, marksClient, symbols)
	confirmer := worker.NewEVMConfirmer(chain)
	platformWithdrawals := app.NewPlatformWithdrawService(store, authVerifier, walletClient, deposits, confirmer)
	sessions := app.NewSessionService(store, authVerifier, walletClient).
		WithDisplayNameLimiter(app.NewDisplayNameUpdateLimiter())
	var storageClient storage.Client
	if cfg.SupabaseURL != "" && cfg.SupabaseServiceRoleKey != "" {
		storageClient = storage.NewSupabaseClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
		slog.Info("supabase storage ready")
	} else {
		slog.Info("supabase storage skipped", "reason", "SUPABASE_URL or SUPABASE_SERVICE_ROLE_KEY unset")
	}
	profilePhotos := app.NewProfilePhotoService(store, authVerifier, walletClient, storageClient).
		WithUploadLimiter(app.NewProfilePhotoUploadLimiter())
	home := app.NewHomeService(store, authVerifier, walletClient, marksClient, deposits, symbols)
	groups := app.NewGroupService(store, authVerifier, walletClient)
	governance := app.NewGovernanceService(store, authVerifier, walletClient)
	depositHandlers := &httpapi.DepositHandlers{Deposits: deposits}
	platformWithdrawHandlers := &httpapi.PlatformWithdrawHandlers{Withdrawals: platformWithdrawals}
	buy := app.NewBuyService(dexClient, catalog)
	swap := app.NewSwapService(store, buy, dexClient, walletClient, chain, symbols)
	redeem := app.NewRedeemService(store, walletClient, authVerifier, marksClient, dexClient, swap)
	governance.SetRedeemService(redeem)
	auth := &httpapi.AuthHandlers{Sessions: sessions, Verifier: authVerifier}
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
		Auth:    authVerifier,
		Wallets: walletClient,
		Catalog: catalog,
		Swap:    swap,
		Symbols: symbols,
	}
	agentKeyGuard := httpapi.NewAgentKeyGuard()
	catalogHandlers := &httpapi.CatalogHandlers{
		Store:    store,
		Auth:     authVerifier,
		Wallets:  walletClient,
		Catalog:  catalog,
		KeyGuard: agentKeyGuard,
	}
	var assetPrices pyth.AssetPriceClient = chainlink.NewAssetPrices(chain, catalog, time.Now)
	if hermes != nil {
		assetPrices = pyth.WithCharts(assetPrices, hermes)
	}
	assetsHandlers := &httpapi.AssetsHandlers{
		Store:   store,
		Auth:    authVerifier,
		Wallets: walletClient,
		Catalog: catalog,
		Pyth:    assetPrices,
		Dex:     dexClient,
	}
	go warmCatalogMarks(catalog, assetPrices)
	quoteHandlers := &httpapi.QuoteHandlers{
		Store:      store,
		Auth:       authVerifier,
		Wallets:    walletClient,
		Buy:        buy,
		Governance: governance,
	}
	proposalHandlers := &httpapi.ProposalHandlers{
		Store:      store,
		Auth:       authVerifier,
		Wallets:    walletClient,
		Governance: governance,
	}
	agentIntents := app.NewAgentIntentService(store, swap, symbols)
	agentHandlers := &httpapi.AgentHandlers{Intents: agentIntents, KeyGuard: agentKeyGuard}

	addr := "127.0.0.1:8080"
	if v := os.Getenv("API_ADDR"); v != "" {
		addr = v
	}

	groupChat := app.NewGroupChatService(store, authVerifier, walletClient)
	groupMessageHandlers := &httpapi.GroupMessageHandlers{Chat: groupChat}
	fakerHandlers := &httpapi.DevFakerHandlers{
		Enabled:     config.FakerEnabled(),
		DatabaseURL: cfg.DatabaseURL,
		Store:       store,
		Auth:        authVerifier,
		Wallets:     walletClient,
		Seeder:      faker.NewSeeder(store, faker.MarksSource(marksClient)),
	}
	if fakerHandlers.Enabled {
		if config.IsLocalDatabaseURL(cfg.DatabaseURL) {
			slog.Warn("faker seed endpoint enabled", "route", "POST /v1/dev/faker")
		} else {
			slog.Error("FAKER_ENABLED set but DATABASE_URL is not local; faker endpoint will refuse requests")
		}
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
	mux.HandleFunc("GET /v1/groups/{id}/messages", groupMessageHandlers.ListGroupMessagesHandler)
	mux.HandleFunc("POST /v1/groups/{id}/messages", groupMessageHandlers.PostGroupMessageHandler)
	mux.HandleFunc("GET /v1/proposals/{id}", proposalHandlers.GetProposalDetailHandler)
	mux.HandleFunc("POST /v1/proposals/{id}/votes", proposalHandlers.CastVoteHandler)
	mux.HandleFunc("POST /v1/groups/{id}/agents/intents", agentHandlers.SubmitAgentIntentHandler)
	mux.HandleFunc("GET /v1/proposals/{id}/comments", proposalHandlers.ListProposalCommentsHandler)
	mux.HandleFunc("POST /v1/proposals/{id}/comments", proposalHandlers.CreateProposalCommentHandler)
	routes := registerDevFakerRoute(mux, fakerHandlers, apiRoutes)
	logRoutesReady(routes)

	poller := worker.NewSweepPoller(store, walletClient, confirmer, deposits, "", nil)
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

// registerDevFakerRoute adds POST /v1/dev/faker only when FAKER_ENABLED is set (#153), so
// production muxes never expose the seed route. Returns the route list for startup logging.
func registerDevFakerRoute(mux *http.ServeMux, h *httpapi.DevFakerHandlers, routes []string) []string {
	if h == nil || !h.Enabled {
		return routes
	}
	mux.HandleFunc("POST /v1/dev/faker", h.FakerHandler)
	return append(append([]string(nil), routes...), "POST /v1/dev/faker")
}

func warmCatalogMarks(catalog b20.Catalog, prices pyth.AssetPriceClient) {
	if catalog == nil || prices == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	assets, err := catalog.Popular(ctx)
	if err != nil {
		slog.Warn("catalog mark warm skipped", "err", err.Error())
		return
	}
	symbols := make([]string, 0, len(assets))
	for _, asset := range assets {
		symbols = append(symbols, asset.Symbol)
	}
	marks, err := prices.AssetMarks(ctx, symbols)
	if err != nil {
		slog.Warn("catalog mark warm skipped", "err", err.Error())
		return
	}
	for _, symbol := range symbols {
		if mark, ok := marks[symbol]; !ok || mark.PriceUsdcMicros <= 0 {
			slog.Warn("catalog mark warm missed", "symbol", symbol)
		}
	}
}

func parseSharesKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "0x")
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 32 {
		return nil, fmt.Errorf("WALLET_SHARES_KEY must be 32-byte hex")
	}
	return b, nil
}
