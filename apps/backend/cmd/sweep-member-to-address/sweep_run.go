package main

import (
	"context"
	"fmt"
	"os"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

type sweepRunner struct {
	flags       sweepFlags
	cfg         *config.Config
	wallets     wallets.Client
	dex         dex.Client
	relayerPub  string
	mintCatalog b20.Catalog
}

type swapOutcome struct {
	executed bool
	skipped  bool
	outUSDC  int64
	routable bool
	quoteErr error
	txSig    string
}

func runSweep(ctx context.Context, runner sweepRunner, sources []sweepSource) (swept, skipped, failed int, recap sweepRecap) {
	recap = sweepRecap{
		dryRun:      runner.flags.dryRun,
		destination: runner.flags.destination,
	}

	for _, src := range sources {
		if src.address == runner.relayerPub {
			fmt.Printf("skip relayer %s (fee payer; do not drain)\n", src.address)
			skipped++
			continue
		}
		if src.address == runner.flags.destination {
			fmt.Printf("skip %s %s (same as destination)\n", src.kind, src.address)
			skipped++
			continue
		}

		plan := walletPlan{kind: src.kind, address: src.address}

		amount, err := runner.wallets.MemberUSDCBalance(ctx, src.address)
		if err != nil {
			msg := fmt.Sprintf("balance: %v", err)
			fmt.Fprintf(os.Stderr, "balance %s %s: %v\n", src.kind, src.address, err)
			fmt.Printf("fail usdc-sweep %s from=%s err=%s\n", src.kind, src.address, oneLineErr(msg))
			plan.markFailed()
			plan.actions = append(plan.actions, sweepAction{
				kind:   "usdc-sweep",
				label:  "USDC",
				mint:   evm.USDCAddress,
				status: actionFail,
				errMsg: msg,
			})
			recap.addWallet(plan)
			failed++
			continue
		}

		sweepAction := sweepAction{
			kind:      "usdc-sweep",
			label:     "USDC",
			mint:      evm.USDCAddress,
			rawAmount: amount,
		}
		plan.drainTotal = amount

		if amount <= 0 {
			sweepAction.status = actionSkipped
			sweepAction.note = "zero USDC balance"
			plan.actions = append(plan.actions, sweepAction)
			recap.addWallet(plan)
			fmt.Printf("skip %s %s (zero USDC)\n", src.kind, src.address)
			skipped++
			continue
		}

		if runner.flags.dryRun {
			fmt.Printf("dry-run would sweep %s from=%s dest=%s mint=%s amount=%d no_tx_sent\n",
				src.kind, src.address, runner.flags.destination, evm.USDCAddress, amount)
			sweepAction.status = actionDryRun
			sweepAction.note = "no tx sent"
			plan.actions = append(plan.actions, sweepAction)
			recap.addWallet(plan)
			swept++
			continue
		}

		req := wallets.SweepRequest{
			MemberAddress:   src.address,
			TreasuryAddress: runner.flags.destination,
			Amount:          amount,
			IntentID:        fmt.Sprintf("ops-sweep:%s", src.address),
		}
		result, err := runner.wallets.SubmitSweep(ctx, req)
		if err != nil {
			msg := err.Error()
			fmt.Fprintf(os.Stderr, "submit %s %s amount=%d: %v\n", src.kind, src.address, amount, err)
			fmt.Printf("fail usdc-sweep %s from=%s mint=%s amount=%d err=%s\n",
				src.kind, src.address, evm.USDCAddress, amount, oneLineErr(msg))
			sweepAction.status = actionFail
			sweepAction.errMsg = msg
			plan.markFailed()
			plan.actions = append(plan.actions, sweepAction)
			recap.addWallet(plan)
			failed++
			continue
		}
		fmt.Printf("ok usdc-sweep %s from=%s dest=%s mint=%s amount=%d tx=%s\n",
			src.kind, src.address, runner.flags.destination, evm.USDCAddress, amount, result.TxHash)
		sweepAction.status = actionOK
		sweepAction.txSig = result.TxHash
		plan.actions = append(plan.actions, sweepAction)
		recap.addWallet(plan)
		swept++
	}
	return swept, skipped, failed, recap
}

func (runner sweepRunner) walletID(ctx context.Context, src sweepSource) (string, error) {
	if src.walletID != "" {
		return src.walletID, nil
	}
	return "", fmt.Errorf("wallet id not available for %s", src.address)
}

func (runner sweepRunner) swapTokenToUSDC(ctx context.Context, src sweepSource, walletID string, token string, amount int64) (swapOutcome, error) {
	_ = ctx
	_ = src
	_ = walletID
	_ = token
	_ = amount
	return swapOutcome{skipped: true}, nil
}
