package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// Clock provides time for poller ticks in tests.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// stubClock is the locked test double for poller timing.
type stubClock struct {
	now time.Time
}

// NewStubClock returns a fixed clock for tests.
func NewStubClock(now time.Time) *stubClock {
	return &stubClock{now: now}
}

func (c *stubClock) Now() time.Time { return c.now }

// DefaultPollInterval is how often the API process polls pending deposits.
const DefaultPollInterval = 3 * time.Second

// PollerWake coalesces immediate tick requests (e.g. right after fund intent created).
type PollerWake struct {
	ch chan struct{}
}

// NewPollerWake returns a wake channel for Run.
func NewPollerWake() *PollerWake {
	return &PollerWake{ch: make(chan struct{}, 1)}
}

// Notify requests an immediate poller tick; coalesced if one is already queued.
func (w *PollerWake) Notify() {
	if w == nil {
		return
	}
	select {
	case w.ch <- struct{}{}:
	default:
	}
}

func (w *PollerWake) wakeChan() <-chan struct{} {
	if w == nil {
		return nil
	}
	return w.ch
}

// SweepPoller polls member USDC balances and submits sweeps to treasury.
type SweepPoller struct {
	store    *postgres.Store
	privy    privy.Client
	rpc      SolanaRPC
	deposits *app.DepositService
	relayer  string
	clock    Clock
}

// NewSweepPoller wires sweep polling dependencies.
func NewSweepPoller(store *postgres.Store, privyClient privy.Client, rpc SolanaRPC, deposits *app.DepositService, relayerKey string, clock Clock) *SweepPoller {
	if clock == nil {
		clock = systemClock{}
	}
	return &SweepPoller{
		store:    store,
		privy:    privyClient,
		rpc:      rpc,
		deposits: deposits,
		relayer:  relayerKey,
		clock:    clock,
	}
}

// Run ticks the poller until ctx is cancelled. wake triggers an immediate coalesced tick.
func Run(ctx context.Context, poller *SweepPoller, interval time.Duration, wake *PollerWake) {
	if poller == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	logSweepPollerStarted(interval)
	telemetry.RegisterPoller(PollerSweep, interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer logSweepPollerStopped()

	runTick := func() {
		tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		telemetry.GuardTick(ctx, PollerSweep, func() error {
			err := poller.Tick(tickCtx)
			if err != nil && ctx.Err() == nil {
				slog.Error("sweep poller tick failed", "err", err)
			}
			return err
		})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runTick()
		case <-wake.wakeChan():
			runTick()
		}
	}
}

// Tick evaluates user-initiated pending deposits once.
func (p *SweepPoller) Tick(ctx context.Context) error {
	_ = p.clock.Now()

	pending, err := p.store.ListPendingDeposits(ctx)
	if err != nil {
		logSweepPollerListPendingFailed(err)
		return err
	}

	logSweepPollerTickStart(len(pending))

	for _, deposit := range pending {
		if err := p.processPendingDeposit(ctx, deposit); err != nil {
			logSweepPollerTickEnd(len(pending), err)
			return err
		}
	}

	// Surplus reconcile scans every real group; skip while fund sweeps are in flight.
	if len(pending) == 0 {
		if err := p.reconcileAllTreasurySurplus(ctx); err != nil {
			logSweepPollerTickEnd(len(pending), err)
			return err
		}
	}

	logSweepPollerTickEnd(len(pending), nil)
	return nil
}

func (p *SweepPoller) reconcileAllTreasurySurplus(ctx context.Context) error {
	// Faker scale clubs (#153) have dummy treasuries: never read their balance via Privy.
	groupIDs, err := p.store.ListRealGroupIDs(ctx)
	if err != nil {
		return err
	}
	for _, groupID := range groupIDs {
		if _, err := p.deposits.CreditUncreditedTreasuryUSDC(ctx, groupID); err != nil {
			slog.Warn("sweep treasury surplus reconcile failed",
				"group_id", groupID,
				"stage", "reconcile_surplus",
				"err", err,
			)
			return err
		}
	}
	return nil
}

