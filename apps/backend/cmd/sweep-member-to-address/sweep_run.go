package main

import (
	"context"
	"fmt"
	"os"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/solana/txsign"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

type sweepRunner struct {
	flags       sweepFlags
	cfg         *config.Config
	privy       *privy.HTTPClient
	jupiter     jupiter.Client
	signer      app.TreasurySigner
	relayerPub  string
	relayerKey  string
	mintCatalog xstocks.MintCatalog
	prices      jupiter.PriceClient
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
		var estUSDCFromSells int64

		walletID, err := runner.walletID(ctx, src)
		if err != nil {
			msg := fmt.Sprintf("wallet id: %v", err)
			fmt.Fprintf(os.Stderr, "wallet id %s %s: %v\n", src.kind, src.address, err)
			fmt.Printf("fail wallet %s from=%s err=%s\n", src.kind, src.address, oneLineErr(msg))
			plan.markFailed()
			plan.actions = append(plan.actions, sweepAction{
				kind:   "wallet",
				status: actionFail,
				errMsg: msg,
			})
			recap.addWallet(plan)
			failed++
			continue
		}

		tokens, err := runner.privy.ListSPLTokenBalances(ctx, src.address)
		if err != nil {
			msg := fmt.Sprintf("token balances: %v", err)
			fmt.Fprintf(os.Stderr, "token balances %s %s: %v\n", src.kind, src.address, err)
			fmt.Printf("fail wallet %s from=%s err=%s\n", src.kind, src.address, oneLineErr(msg))
			plan.markFailed()
			plan.actions = append(plan.actions, sweepAction{
				kind:   "wallet",
				status: actionFail,
				errMsg: msg,
			})
			recap.addWallet(plan)
			failed++
			continue
		}

		for _, token := range tokens {
			if token.Mint == jupiter.USDCMint {
				continue
			}
			outcome, err := runner.swapTokenToUSDC(ctx, src, walletID, token)
			action := sweepAction{
				kind:      "jupiter-sell",
				label:     assetLabel(ctx, token.Mint, runner.mintCatalog),
				mint:      token.Mint,
				rawAmount: token.Amount,
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "jupiter swap %s %s mint=%s amount=%d: %v\n", src.kind, src.address, token.Mint, token.Amount, err)
				fmt.Printf("fail jupiter-sell %s from=%s mint=%s amount=%d err=%s\n",
					src.kind, src.address, token.Mint, token.Amount, oneLineErr(err.Error()))
				action.status = actionFail
				action.errMsg = err.Error()
				plan.markFailed()
				plan.actions = append(plan.actions, action)
				failed++
				continue
			}

			switch {
			case outcome.routable && outcome.outUSDC > 0:
				action.estUSDCOut = &outcome.outUSDC
				estUSDCFromSells += outcome.outUSDC
			case outcome.quoteErr != nil:
				action.note = "Jupiter quote failed"
			case !outcome.routable:
				action.note = "no Jupiter route"
			}
			action.status = swapActionStatus(outcome, runner.flags.dryRun)
			if action.note == "" {
				action.note = swapActionNote(outcome)
			}
			action.txSig = outcome.txSig
			plan.actions = append(plan.actions, action)

			if outcome.executed {
				swept++
			} else if outcome.skipped {
				skipped++
			}
		}

		amount, err := runner.privy.MemberUSDCBalance(ctx, src.address)
		if err != nil {
			msg := fmt.Sprintf("balance: %v", err)
			fmt.Fprintf(os.Stderr, "balance %s %s: %v\n", src.kind, src.address, err)
			fmt.Printf("fail usdc-sweep %s from=%s err=%s\n", src.kind, src.address, oneLineErr(msg))
			plan.markFailed()
			plan.actions = append(plan.actions, sweepAction{
				kind:   "usdc-sweep",
				label:  "USDC",
				mint:   jupiter.USDCMint,
				status: actionFail,
				errMsg: msg,
			})
			plan.drainTotal = amount + estUSDCFromSells
			recap.addWallet(plan)
			failed++
			continue
		}

		sweepAction := sweepAction{
			kind:      "usdc-sweep",
			label:     "USDC",
			mint:      jupiter.USDCMint,
			rawAmount: amount,
		}

		plan.drainTotal = amount + estUSDCFromSells

		if amount <= 0 {
			sweepAction.status = actionSkipped
			sweepAction.note = "zero USDC balance"
			plan.actions = append(plan.actions, sweepAction)
			recap.addWallet(plan)
			if plan.drainTotal <= 0 {
				fmt.Printf("skip %s %s (zero USDC)\n", src.kind, src.address)
				skipped++
				continue
			}
			fmt.Printf("skip %s %s (zero USDC now; drain total from planned sells=%s)\n",
				src.kind, src.address, formatUSDCAtomic(plan.drainTotal))
			skipped++
			continue
		}

		if runner.flags.dryRun {
			fmt.Printf("dry-run would sweep %s from=%s dest=%s mint=%s amount=%d jupiter_swap=false no_tx_sent\n",
				src.kind, src.address, runner.flags.destination, jupiter.USDCMint, amount)
			sweepAction.status = actionDryRun
			sweepAction.note = "no tx sent"
			plan.actions = append(plan.actions, sweepAction)
			recap.addWallet(plan)
			swept++
			continue
		}

		req, err := privy.BuildSweepRequest(src.address, runner.flags.destination, amount, runner.relayerKey)
		if err != nil {
			msg := err.Error()
			fmt.Fprintf(os.Stderr, "build %s %s: %v\n", src.kind, src.address, err)
			fmt.Printf("fail usdc-sweep %s from=%s mint=%s amount=%d err=%s\n",
				src.kind, src.address, jupiter.USDCMint, amount, oneLineErr(msg))
			sweepAction.status = actionFail
			sweepAction.errMsg = msg
			plan.markFailed()
			plan.actions = append(plan.actions, sweepAction)
			recap.addWallet(plan)
			failed++
			continue
		}
		result, err := runner.privy.SubmitSweep(ctx, req)
		if err != nil {
			msg := err.Error()
			fmt.Fprintf(os.Stderr, "submit %s %s amount=%d: %v\n", src.kind, src.address, amount, err)
			fmt.Printf("fail usdc-sweep %s from=%s mint=%s amount=%d err=%s\n",
				src.kind, src.address, jupiter.USDCMint, amount, oneLineErr(msg))
			sweepAction.status = actionFail
			sweepAction.errMsg = msg
			plan.markFailed()
			plan.actions = append(plan.actions, sweepAction)
			recap.addWallet(plan)
			failed++
			continue
		}
		fmt.Printf("ok usdc-sweep %s from=%s dest=%s mint=%s amount=%d tx=%s\n",
			src.kind, src.address, runner.flags.destination, jupiter.USDCMint, amount, result.TxSignature)
		sweepAction.status = actionOK
		sweepAction.txSig = result.TxSignature
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
	return runner.privy.LookupWalletID(ctx, src.address)
}

