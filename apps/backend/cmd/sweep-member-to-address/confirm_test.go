package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseSweepFlags_requiresDestination(t *testing.T) {
	t.Parallel()

	if _, err := parseSweepFlags(nil, bytes.NewBuffer(nil)); err == nil {
		t.Fatal("expected error")
	}

	flags, err := parseSweepFlags([]string{"--destination", "Dest111", "--all", "--dry-run"}, bytes.NewBuffer(nil))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if flags.destination != "Dest111" {
		t.Fatalf("destination = %q", flags.destination)
	}
	if !flags.all || !flags.dryRun {
		t.Fatalf("all=%t dryRun=%t", flags.all, flags.dryRun)
	}
}

func TestConfirmSweep_requiresAckAndDestRepeat(t *testing.T) {
	t.Parallel()

	src := []sweepSource{{kind: "member", address: "Mem111"}}
	opts := confirmOpts{
		dest:        "Dest111",
		databaseURL: "postgres://u:p@localhost:54322/monaco",
		sourceNote:  "postgres member_wallets + treasuries",
		sources:     src,
	}
	var out bytes.Buffer
	err := confirmSweep(strings.NewReader("nope\n"), &out, opts)
	if err == nil {
		t.Fatal("expected reject")
	}

	out.Reset()
	input := ackPhrase + "\nDest111\n"
	if err := confirmSweep(strings.NewReader(input), &out, opts); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !strings.Contains(out.String(), "DANGER") {
		t.Fatalf("missing danger text: %s", out.String())
	}
}

func TestConfirmSweep_dryRunSkipsAck(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := confirmSweep(strings.NewReader(""), &out, confirmOpts{
		dest:        "Dest111",
		databaseURL: "postgres://u:p@localhost:54322/monaco",
		sourceNote:  "privy app wallets (--all)",
		dryRun:      true,
		sources:     []sweepSource{{kind: "privy", address: "Mem111"}},
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !strings.Contains(out.String(), "DRY-RUN") {
		t.Fatalf("missing dry-run text: %s", out.String())
	}
}
