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
	sources     []string
}

type sourceFlagValues []string

func (values *sourceFlagValues) String() string {
	return strings.Join(*values, ",")
}

func (values *sourceFlagValues) Set(raw string) error {
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		*values = append(*values, part)
	}
	return nil
}

func parseSweepFlags(args []string, errOut io.Writer) (sweepFlags, error) {
	fs := flag.NewFlagSet("sweep-member-to-address", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dest := fs.String("destination", "", "Solana address that receives all swept USDC")
	all := fs.Bool("all", false, "use Privy as source of truth: every Solana wallet in this app")
	dryRun := fs.Bool("dry-run", false, "list balances and planned sweeps; send no transactions")
	var sources sourceFlagValues
	fs.Var(&sources, "source", "Solana wallet to drain (repeatable; comma-separated in one value)")
	fs.Usage = func() {
		fmt.Fprintf(errOut, "usage: %s --destination <wallet_address> [--source <addr>] [--all] [--dry-run]\n", fs.Name())
		fmt.Fprintln(errOut, "default: sweep DB member wallets + group treasuries")
		fmt.Fprintln(errOut, "--source: drain only the listed wallet(s); repeat or comma-separate")
		fmt.Fprintln(errOut, "--all: sweep every Solana wallet Privy returns for this app")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return sweepFlags{}, err
	}
	if fs.NArg() != 0 {
		return sweepFlags{}, fmt.Errorf("unexpected args: %s (use --destination / --source)", strings.Join(fs.Args(), " "))
	}
	destination := strings.TrimSpace(*dest)
	if destination == "" {
		fs.Usage()
		return sweepFlags{}, fmt.Errorf("--destination is required")
	}
	if *all && len(sources) > 0 {
		return sweepFlags{}, fmt.Errorf("--all cannot be combined with --source")
	}
	return sweepFlags{
		destination: destination,
		all:         *all,
		dryRun:      *dryRun,
		sources:     append([]string(nil), sources...),
	}, nil
}
