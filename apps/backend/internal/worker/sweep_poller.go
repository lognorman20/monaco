package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
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

// Tick evaluates pending deposits once.
func (p *SweepPoller) Tick(ctx context.Context) error {
	_ = p.clock.Now()
	if err := p.scanMemberWalletDeposits(ctx); err != nil {
		return err
	}

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
	groupIDs, err := p.store.ListGroupIDs(ctx)
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
		if balance <= 0 {
			logSweepDepositSkipped(deposit.ID, deposit.GroupID, "insufficient_balance",
				"balance", balance,
				"required", deposit.Amount,
			)
			return nil
		}

		sweepAmount := balance
		if sweepAmount != deposit.Amount {
			if err := p.store.UpdatePendingDepositAmount(ctx, deposit.ID, sweepAmount); err != nil {
				logSweepDepositFailed(deposit.ID, deposit.GroupID, "update_deposit_amount", err)
				return err
			}
			slog.Info("sweep deposit amount adjusted",
				"deposit_id", deposit.ID,
				"group_id", deposit.GroupID,
				"previous_amount", deposit.Amount,
				"sweep_amount", sweepAmount,
			)
			deposit.Amount = sweepAmount
		}

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

// scanMemberWalletDeposits creates pending deposit rows when USDC lands without a prior intent.
func (p *SweepPoller) scanMemberWalletDeposits(ctx context.Context) error {
	wallets, err := p.store.ListMemberWallets(ctx)
	if err != nil {
		return err
	}

	for _, wallet := range wallets {
		hasPending, err := p.store.HasPendingDepositForFromAddress(ctx, wallet.SolanaAddress)
		if err != nil {
			return err
		}
		if hasPending {
			continue
		}

		balance, err := p.privy.MemberUSDCBalance(ctx, wallet.SolanaAddress)
		if err != nil {
			slog.Warn("deposit scan balance check failed",
				"from_address", wallet.SolanaAddress,
				"user_id", wallet.UserID,
				"err", err,
			)
			continue
		}
		if balance <= 0 {
			continue
		}

		groupID, ok, err := p.resolveDepositGroupID(ctx, wallet.UserID)
		if err != nil {
			return err
		}
		if !ok {
			slog.Info("deposit scan skipped ambiguous group membership",
				"from_address", wallet.SolanaAddress,
				"user_id", wallet.UserID,
				"balance", balance,
			)
			continue
		}

		row, err := p.store.InsertDeposit(ctx, wallet.UserID, groupID, balance, wallet.SolanaAddress)
		if err != nil {
			return err
		}
		slog.Info("deposit scan created pending deposit",
			"deposit_id", row.ID,
			"group_id", row.GroupID,
			"user_id", row.UserID,
			"from_address", row.FromAddress,
			"amount", row.Amount,
		)
	}

	return nil
}

func (p *SweepPoller) resolveDepositGroupID(ctx context.Context, userID string) (string, bool, error) {
	groupIDs, err := p.store.ListUserGroupIDs(ctx, userID)
	if err != nil {
		return "", false, err
	}
	if len(groupIDs) == 0 {
		groupIDs, err = p.store.ListGroupsCreatedByUserID(ctx, userID)
		if err != nil {
			return "", false, err
		}
	}
	if len(groupIDs) != 1 {
		return "", false, nil
	}
	return groupIDs[0], true, nil
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
