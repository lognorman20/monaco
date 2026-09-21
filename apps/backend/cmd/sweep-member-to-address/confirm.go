package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const ackPhrase = "I UNDERSTAND THIS MAY MESS WITH PROD"

type confirmOpts struct {
	dest        string
	databaseURL string
	sourceNote  string
	dryRun      bool
	sources     []sweepSource
}

func confirmSweep(in io.Reader, out io.Writer, opts confirmOpts) error {
	fmt.Fprintln(out, "")
	if opts.dryRun {
		fmt.Fprintln(out, "DRY-RUN: no transactions. Balances + plan only.")
	} else {
		fmt.Fprintln(out, "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		fmt.Fprintln(out, "DANGER: ops USDC sweep. Can empty live wallets.")
		fmt.Fprintln(out, "Can break prod balances, share credits, pending deposits.")
		fmt.Fprintln(out, "Use only if you know DATABASE_URL and dest are correct.")
		fmt.Fprintln(out, "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	}
	fmt.Fprintf(out, "DATABASE_URL=%s\n", redactDatabaseURL(opts.databaseURL))
	fmt.Fprintf(out, "destination=%s\n", opts.dest)
	fmt.Fprintf(out, "source=%s\n", opts.sourceNote)
	fmt.Fprintf(out, "dry_run=%t\n", opts.dryRun)
	fmt.Fprintf(out, "sources=%d\n", len(opts.sources))
	for _, src := range opts.sources {
		fmt.Fprintf(out, "  - %s %s\n", src.kind, src.address)
	}
	if opts.dryRun {
		return nil
	}
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "Type exactly: %s\n", ackPhrase)

	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("confirmation aborted")
	}
	if strings.TrimSpace(scanner.Text()) != ackPhrase {
		return fmt.Errorf("confirmation rejected")
	}

	fmt.Fprintln(out, "Type destination wallet address again:")
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("destination confirmation aborted")
	}
	if strings.TrimSpace(scanner.Text()) != opts.dest {
		return fmt.Errorf("destination confirmation mismatch")
	}
	return nil
}

func redactDatabaseURL(raw string) string {
	if raw == "" {
		return "(empty)"
	}
	at := strings.LastIndex(raw, "@")
	if at == -1 {
		return raw
	}
	return "***@" + raw[at+1:]
}
