package main

import (
	"bytes"
	"strings"
	"testing"
)

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
		sourceNote:  "database member wallets (--all)",
		dryRun:      true,
		sources:     []sweepSource{{kind: "member", address: "Mem111"}},
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !strings.Contains(out.String(), "DRY-RUN") {
		t.Fatalf("missing dry-run text: %s", out.String())
	}
}