func (runner sweepRunner) swapTokenToUSDC(ctx context.Context, src sweepSource, walletID string, token privy.SPLTokenBalance) (swapOutcome, error) {
	plan, err := resolveSweepSell(ctx, runner.mintCatalog, runner.prices, token.Mint, token.Amount)
	if err != nil {
		return swapOutcome{}, err
	}
	if plan.SkipDust {
		fmt.Printf("skip dust %s from=%s mint=%s amount=%d whole=%s value_usdc_micros=%d\n",
			src.kind, src.address, token.Mint, token.Amount, plan.WholeTokens, plan.ValueUSDCMicros)
		return swapOutcome{skipped: true}, nil
	}
	quote, err := runner.jupiter.QuoteSell(ctx, jupiter.QuoteSellParams{
		GroupID:     "ops-sweep",
		UserID:      "ops-sweep",
		Symbol:      token.Mint,
		InputMint:   token.Mint,
		Amount:      token.Amount,
		Taker:       src.address,
		SlippageBps: plan.SlippageBps,
	})
	if err != nil {
		if runner.flags.dryRun {
			fmt.Printf("dry-run %s from=%s dest=%s mint=%s amount=%d whole=%s slippage_bps=%d jupiter_swap=true routable=false quote_err=%v no_tx_sent\n",
				src.kind, src.address, runner.flags.destination, token.Mint, token.Amount, plan.WholeTokens, plan.SlippageBps, err)
			return swapOutcome{skipped: true, quoteErr: err}, nil
		}
		return swapOutcome{}, err
	}
	if !quote.Routable {
		if runner.flags.dryRun {
			fmt.Printf("dry-run %s from=%s dest=%s mint=%s amount=%d whole=%s slippage_bps=%d jupiter_swap=true routable=false no_tx_sent\n",
				src.kind, src.address, runner.flags.destination, token.Mint, token.Amount, plan.WholeTokens, plan.SlippageBps)
			return swapOutcome{skipped: true, routable: false}, nil
		}
		return swapOutcome{}, fmt.Errorf("no route")
	}

	outUSDC, err := parseUSDCAmount(quote.OutAmount)
	if err != nil {
		if runner.flags.dryRun {
			fmt.Printf("dry-run %s from=%s dest=%s mint=%s amount=%d whole=%s slippage_bps=%d jupiter_swap=true routable=true out_usdc=invalid no_tx_sent\n",
				src.kind, src.address, runner.flags.destination, token.Mint, token.Amount, plan.WholeTokens, plan.SlippageBps)
			return swapOutcome{skipped: true, routable: true, quoteErr: err}, nil
		}
		return swapOutcome{}, fmt.Errorf("parse out amount: %w", err)
	}

	if runner.flags.dryRun {
		fmt.Printf("dry-run %s from=%s dest=%s mint=%s amount=%d whole=%s slippage_bps=%d jupiter_swap=true routable=true out_usdc=%s no_tx_sent\n",
			src.kind, src.address, runner.flags.destination, token.Mint, token.Amount, plan.WholeTokens, plan.SlippageBps, quote.OutAmount)
		return swapOutcome{executed: true, routable: true, outUSDC: outUSDC}, nil
	}

	signedTx, err := runner.signSwapTransaction(ctx, walletID, quote.Transaction)
	if err != nil {
		return swapOutcome{}, err
	}
	_, err = runner.jupiter.SellToUSDC(ctx, jupiter.SellToUSDCParams{
		GroupID:           "ops-sweep",
		UserID:            "ops-sweep",
		Symbol:            token.Mint,
		RequestID:         quote.RequestID,
		SignedTransaction: signedTx,
		InputMint:         token.Mint,
		OutputMint:        jupiter.USDCMint,
		Amount:            token.Amount,
	})
	if err != nil {
		return swapOutcome{}, err
	}

	fill, err := jupiter.PollUntilConfirmed(ctx, runner.jupiter, jupiter.PollExecuteParams{
		GroupID:           "ops-sweep",
		UserID:            "ops-sweep",
		Symbol:            token.Mint,
		RequestID:         quote.RequestID,
		SignedTransaction: signedTx,
	}, jupiter.DefaultPollConfig())
	if err != nil {
		return swapOutcome{}, err
	}
	if !fill.IsConfirmedSuccess() {
		return swapOutcome{}, fmt.Errorf("jupiter sell not confirmed: status=%s code=%d", fill.Status, fill.Code)
	}
	fmt.Printf("ok jupiter-sell %s from=%s mint=%s amount=%d out_usdc=%s tx=%s\n",
		src.kind, src.address, token.Mint, token.Amount, fill.OutputAmountResult, fill.Signature)
	liveOut, err := parseUSDCAmount(fill.OutputAmountResult)
	if err != nil {
		liveOut = outUSDC
	}
	return swapOutcome{executed: true, routable: true, outUSDC: liveOut, txSig: fill.Signature}, nil
}

func (runner sweepRunner) signSwapTransaction(ctx context.Context, walletID, unsignedTx string) (string, error) {
	tx := unsignedTx
	if runner.relayerKey != "" {
		var err error
		tx, err = txsign.SignLocalSignerIfRequired(tx, runner.relayerKey)
		if err != nil {
			return "", fmt.Errorf("sign fee payer: %w", err)
		}
	}
	return runner.signer.SignTreasuryTransaction(ctx, walletID, tx)
}
