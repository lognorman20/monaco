package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
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
const DefaultPollInterval = 15 * time.Second

// SweepPoller polls member USDC balances and submits sweeps to treasury.
type SweepPoller struct {
	store    *postgres.Store
	privy    wallets.Client
	rpc      Confirmer
	deposits *app.DepositService
	relayer  string
	clock    Clock
}

// NewSweepPoller wires sweep polling dependencies.
func NewSweepPoller(store *postgres.Store, privyClient wallets.Client, rpc Confirmer, deposits *app.DepositService, relayerKey string, clock Clock) *SweepPoller {
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

// Run ticks the poller until ctx is cancelled.
func Run(ctx context.Context, poller *SweepPoller, interval time.Duration) {
	if poller == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	logSweepPollerStarted(interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer logSweepPollerStopped()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := poller.Tick(tickCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Error("sweep poller tick failed", "err", err)
			}
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

	if err := p.reconcileAllTreasurySurplus(ctx); err != nil {
		logSweepPollerTickEnd(len(pending), err)
		return err
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
	txHash := depositBroadcastSignature(deposit)
	logSweepDepositProcessing(
		deposit.ID,
		deposit.GroupID,
		deposit.UserID,
		deposit.Amount,
		deposit.FromAddress,
		deposit.Status,
		txHash != "",
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

	if txHash == "" {
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
		logSweepAttempt(deposit.GroupID, deposit.UserID, deposit.ID, sweepAmount, deposit.FromAddress, treasury.Address)

		req, err := privy.BuildSweepRequest(deposit.FromAddress, treasury.Address, sweepAmount, p.relayer)
		if err != nil {
			p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, "build_sweep_request", err)
			return nil
		}
		result, err := p.privy.SubmitSweep(ctx, req)
		if err != nil {
			p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, "submit_sweep", err)
			return nil
		}
		txHash = result.TxHash
		logSweepBroadcastSubmitted(deposit.ID, deposit.GroupID, txHash, treasury.Address)

		if err := p.store.SetDepositBroadcastSignature(ctx, deposit.ID, txHash); err != nil {
			logSweepDepositFailed(deposit.ID, deposit.GroupID, "persist_broadcast_signature", err)
			return err
		}
	} else {
		logSweepDepositResuming(deposit.ID, deposit.GroupID, txHash)
	}

	return p.confirmAndObserveSweep(ctx, deposit, treasury.Address, txHash)
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
	if deposit.TxHash.Valid {
		return deposit.TxHash.String
	}
	return ""
}

func (p *SweepPoller) confirmAndObserveSweep(ctx context.Context, deposit postgres.DepositRow, treasuryAddress, txHash string) error {
	confirmed, err := p.rpc.IsConfirmed(ctx, txHash)
	logSweepConfirmationCheck(deposit.ID, txHash, confirmed, err)
	if err != nil {
		logSweepDepositFailed(deposit.ID, deposit.GroupID, "confirmation_check", err)
		return err
	}
	if !confirmed {
		logSweepDepositSkipped(deposit.ID, deposit.GroupID, "awaiting_confirmation",
			"tx_signature", txHash,
		)
		return nil
	}

	logSweepConfirm(deposit.GroupID, deposit.UserID, deposit.ID, txHash)

	_, err = p.deposits.ObserveSweep(ctx, app.ObservedSweep{
		TxHash: txHash,
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
			"tx_signature", txHash,
			"err", err,
		)
		return nil
	}

	logSweepDepositCredited(deposit.ID, deposit.GroupID, deposit.UserID, txHash)
	return nil
}
