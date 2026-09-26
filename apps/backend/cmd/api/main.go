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
	"github.com/monaco/monaco/apps/backend/internal/catalog"
	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/demochain"
	"github.com/monaco/monaco/apps/backend/internal/faker"
	"github.com/monaco/monaco/apps/backend/internal/flash"
	"github.com/monaco/monaco/apps/backend/internal/httpapi"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/jupitercharts"
	"github.com/monaco/monaco/apps/backend/internal/news"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/prestocks"
	"github.com/monaco/monaco/apps/backend/internal/pricechain"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/solana/balance"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/storage"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
	"github.com/monaco/monaco/apps/backend/internal/tessera"
	"github.com/monaco/monaco/apps/backend/internal/worker"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/apps/backend/internal/yahoocharts"
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
	stopSparkWarmer   context.CancelFunc
	// lane: notifications
	stopNotificationPoller context.CancelFunc
	// lane: watchlist
	stopAlertPoller context.CancelFunc
	// lane: matchups
	stopMatchupPoller context.CancelFunc
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
	// lane: settings
	"GET /v1/me/preferences",
	"PATCH /v1/me/preferences",
	"GET /v1/me/deletion-check",
	"DELETE /v1/me",
	"GET /v1/me/balance",
	"POST /v1/me/withdrawals",
	"GET /v1/me/withdrawals/{id}",
	// lane: portfolio
	"GET /v1/me/portfolio",
	"GET /v1/me/transactions",
	"GET /v1/me/transactions/export.csv",
	"GET /v1/home",
	"GET /v1/home/dashboard",
	"GET /v1/home/pnl-series",
	"GET /v1/home/missed-proposals",
	"GET /v1/users/{id}/groups",
	"POST /v1/groups",
	"GET /v1/groups/search",
	"GET /v1/groups/leaderboard",
	"GET /v1/groups/pnl-history",
	"GET /v1/groups/{id}/pnl-history",
	"POST /v1/groups/{id}/join",
	"POST /v1/groups/{id}/leave",
	"POST /v1/groups/{id}/withdraw-to-balance",
	"GET /v1/groups/{id}/join-requests",
	"POST /v1/groups/{id}/join-requests/{requestId}/approve",
	"POST /v1/groups/{id}/join-requests/{requestId}/deny",
	"GET /v1/groups/{id}",
	"GET /v1/groups/{id}/view",
	"POST /v1/groups/{id}/picture",
	"DELETE /v1/groups/{id}/picture",
	"GET /v1/groups/{id}/activity",
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
	"GET /v1/assets/held",
	"GET /v1/assets/popular",
	"GET /v1/assets/{symbol}/chart",
	"GET /v1/assets/{symbol}/social",
	// lane: news
	"GET /v1/assets/{symbol}/news",
	"GET /v1/news/market",
	"GET /v1/assets/{symbol}",
	"POST /v1/groups/{id}/quotes",
	"GET /v1/groups/{id}/proposals",
	"POST /v1/groups/{id}/proposals",
	"GET /v1/groups/{id}/messages",
	"POST /v1/groups/{id}/messages",
	"GET /v1/proposals/{id}",
	"POST /v1/proposals/{id}/votes",
	"POST /v1/groups/{id}/agents/intents",
	"GET /v1/agent",
	"GET /v1/agent/assets",
	"POST /v1/agent/intents",
	"GET /v1/agent/intents/{intentId}",
	"GET /v1/agent/skill.md",
	"GET /v1/proposals/{id}/comments",
	"POST /v1/proposals/{id}/comments",
	// lane: invites
	"GET /v1/groups/{id}/invites",
	"POST /v1/groups/{id}/invites",
	"POST /v1/groups/{id}/invites/revoke",
	"GET /v1/invites/{code}",
	"POST /v1/groups/join-by-code",
	// lane: notifications
	"GET /v1/me/notifications",
	"POST /v1/me/notifications/read",
	"PUT /v1/me/devices",
	"DELETE /v1/me/devices/{token}",
	"POST /v1/proposals/{id}/nudge",
	// lane: watchlist
	"GET /v1/me/watchlist",
	"PUT /v1/me/watchlist",
	"PUT /v1/me/watchlist/{symbol}",
	"DELETE /v1/me/watchlist/{symbol}",
	"GET /v1/me/alerts",
	"POST /v1/me/alerts",
	"DELETE /v1/me/alerts/{id}",
	// lane: matchups
	"GET /v1/groups/{id}/matchup",
	"GET /v1/home/matchups",
	"GET /v1/matchups/table",
	"POST /v1/groups/{id}/matchups/challenge",
	"POST /v1/groups/{id}/matchups/challenges/{challengeId}/accept",
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

	var solanaRPC interface {
		worker.SolanaRPC
		app.SolanaConfirmer
	}
	httpRPC := worker.NewHTTPSolanaRPC(cfg.SolanaRPCEndpoint())
	if cfg.DemoMode {
		// DEMO_MODE: nothing touches Solana, so the fee payer's balance is irrelevant.
		solanaRPC = demochain.NewRPC()
		slog.Warn("DEMO MODE: fake money. Balances, fills and cash-outs live in memory and reset on restart; nothing reaches Solana.")
	} else {
		if err := balance.MustHaveSOL(ctx, httpRPC, relayer.PublicKey(), balance.FeePayerMinLamports); err != nil {
			return nil, err
		}
		slog.Info("relayer SOL balance ok", "pubkey", relayer.PublicKey(), "min_lamports", balance.FeePayerMinLamports)
		solanaRPC = httpRPC
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
	cfg.DBPool.Apply(db)
	slog.Info("database connected")

	store := postgres.NewStore(db)
	if err := registerDatabaseMetrics(db, store); err != nil {
		_ = db.Close()
		return nil, err
	}
	httpPrivy := privy.NewHTTPClient(cfg)
	var privyClient privy.Client = httpPrivy
	var sweepClient privy.SweepClient = httpPrivy
	var demoChain *demochain.Client
	if cfg.DemoMode {
		demoChain = demochain.NewClient(httpPrivy, demochain.StartingBalance)
		privyClient = demoChain
		sweepClient = demoChain
	}
	xstocksResolver := xstocks.NewHTTPResolver()
	// Chart history comes from Jupiter's candles for the xStock itself, in one call
	// per range, and Jupiter needs no key.
	//
	// It used to come from Pyth Benchmarks. Every Pyth history source prices the
	// *underlying equity*, and reaching an equity feed needs an entitlement our key
	// does not carry: Hermes refuses `Equity.US.AAPL/USD` and `Crypto.AAPLX/USD`
	// alike with a 403, and the public Benchmarks TradingView shim now 404s outright.
	// Charts were empty whichever of them answered, and no retry was going to change
	// that. Jupiter has no such gate and prices the thing a cabal can actually buy.
	//
	// Benchmarks stays reachable for an operator who sets PYTH_BENCHMARKS_BASE_URL at
	// a deployment that does serve equity history; it is no longer wired to the public
	// host by default, because that host has nothing for us.
	// The free equity curve first (CHART_SOURCE=yahoo, the default for now), the
	// token's own candles behind it; CHART_SOURCE=jupiter draws the token alone.
	seriesSources := []pyth.SeriesSource{jupitercharts.NewChartsClient(xstocksResolver, cfg.JupiterAPIKey)}
	if cfg.ChartSource == "yahoo" {
		seriesSources = append([]pyth.SeriesSource{yahoocharts.New()}, seriesSources...)
		slog.Info("chart history from yahoo finance (underlying equity); jupiter candles as fallback")
	}
	if cfg.PythBenchmarksBaseURL != "" {
		seriesSources = append(seriesSources, pyth.NewBenchmarksClientWithHTTP(cfg.PythBenchmarksBaseURL, nil))
		slog.Info("pyth benchmarks wired as chart fallback", "base_url", cfg.PythBenchmarksBaseURL)
	}
	// Built unconditionally. Building it inside a `PythAPIKey != ""` branch made
	// keyless history depend on exactly the entitlement it exists not to need; the
	// Hermes per-sample path stays behind a breaker as the last resort, and it is
	// that path, not this one, that needs the key.
	chartClient := pyth.NewHermesClientWithBaseURL(cfg.PythHermesBaseURL, cfg.PythAPIKey).
		WithSeriesSource(pyth.NewFallbackSeriesSource(seriesSources...))
	// Latest marks and the stock-vs-token feeds do need a key; without one they are
	// left unwired rather than wired to something that would 401 on every call.
	var hermes *pyth.HermesClient
	if cfg.PythAPIKey != "" {
		hermes = chartClient
		slog.Info("pyth client ready")
	} else {
		slog.Info("pyth marks skipped", "reason", "PYTH_API_KEY unset; Jupiter chart history stays active")
	}
	jupiterPriceClient := jupiter.NewHTTPPriceClient(cfg.JupiterAPIKey)
	if cfg.JupiterAPIKey != "" {
		slog.Info("jupiter price client ready")
	} else {
		slog.Info("jupiter price client ready", "reason", "JUPITER_API_KEY unset, using unauthenticated rate limit")
	}
	// Pot valuation and charts price through one chain: Pyth, then Jupiter, then cost basis.
	// The mark source is nil without a key; the chart client is passed either way.
	var pythSource pricechain.PythSource
	if hermes != nil {
		pythSource = hermes
	}
	priceChain := pricechain.New(pythSource, jupiterPriceClient, chartClient, pricechain.DefaultConfig())
	var pythClient pyth.Client = priceChain
	catalogSearcher := xstocks.NewHTTPCatalogSearcher()
	jupiterClient := jupiter.NewHTTPClientWithPayer(relayer.PublicKey())
	mintReader := mintinfo.NewHTTPReader(cfg.SolanaRPCEndpoint())
	var catalogSources []catalog.TaggedSource
	if cfg.TesseraEnabled {
		catalogSources = append(catalogSources, catalog.TaggedSource{
			Source:   tessera.NewHTTPCatalogWithClient(cfg.TesseraAPIBaseURL, nil),
			SourceID: xstocks.AssetSourceTessera,
		})
	}
	if cfg.PreStocksEnabled {
		catalogSources = append(catalogSources, catalog.TaggedSource{
			Source:   prestocks.NewHTTPCatalogWithClient(cfg.PreStocksAPIBaseURL, nil),
			SourceID: xstocks.AssetSourcePreStocks,
		})
	}
	// Nil routability prober: lists must not Jupiter-quote every symbol.
	catalogComposite := catalog.NewCompositeWithSources(catalogSearcher, catalogSources, nil, mintReader, jupiterPriceClient)
	slog.Info("catalog sources ready", "xstocks", true, "tessera", cfg.TesseraEnabled, "prestocks", cfg.PreStocksEnabled)
	symbols := app.NewSymbolResolver(catalogComposite)
	symbols.SetMintInfo(mintReader)
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
	groupPictures := app.NewGroupPictureService(store, privyClient, storageClient).
		WithWriteLimiter(app.NewGroupPictureWriteLimiter())
	home := app.NewHomeService(store, privyClient, pythClient, deposits, symbols)
	groups := app.NewGroupService(store, privyClient)
	governance := app.NewGovernanceService(store, privyClient)
	sweepWake := worker.NewPollerWake()
	depositHandlers := &httpapi.DepositHandlers{
		Deposits:        deposits,
		NotifySweepPoll: sweepWake.Notify,
	}
	platformWithdrawHandlers := &httpapi.PlatformWithdrawHandlers{Withdrawals: platformWithdrawals}
	mintResolver := catalog.NewResolverWithCatalog(xstocksResolver, catalogComposite)
	buy := app.NewBuyService(jupiterClient, mintResolver)
	buy.SetMintCatalog(catalogComposite)
	buy.SetBuyVariantPicker(catalogComposite)
	buy.SetMintInfo(mintReader)
	signer := app.NewPrivyTreasurySigner(httpPrivy)
	swap := app.NewSwapService(store, buy, jupiterClient, privyClient, signer, relayer.PrivateKey(), symbols)
	swap.SetPriceClient(pythClient)
	swap.SetMintInfo(mintReader)
	if cfg.SwapProvider == swapprovider.NameFlash {
		swap.SetSwapProvider(flash.NewSwapProvider(
			flash.NewHTTPClient(cfg.FlashAPIKey),
			signer,
			app.NewPrivyFlashSetupSubmitter(httpPrivy, relayer.PrivateKey()),
			flash.ProviderConfig{MaxSlippage: cfg.FlashMaxSlippage, SponsorAddress: relayer.PublicKey()},
		))
	}
	if demoChain != nil {
		swap.SetSwapProvider(demochain.NewSwapProvider(jupiterPriceClient, demoChain.Chain()))
		slog.Warn("DEMO MODE: swaps fill at Jupiter's live price on the in-memory ledger")
	}
	slog.Info("swap provider ready", "provider", swap.SwapProviderName())
	redeem := app.NewRedeemService(store, privyClient, pythClient, jupiterClient, swap, signer)
	governance.SetRedeemService(redeem)
	auth := &httpapi.AuthHandlers{Sessions: sessions}
	me := &httpapi.MeHandlers{Sessions: sessions, ProfilePhoto: profilePhotos}
	homeHandlers := &httpapi.HomeHandlers{Home: home}
	// lane: portfolio
	portfolioHandlers := &httpapi.PortfolioHandlers{Portfolio: app.NewPortfolioService(home)}
	// lane: settings
	accountHandlers := &httpapi.AccountHandlers{Accounts: app.NewAccountService(store, privyClient, home).
		WithPreferencesLimiter(app.NewPreferencesUpdateLimiter()).
		WithDeleteLimiter(app.NewAccountDeleteLimiter())}
	assetSocialHandlers := &httpapi.AssetSocialHandlers{Home: home}
	groupHandlers := &httpapi.GroupHandlers{
		Groups:     groups,
		Governance: governance,
		Home:       home,
		Redeem:     redeem,
		// Pot rows carry the catalog's kind, decimals and issuer, and the token's premium.
		Catalog: catalogComposite,
		Price:   jupiterPriceClient,
		// Holdings rows read the market the same way the Stocks tab does.
		Market: &httpapi.MarketRowSource{Catalog: catalogComposite, Pyth: priceChain, Price: jupiterPriceClient},
	}
	groupsTabHandlers := &httpapi.GroupsTabHandlers{GroupsTab: app.NewGroupsTabService(home, store)}
	// lane: invites
	inviteHandlers := &httpapi.InviteHandlers{Invites: app.NewInviteService(store, privyClient, governance, groupsTabHandlers.GroupsTab)}
	// lane: matchups
	matchups := app.NewMatchupService(store, home)
	matchupHandlers := &httpapi.MatchupHandlers{Matchups: matchups}
	groupPictureHandlers := &httpapi.GroupPictureHandlers{Pictures: groupPictures}
	executeOnPass := app.NewExecuteOnPassService(swap, store)
	governance.SetBuyService(buy)
	governance.SetHomeService(home)
	governance.SetSwapService(swap)
	transactionHandlers := &httpapi.TransactionHandlers{
		Store:   store,
		Privy:   privyClient,
		XStocks: mintResolver,
		Swap:    swap,
		Symbols: symbols,
		Catalog: catalogComposite,
	}
	agentKeyGuard := httpapi.NewAgentKeyGuard(trustProxyHeaders())
	catalogHandlers := &httpapi.CatalogHandlers{
		Store:    store,
		Privy:    privyClient,
		Catalog:  catalogComposite,
		KeyGuard: agentKeyGuard,
	}
	assetsHandlers := &httpapi.AssetsHandlers{
		Store:   store,
		Privy:   privyClient,
		Catalog: catalogComposite,
		Pyth:    priceChain,
		Jupiter: jupiterClient,
		Price:   jupiterPriceClient,
		Home:    home,
	}
	if hermes != nil {
		// The stock-vs-token card reads the raw feeds, not the valuation chain: its
		// whole point is to show where the two prices disagree.
		var quotes pyth.ReferenceQuoteClient = hermes
		if cfg.ChartSource == "yahoo" {
			// Our key is not entitled to the equity feeds, so the equity leg is read
			// off Yahoo's day chart of the same stock instead, labelled as Yahoo's.
			// Through the price chain, so it is the series the screen already cached.
			quotes = pyth.WithEquityQuoteFallback(hermes, priceChain)
		}
		assetsHandlers.Quotes = quotes
	}
	// lane: news
	// Headlines from keyless RSS (Yahoo Finance per ticker, Google News by name),
	// cached per company for ten minutes; a feed that fails serves its last list.
	newsHandlers := &httpapi.NewsHandlers{Assets: assetsHandlers, News: news.NewService(news.NewClient(nil))}
	// lane: watchlist
	// Alerts are priced the way the Stocks tab prices a row: catalogue mint, Jupiter mark.
	alertMarks := &app.CatalogMarkSource{Catalog: catalogComposite, Price: jupiterPriceClient}
	watchlist := app.NewWatchlistService(store, privyClient, catalogComposite, alertMarks)
	assetsHandlers.Watchlist = watchlist
	watchlistHandlers := &httpapi.WatchlistHandlers{Watchlist: watchlist, Assets: assetsHandlers}
	quoteHandlers := &httpapi.QuoteHandlers{
		Store:      store,
		Privy:      privyClient,
		Buy:        buy,
		Governance: governance,
		// A buy quote reports the pre-IPO premium against a fresh reference.
		Price: jupiterPriceClient,
	}
	proposalHandlers := &httpapi.ProposalHandlers{
		Store:      store,
		Privy:      privyClient,
		Governance: governance,
		// Proposals name their asset's kind and decimals from the catalog.
		Catalog: catalogComposite,
	}
	agentDocs, err := app.NewAgentDocs(cfg.PublicAPIBaseURL)
	if err != nil {
		return nil, err
	}
	groupHandlers.AgentDocs = agentDocs
	agentIntents := app.NewAgentIntentService(store, swap, symbols).WithMarketData(catalogSearcher, priceChain)
	agentHandlers := &httpapi.AgentHandlers{
		Store:    store,
		Intents:  agentIntents,
		KeyGuard: agentKeyGuard,
		Limits:   httpapi.NewAgentRateLimits(time.Now),
		Docs:     agentDocs,
	}

	addr := "127.0.0.1:8080"
	if v := os.Getenv("API_ADDR"); v != "" {
		addr = v
	}

	groupChat := app.NewGroupChatService(store, privyClient)
	groupMessageHandlers := &httpapi.GroupMessageHandlers{Chat: groupChat}

	// lane: notifications
	notifier := app.NewNotifier(store, pushSender(cfg.APNS, store))
	governance.SetNotifier(notifier)
	deposits.SetNotifier(notifier)
	redeem.SetNotifier(notifier)
	groupChat.SetNotifier(notifier)
	executeOnPass.SetNotifier(notifier)
	agentIntents.SetNotifier(notifier)
	notificationHandlers := &httpapi.NotificationHandlers{Inbox: app.NewNotificationService(store, privyClient), Governance: governance}
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
	health := &httpapi.HealthHandlers{Checks: healthChecks(db, httpRPC, relayer.PublicKey(), jupiterPriceClient, httpPrivy)}
	mux.HandleFunc("GET /health", health.HealthHandler)
	mux.Handle("GET /metrics", metricsHandler())
	mux.HandleFunc("POST /v1/auth/session", auth.SessionHandler)
	mux.HandleFunc("GET /v1/me", me.MeHandler)
	mux.HandleFunc("PATCH /v1/me", me.PatchMeHandler)
	mux.HandleFunc("POST /v1/me/profile-photo", me.UploadProfilePhotoHandler)
	// lane: settings
	mux.HandleFunc("GET /v1/me/preferences", accountHandlers.GetPreferencesHandler)
	mux.HandleFunc("PATCH /v1/me/preferences", accountHandlers.PatchPreferencesHandler)
	mux.HandleFunc("GET /v1/me/deletion-check", accountHandlers.DeletionCheckHandler)
	mux.HandleFunc("DELETE /v1/me", accountHandlers.DeleteMeHandler)
	mux.HandleFunc("GET /v1/me/balance", depositHandlers.GetPlatformBalanceHandler)
	mux.HandleFunc("POST /v1/me/withdrawals", platformWithdrawHandlers.CreatePlatformWithdrawalHandler)
	mux.HandleFunc("GET /v1/me/withdrawals/{id}", platformWithdrawHandlers.GetPlatformWithdrawalHandler)
	// lane: portfolio
	mux.HandleFunc("GET /v1/me/portfolio", portfolioHandlers.GetPortfolioHandler)
	mux.HandleFunc("GET /v1/me/transactions", portfolioHandlers.ListHistoryHandler)
	mux.HandleFunc("GET /v1/me/transactions/export.csv", portfolioHandlers.ExportHistoryCSVHandler)
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
	mux.HandleFunc("POST /v1/groups/{id}/picture", groupPictureHandlers.UploadGroupPictureHandler)
	mux.HandleFunc("DELETE /v1/groups/{id}/picture", groupPictureHandlers.RemoveGroupPictureHandler)
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
	mux.HandleFunc("GET /v1/assets/held", assetsHandlers.HeldAssetsHandler)
	mux.HandleFunc("GET /v1/assets/popular", assetsHandlers.PopularAssetsHandler)
	mux.HandleFunc("GET /v1/assets/{symbol}/chart", assetsHandlers.GetAssetChartHandler)
	mux.HandleFunc("GET /v1/assets/{symbol}/social", assetSocialHandlers.GetAssetSocialHandler)
	// lane: news
	mux.HandleFunc("GET /v1/assets/{symbol}/news", newsHandlers.GetAssetNewsHandler)
	mux.HandleFunc("GET /v1/news/market", newsHandlers.GetMarketNewsHandler)
	mux.HandleFunc("GET /v1/assets/{symbol}", assetsHandlers.GetAssetHandler)
	mux.HandleFunc("POST /v1/groups/{id}/quotes", quoteHandlers.QuoteHandler)
	mux.HandleFunc("GET /v1/groups/{id}/proposals", proposalHandlers.ListGroupProposalsHandler)
	mux.HandleFunc("POST /v1/groups/{id}/proposals", proposalHandlers.CreateProposalHandler)
	mux.HandleFunc("GET /v1/groups/{id}/messages", groupMessageHandlers.ListGroupMessagesHandler)
	mux.HandleFunc("POST /v1/groups/{id}/messages", groupMessageHandlers.PostGroupMessageHandler)
	mux.HandleFunc("GET /v1/proposals/{id}", proposalHandlers.GetProposalDetailHandler)
	mux.HandleFunc("POST /v1/proposals/{id}/votes", proposalHandlers.CastVoteHandler)
	mux.HandleFunc("POST /v1/groups/{id}/agents/intents", agentHandlers.SubmitAgentIntentHandler)
	mux.HandleFunc("GET /v1/agent", agentHandlers.AgentAccountHandler)
	mux.HandleFunc("GET /v1/agent/assets", agentHandlers.AgentAssetsHandler)
	mux.HandleFunc("POST /v1/agent/intents", agentHandlers.SubmitKeyAgentIntentHandler)
	mux.HandleFunc("GET /v1/agent/intents/{intentId}", agentHandlers.GetAgentIntentHandler)
	mux.HandleFunc("GET /v1/agent/skill.md", agentHandlers.AgentSkillHandler)
	mux.HandleFunc("GET /v1/proposals/{id}/comments", proposalHandlers.ListProposalCommentsHandler)
	mux.HandleFunc("POST /v1/proposals/{id}/comments", proposalHandlers.CreateProposalCommentHandler)
	// lane: invites
	mux.HandleFunc("GET /v1/groups/{id}/invites", inviteHandlers.GetGroupInviteHandler)
	mux.HandleFunc("POST /v1/groups/{id}/invites", inviteHandlers.CreateGroupInviteHandler)
	mux.HandleFunc("POST /v1/groups/{id}/invites/revoke", inviteHandlers.RevokeGroupInviteHandler)
	mux.HandleFunc("GET /v1/invites/{code}", inviteHandlers.GetInvitePreviewHandler)
	mux.HandleFunc("POST /v1/groups/join-by-code", inviteHandlers.JoinByCodeHandler)
	// lane: notifications
	mux.HandleFunc("GET /v1/me/notifications", notificationHandlers.ListNotificationsHandler)
	mux.HandleFunc("POST /v1/me/notifications/read", notificationHandlers.MarkNotificationsReadHandler)
	mux.HandleFunc("PUT /v1/me/devices", notificationHandlers.RegisterDeviceHandler)
	mux.HandleFunc("DELETE /v1/me/devices/{token}", notificationHandlers.UnregisterDeviceHandler)
	mux.HandleFunc("POST /v1/proposals/{id}/nudge", notificationHandlers.NudgeProposalHandler)
	// lane: watchlist
	mux.HandleFunc("GET /v1/me/watchlist", watchlistHandlers.GetWatchlistHandler)
	mux.HandleFunc("PUT /v1/me/watchlist", watchlistHandlers.ReorderWatchlistHandler)
	mux.HandleFunc("PUT /v1/me/watchlist/{symbol}", watchlistHandlers.AddToWatchlistHandler)
	mux.HandleFunc("DELETE /v1/me/watchlist/{symbol}", watchlistHandlers.RemoveFromWatchlistHandler)
	mux.HandleFunc("GET /v1/me/alerts", watchlistHandlers.ListPriceAlertsHandler)
	mux.HandleFunc("POST /v1/me/alerts", watchlistHandlers.CreatePriceAlertHandler)
	mux.HandleFunc("DELETE /v1/me/alerts/{id}", watchlistHandlers.DeletePriceAlertHandler)
	// lane: matchups
	mux.HandleFunc("GET /v1/groups/{id}/matchup", matchupHandlers.GroupMatchupHandler)
	mux.HandleFunc("GET /v1/home/matchups", matchupHandlers.HomeMatchupsHandler)
	mux.HandleFunc("GET /v1/matchups/table", matchupHandlers.MatchupTableHandler)
	mux.HandleFunc("POST /v1/groups/{id}/matchups/challenge", matchupHandlers.CreateMatchupChallengeHandler)
	mux.HandleFunc("POST /v1/groups/{id}/matchups/challenges/{challengeId}/accept", matchupHandlers.AcceptMatchupChallengeHandler)
	routes := registerDevFakerRoute(mux, fakerHandlers, apiRoutes)
	logRoutesReady(routes)

	poller := worker.NewSweepPoller(store, sweepClient, solanaRPC, deposits, relayer.PrivateKey(), nil)
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

	// lane: notifications
	notificationPoller := worker.NewNotificationPoller(store, governance, notifier, privyClient, nil)
	notifyCtx, stopNotificationPoller := context.WithCancel(context.Background())
	workers.Add(1)
	go func() {
		defer workers.Done()
		worker.RunNotificationPoller(notifyCtx, notificationPoller, worker.DefaultNotificationInterval)
	}()

	// Keeps the popular symbols' day series hot, so the Stocks list serves every
	// row's sparkline from the chart cache instead of waiting on Hermes.
	sparkCtx, stopSparkWarmer := context.WithCancel(context.Background())
	if sparkWarmer := worker.NewSparkWarmer(catalogSearcher, priceChain, 10); sparkWarmer != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			worker.RunSparkWarmer(sparkCtx, sparkWarmer, worker.DefaultSparkWarmInterval)
		}()
		slog.Info("spark warmer started")
	}

	// lane: watchlist
	// Fires price alerts once a minute. The notifier logs until push delivery is wired.
	alertPoller := worker.NewAlertPoller(store, alertMarks, app.LogAlertNotifier{}, nil)
	alertCtx, stopAlertPoller := context.WithCancel(context.Background())
	workers.Add(1)
	go func() {
		defer workers.Done()
		worker.RunAlertPoller(alertCtx, alertPoller, worker.DefaultAlertPollInterval)
	}()
	// lane: matchups — freezes ended weeks and draws the current one, on boot and every few minutes.
	matchupCtx, stopMatchupPoller := context.WithCancel(context.Background())
	workers.Add(1)
	go func() {
		defer workers.Done()
		worker.RunMatchupPoller(matchupCtx, matchups, worker.DefaultMatchupInterval)
	}()

	return &bootResult{
		Server:            newHTTPServer(addr, platformHandler(mux, privyClient, httpapi.NewIdempotency(store, privyClient))),
		Config:            cfg,
		Relayer:           relayer,
		DB:                db,
		stopPoller:        stopPoller,
		stopExecutePoller: stopExecutePoller,
		stopRedeemPoller:  stopRedeemPoller,
		stopSparkWarmer:   stopSparkWarmer,
		// lane: notifications
		stopNotificationPoller: stopNotificationPoller,
		// lane: watchlist
		stopAlertPoller: stopAlertPoller,
		// lane: matchups
		stopMatchupPoller: stopMatchupPoller,
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
	result.stopSparkWarmer()
	// lane: notifications
	result.stopNotificationPoller()
	// lane: watchlist
	result.stopAlertPoller()
	// lane: matchups
	result.stopMatchupPoller()
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
