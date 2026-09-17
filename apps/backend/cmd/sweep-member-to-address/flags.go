package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

type sweepFlags struct {
	destination string
	all         bool
	dryRun      bool
}

func parseSweepFlags(args []string, errOut io.Writer) (sweepFlags, error) {
	fs := flag.NewFlagSet("sweep-member-to-address", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dest := fs.String("destination", "", "Solana address that receives all swept USDC")
	all := fs.Bool("all", false, "use Privy as source of truth: every Solana wallet in this app")
	dryRun := fs.Bool("dry-run", false, "list balances and planned sweeps; send no transactions")
	fs.Usage = func() {
		fmt.Fprintf(errOut, "usage: %s --destination <wallet_address> [--all] [--dry-run]\n", fs.Name())
		fmt.Fprintln(errOut, "default: sweep DB member wallets + group treasuries")
		fmt.Fprintln(errOut, "--all: sweep every Solana wallet Privy returns for this app")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return sweepFlags{}, err
	}
	if fs.NArg() != 0 {
		return sweepFlags{}, fmt.Errorf("unexpected args: %s (use --destination)", strings.Join(fs.Args(), " "))
	}
	destination := strings.TrimSpace(*dest)
	if destination == "" {
		fs.Usage()
		return sweepFlags{}, fmt.Errorf("--destination is required")
	}
	return sweepFlags{destination: destination, all: *all, dryRun: *dryRun}, nil
}
