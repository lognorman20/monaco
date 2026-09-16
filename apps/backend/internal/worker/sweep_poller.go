package worker

import (
	"context"
	"fmt"
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

// Tick evaluates pending deposits once.
func (p *SweepPoller) Tick(ctx context.Context) error {
	_ = p.clock.Now()
	pending, err := p.store.ListPendingDeposits(ctx)
	if err != nil {
		return err
	}

	for _, deposit := range pending {
		balance, err := p.privy.MemberUSDCBalance(ctx, deposit.FromAddress)
		if err != nil {
			return err
		}
		if balance < deposit.Amount {
			continue
		}

		treasury, found, err := p.store.GetTreasuryByGroupID(ctx, deposit.GroupID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("treasury not found for group %s", deposit.GroupID)
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

		confirmed, err := p.rpc.IsConfirmed(ctx, result.TxSignature)
		if err != nil {
			return err
		}
		if !confirmed {
			continue
		}

		logSweepConfirm(deposit.GroupID, deposit.UserID, deposit.ID, result.TxSignature)

		_, err = p.deposits.ObserveSweep(ctx, app.ObservedSweep{
			TxSignature: result.TxSignature,
			FromAddress: deposit.FromAddress,
			ToAddress:   treasury.SolanaAddress,
			Amount:      deposit.Amount,
			DepositID:   deposit.ID,
			UserID:      deposit.UserID,
			GroupID:     deposit.GroupID,
		})
		if err != nil {
			return err
		}
	}
	return nil
}