func (p *SweepPoller) processPendingDeposit(ctx context.Context, deposit postgres.DepositRow) error {
	txSignature := depositBroadcastSignature(deposit)
	logSweepDepositProcessing(
		deposit.ID,
		deposit.GroupID,
		deposit.UserID,
		deposit.Amount,
		deposit.FromAddress,
		deposit.Status,
		txSignature != "",
	)

	treasury, found, err := p.store.GetTreasuryByGroupID(ctx, deposit.GroupID)
	if err != nil {
		logSweepDepositFailed(deposit.ID, deposit.GroupID, "treasury_lookup", err)
		return err
	}
	if !found {
		err := fmt.Errorf("treasury not found for group %s", deposit.GroupID)
		logSweepDepositFailed(deposit.ID, deposit.GroupID, "treasury_not_found", err)
		return err
	}

	if txSignature == "" {
		balance, err := p.privy.MemberUSDCBalance(ctx, deposit.FromAddress)
		logSweepBalanceCheck(deposit.ID, deposit.FromAddress, balance, deposit.Amount, err)
		if err != nil {
			logSweepDepositFailed(deposit.ID, deposit.GroupID, "balance_check", err)
			return err
		}
		if balance < deposit.Amount {
			logSweepDepositSkipped(deposit.ID, deposit.GroupID, "insufficient_balance",
				"balance", balance,
				"required", deposit.Amount,
			)
			return nil
		}

		sweepAmount := deposit.Amount
		logSweepAttempt(deposit.GroupID, deposit.UserID, deposit.ID, sweepAmount, deposit.FromAddress, treasury.SolanaAddress)

		req, err := privy.BuildSweepRequest(deposit.FromAddress, treasury.SolanaAddress, sweepAmount, p.relayer)
		if err != nil {
			p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, "build_sweep_request", err)
			return nil
		}
		result, err := p.privy.SubmitSweep(ctx, req)
		if err != nil {
			p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, "submit_sweep", err)
			return nil
		}
		txSignature = result.TxSignature
		logSweepBroadcastSubmitted(deposit.ID, deposit.GroupID, txSignature, treasury.SolanaAddress)

		if err := p.store.SetDepositBroadcastSignature(ctx, deposit.ID, txSignature); err != nil {
			logSweepDepositFailed(deposit.ID, deposit.GroupID, "persist_broadcast_signature", err)
			return err
		}
	} else {
		logSweepDepositResuming(deposit.ID, deposit.GroupID, txSignature)
	}

	return p.confirmAndObserveSweep(ctx, deposit, treasury.SolanaAddress, txSignature)
}

func (p *SweepPoller) markDepositSweepFailed(ctx context.Context, depositID, groupID, stage string, err error) {
	logSweepDepositFailed(depositID, groupID, stage, err)
	if _, ok, failErr := p.store.FailDeposit(ctx, depositID, stage); failErr != nil {
		slog.Error("persist deposit failed status",
			"deposit_id", depositID,
			"group_id", groupID,
			"stage", stage,
			"err", failErr,
		)
		return
	} else if !ok {
		slog.Warn("deposit not pending, skip fail mark",
			"deposit_id", depositID,
			"group_id", groupID,
			"stage", stage,
		)
	}
}

func depositBroadcastSignature(deposit postgres.DepositRow) string {
	if deposit.TxSignature.Valid {
		return deposit.TxSignature.String
	}
	return ""
}

func (p *SweepPoller) confirmAndObserveSweep(ctx context.Context, deposit postgres.DepositRow, treasuryAddress, txSignature string) error {
	confirmed, err := p.rpc.IsConfirmed(ctx, txSignature)
	logSweepConfirmationCheck(deposit.ID, txSignature, confirmed, err)
	if err != nil {
		logSweepDepositFailed(deposit.ID, deposit.GroupID, "confirmation_check", err)
		return err
	}
	if !confirmed {
		logSweepDepositSkipped(deposit.ID, deposit.GroupID, "awaiting_confirmation",
			"tx_signature", txSignature,
		)
		return nil
	}

	logSweepConfirm(deposit.GroupID, deposit.UserID, deposit.ID, txSignature)

	_, err = p.deposits.ObserveSweep(ctx, app.ObservedSweep{
		TxSignature: txSignature,
		FromAddress: deposit.FromAddress,
		ToAddress:   treasuryAddress,
		Amount:      deposit.Amount,
		DepositID:   deposit.ID,
		UserID:      deposit.UserID,
		GroupID:     deposit.GroupID,
	})
	if err != nil {
		slog.Warn("deposit observe sweep failed; will retry on next tick",
			"deposit_id", deposit.ID,
			"group_id", deposit.GroupID,
			"tx_signature", txSignature,
			"err", err,
		)
		return nil
	}

	logSweepDepositCredited(deposit.ID, deposit.GroupID, deposit.UserID, txSignature)
	return nil
}
