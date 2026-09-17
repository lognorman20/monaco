// Ops helper: sweep USDC from Monaco-controlled wallets to --destination.
// Usage: ./scripts/sweep-wallets.sh --destination <addr> [--all] [--dry-run]
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type sweepSource struct {
	kind    string
	address string
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

	var swept, skipped, failed int
	for _, src := range sources {
		if src.address == flags.destination {
			fmt.Printf("skip %s %s (same as destination)\n", src.kind, src.address)
			skipped++
			continue
		}
		amount, err := client.MemberUSDCBalance(ctx, src.address)
		if err != nil {
			fmt.Fprintf(os.Stderr, "balance %s %s: %v\n", src.kind, src.address, err)
			failed++
			continue
		}
		if amount <= 0 {
			fmt.Printf("skip %s %s (zero USDC)\n", src.kind, src.address)
			skipped++
			continue
		}
		if flags.dryRun {
			fmt.Printf("dry-run would sweep %s from=%s dest=%s amount=%d\n", src.kind, src.address, flags.destination, amount)
			swept++
			continue
		}
		req, err := privy.BuildSweepRequest(src.address, flags.destination, amount, cfg.RelayerPrivateKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "build %s %s: %v\n", src.kind, src.address, err)
			failed++
			continue
		}
		result, err := client.SubmitSweep(ctx, req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "submit %s %s amount=%d: %v\n", src.kind, src.address, amount, err)
			failed++
			continue
		}
		fmt.Printf("ok %s from=%s dest=%s amount=%d tx=%s\n", src.kind, src.address, flags.destination, amount, result.TxSignature)
		swept++
	}

	mode := "live"
	if flags.dryRun {
		mode = "dry-run"
	}
	fmt.Printf("done mode=%s swept=%d skipped=%d failed=%d dest=%s\n", mode, swept, skipped, failed, flags.destination)
	if failed > 0 {
		os.Exit(1)
	}
}

func loadSweepSources(ctx context.Context, flags sweepFlags, cfg *config.Config, client *privy.HTTPClient) ([]sweepSource, string, error) {
	if flags.all {
		wallets, err := client.ListAppSolanaWallets(ctx)
		if err != nil {
			return nil, "", err
		}
		var sources []sweepSource
		for _, wallet := range wallets {
			sources = append(sources, sweepSource{kind: "privy", address: wallet.SolanaAddress})
		}
		return sources, "privy app wallets (--all)", nil
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, "", err
	}
	defer db.Close()
	store := postgres.NewStore(db)
	sources, err := listDBSweepSources(ctx, store)
	if err != nil {
		return nil, "", err
	}
	return sources, "postgres member_wallets + treasuries", nil
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
