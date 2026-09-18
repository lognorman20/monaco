// Ops helper: sweep USDC from Monaco-controlled wallets to --destination.
// Non-USDC SPL holdings are sold to USDC on Jupiter first.
// Usage: ./scripts/sweep-wallets.sh --destination <addr> [--all] [--dry-run]
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type sweepSource struct {
	kind     string
	address  string
	walletID string
}

func main() {
	flags, err := parseSweepFlags(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	relayer, err := config.LoadRelayer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "relayer: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	client := privy.NewHTTPClient(cfg)

	sources, sourceNote, err := loadSweepSources(ctx, flags, cfg, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list wallets: %v\n", err)
		os.Exit(1)
	}

	if err := confirmSweep(os.Stdin, os.Stdout, confirmOpts{
		dest:        flags.destination,
		databaseURL: cfg.DatabaseURL,
		sourceNote:  sourceNote,
		dryRun:      flags.dryRun,
		sources:     sources,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "abort: %v\n", err)
		os.Exit(1)
	}

	jupiterClient := jupiter.NewHTTPClientWithPayer(relayer.PublicKey())
	runner := sweepRunner{
		flags:       flags,
		cfg:         cfg,
		privy:       client,
		jupiter:     jupiterClient,
		signer:      app.NewPrivyTreasurySigner(client),
		relayerPub:  relayer.PublicKey(),
		relayerKey:  cfg.RelayerPrivateKey,
		mintCatalog: xstocks.NewHTTPCatalogSearcher(),
	}

	swept, skipped, failed, recap := runSweep(ctx, runner, sources)
	printSweepRecap(os.Stdout, recap)

	mode := "live"
	if flags.dryRun {
		mode = "dry-run"
	}
	fmt.Printf("done mode=%s swept=%d skipped=%d failed=%d dest=%s no_live_tx=%t\n",
		mode, swept, skipped, failed, flags.destination, flags.dryRun)
	if failed > 0 {
		os.Exit(1)
	}
}

func loadSweepSources(ctx context.Context, flags sweepFlags, cfg *config.Config, client *privy.HTTPClient) ([]sweepSource, string, error) {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, "", err
	}
	defer db.Close()
	store := postgres.NewStore(db)

	memberSet, treasurySet, err := walletKindSets(ctx, store)
	if err != nil {
		return nil, "", err
	}

	if flags.all {
		wallets, err := client.ListAppSolanaWallets(ctx)
		if err != nil {
			return nil, "", err
		}
		var sources []sweepSource
		for _, wallet := range wallets {
			sources = append(sources, sweepSource{
				kind:     classifyWalletKind(wallet.SolanaAddress, memberSet, treasurySet, "privy"),
				address:  wallet.SolanaAddress,
				walletID: wallet.PrivyWalletID,
			})
		}
		return sources, "privy app wallets (--all)", nil
	}

	sources, err := listDBSweepSources(ctx, store)
	if err != nil {
		return nil, "", err
	}
	return sources, "postgres member_wallets + treasuries", nil
}

func walletKindSets(ctx context.Context, store *postgres.Store) (members, treasuries map[string]struct{}, err error) {
	members = map[string]struct{}{}
	treasuries = map[string]struct{}{}

	memberWallets, err := store.ListMemberWallets(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, wallet := range memberWallets {
		if wallet.SolanaAddress != "" {
			members[wallet.SolanaAddress] = struct{}{}
		}
	}

	treasuryRows, err := store.ListTreasuries(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, treasury := range treasuryRows {
		if treasury.SolanaAddress != "" {
			treasuries[treasury.SolanaAddress] = struct{}{}
		}
	}
	return members, treasuries, nil
}

func classifyWalletKind(address string, members, treasuries map[string]struct{}, fallback string) string {
	if _, ok := members[address]; ok {
		return "member"
	}
	if _, ok := treasuries[address]; ok {
		return "treasury"
	}
	return fallback
}

func listDBSweepSources(ctx context.Context, store *postgres.Store) ([]sweepSource, error) {
	members, err := store.ListMemberWallets(ctx)
	if err != nil {
		return nil, err
	}
	treasuries, err := store.ListTreasuries(ctx)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var sources []sweepSource
	add := func(kind, address string) {
		if address == "" {
			return
		}
		if _, ok := seen[address]; ok {
			return
		}
		seen[address] = struct{}{}
		sources = append(sources, sweepSource{kind: kind, address: address})
	}
	for _, wallet := range members {
		add("member", wallet.SolanaAddress)
	}
	for _, treasury := range treasuries {
		add("treasury", treasury.SolanaAddress)
	}
	return sources, nil
}
