package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

type actionStatus string

const (
	actionOK      actionStatus = "ok"
	actionFail    actionStatus = "fail"
	actionSkipped actionStatus = "skipped"
	actionDryRun  actionStatus = "dry-run"
)

type sweepAction struct {
	kind       string // "jupiter-sell" | "usdc-sweep"
	label      string
	mint       string
	rawAmount  int64
	estUSDCOut *int64
	status     actionStatus
	errMsg     string
	txSig      string
	note       string
}

type walletPlan struct {
	kind       string
	address    string
	actions    []sweepAction
	drainTotal int64
	failed     bool
}

type sweepRecap struct {
	dryRun      bool
	destination string
	wallets     []walletPlan
	grandTotal  int64
}

func (plan *walletPlan) markFailed() {
	plan.failed = true
}

func (recap *sweepRecap) addWallet(plan walletPlan) {
	if len(plan.actions) == 0 && plan.drainTotal <= 0 {
		return
	}
	recap.wallets = append(recap.wallets, plan)
	recap.grandTotal += plan.drainTotal
}

func printSweepRecap(out io.Writer, recap sweepRecap) {
	mode := "live"
	if recap.dryRun {
		mode = "dry-run"
	}

	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "=== Sweep summary (%s) ===\n", mode)
	fmt.Fprintf(out, "Destination: %s\n", recap.destination)

	if len(recap.wallets) == 0 {
		fmt.Fprintln(out, "No wallets with planned actions.")
		return
	}

	failWallets := 0
	var okCount, failCount, skipCount, dryCount int
	for _, wallet := range recap.wallets {
		if wallet.failed {
			failWallets++
		}
		for _, action := range wallet.actions {
			switch action.status {
			case actionOK:
				okCount++
			case actionFail:
				failCount++
			case actionSkipped:
				skipCount++
			case actionDryRun:
				dryCount++
			}
		}
	}

	fmt.Fprintf(out, "Wallets: %d", len(recap.wallets))
	if failWallets > 0 {
		fmt.Fprintf(out, " (%d with FAIL actions)", failWallets)
	}
	fmt.Fprintln(out, "")

	for i, wallet := range recap.wallets {
		header := fmt.Sprintf("[%d] %s %s", i+1, wallet.kind, formatAddress(wallet.address))
		if wallet.failed {
			header += " FAIL"
		}
		fmt.Fprintln(out, header)
		for _, action := range wallet.actions {
			fmt.Fprintf(out, "  %s\n", formatActionLine(action))
		}
		if planShowsDrainTotal(wallet) {
			fmt.Fprintf(out, "  Drain total: %s\n", formatUSDCAtomic(wallet.drainTotal))
		}
	}

	fmt.Fprintf(out, "\nGrand total: %s across %d wallet(s)\n",
		formatUSDCAtomic(recap.grandTotal), len(recap.wallets))
	fmt.Fprintf(out, "Actions: ok=%d fail=%d skipped=%d dry-run=%d\n",
		okCount, failCount, skipCount, dryCount)
}

func planShowsDrainTotal(wallet walletPlan) bool {
	return wallet.drainTotal > 0
}

func formatActionLine(action sweepAction) string {
	status := string(action.status)
	if action.status == actionFail {
		status = "FAIL"
	}

	parts := []string{status, action.kind}
	if action.label != "" {
		parts = append(parts, action.label)
	}
	if action.mint != "" {
		parts = append(parts, "mint="+truncateMint(action.mint))
	}
	parts = append(parts, formatActionAmount(action))

	line := strings.Join(parts, " ")

	switch action.status {
	case actionFail:
		if msg := oneLineErr(action.errMsg); msg != "" {
			line += " err=" + msg
		}
	case actionOK:
		if action.txSig != "" {
			line += " tx=" + formatAddress(action.txSig)
		}
	case actionSkipped, actionDryRun:
		if action.note != "" {
			line += " (" + action.note + ")"
		}
		if action.status == actionDryRun && action.txSig != "" {
			line += " tx=" + formatAddress(action.txSig)
		}
	}

	return line
}

func formatActionAmount(action sweepAction) string {
	if action.mint == jupiter.USDCMint {
		return fmt.Sprintf("amount=%s (%d raw)", formatUSDCAtomic(action.rawAmount), action.rawAmount)
	}
	amount := fmt.Sprintf("amount=%d raw", action.rawAmount)
	if action.estUSDCOut != nil {
		amount += fmt.Sprintf(" (~%s est)", formatUSDCAtomic(*action.estUSDCOut))
	}
	return amount
}

func oneLineErr(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	return strings.Join(strings.Fields(msg), " ")
}

func formatUSDCAtomic(micro int64) string {
	sign := ""
	if micro < 0 {
		sign = "-"
		micro = -micro
	}
	whole := micro / 1_000_000
	frac := micro % 1_000_000
	return fmt.Sprintf("%s%d.%06d USDC", sign, whole, frac)
}

func formatAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if len(addr) <= 12 {
		return addr
	}
	return addr[:4] + "…" + addr[len(addr)-4:]
}

func assetLabel(ctx context.Context, mint string, catalog xstocks.MintCatalog) string {
	mint = strings.TrimSpace(mint)
	if mint == jupiter.USDCMint {
		return "USDC"
	}
	if catalog != nil {
		asset, ok, err := catalog.LookupByMint(ctx, mint)
		if err == nil && ok && strings.TrimSpace(asset.Symbol) != "" {
			return strings.TrimSpace(asset.Symbol)
		}
	}
	return truncateMint(mint)
}

func truncateMint(mint string) string {
	mint = strings.TrimSpace(mint)
	if len(mint) <= 12 {
		return mint
	}
	return mint[:6] + "…" + mint[len(mint)-4:]
}

func parseUSDCAmount(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func swapActionStatus(outcome swapOutcome, dryRun bool) actionStatus {
	if dryRun && outcome.executed {
		return actionDryRun
	}
	if outcome.executed {
		return actionOK
	}
	return actionSkipped
}

func swapActionNote(outcome swapOutcome) string {
	switch {
	case outcome.quoteErr != nil:
		return "Jupiter quote failed"
	case !outcome.routable:
		return "no Jupiter route"
	default:
		return ""
	}
}
