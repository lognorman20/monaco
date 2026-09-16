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

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

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
	pending, err := p.store.ListPendingDeposits(ctx)
	if err != nil {
		return err
	}

	for _, deposit := range pending {
		if err := p.processPendingDeposit(ctx, deposit); err != nil {
			return err
		}
	}
	return nil
}

func (p *SweepPoller) processPendingDeposit(ctx context.Context, deposit postgres.DepositRow) error {
	treasury, found, err := p.store.GetTreasuryByGroupID(ctx, deposit.GroupID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("treasury not found for group %s", deposit.GroupID)
	}

	txSignature := depositBroadcastSignature(deposit)
	if txSignature == "" {
		balance, err := p.privy.MemberUSDCBalance(ctx, deposit.FromAddress)
		if err != nil {
			return err
		}
		if balance < deposit.Amount {
			return nil
		}

		logSweepAttempt(deposit.GroupID, deposit.UserID, deposit.ID, deposit.Amount)

		req, err := privy.BuildSweepRequest(deposit.FromAddress, treasury.SolanaAddress, deposit.Amount, p.relayer)
		if err != nil {
			return err
		}
		result, err := p.privy.SubmitSweep(ctx, req)
		if err != nil {
			return err
		}
		txSignature = result.TxSignature

		if err := p.store.SetDepositBroadcastSignature(ctx, deposit.ID, txSignature); err != nil {
			return err
		}
	}

	return p.confirmAndObserveSweep(ctx, deposit, treasury.SolanaAddress, txSignature)
}

func depositBroadcastSignature(deposit postgres.DepositRow) string {
	if deposit.TxSignature.Valid {
		return deposit.TxSignature.String
	}
	return ""
}

func (p *SweepPoller) confirmAndObserveSweep(ctx context.Context, deposit postgres.DepositRow, treasuryAddress, txSignature string) error {
	confirmed, err := p.rpc.IsConfirmed(ctx, txSignature)
	if err != nil {
		return err
	}
	if !confirmed {
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
	return err
}
