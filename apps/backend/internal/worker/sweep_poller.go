package worker

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
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
	mu  sync.Mutex
	now time.Time
}

// NewStubClock returns a fixed clock for tests.
func NewStubClock(now time.Time) *stubClock {
	return &stubClock{now: now}
}

func (c *stubClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the stub clock forward for backoff and lease tests.
func (c *stubClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// DefaultPollInterval is how often the API process polls pending deposits.
const DefaultPollInterval = 3 * time.Second

const (
	// sweepDepositTimeout bounds the Privy/RPC work for one deposit so a hung upstream call
	// costs one worker for one deposit, not the whole tick.
	sweepDepositTimeout = 20 * time.Second
	// sweepClaimLease is how long a claim survives its owner. It must exceed
	// sweepDepositTimeout: a live owner always finishes (and releases) inside its lease.
	sweepClaimLease = 2 * time.Minute
	// sweepBookkeepingTimeout bounds claim, release and retry-state writes.
	sweepBookkeepingTimeout = 10 * time.Second
	// sweepWorkers is how many deposits one tick works on at once.
	sweepWorkers = 4

	sweepRetryBase = 5 * time.Second
	sweepRetryMax  = 5 * time.Minute
	// maxSweepTransientAttempts fails a deposit that keeps erroring while nothing is
	// broadcast (about ten minutes of backoff), releasing its balance reservation.
	maxSweepTransientAttempts = 8
	// maxSweepSubmits caps re-submits of sweeps that were dropped by the network.
	maxSweepSubmits = 5

	// sweepExpiryMarginBlocks is how far the finalized height must pass a sweep's last valid
	// block height before it counts as dropped; it absorbs lag between RPC nodes.
	sweepExpiryMarginBlocks = 50
	// unknownExpiryBoundBlocks bounds a signature whose expiry height was never recorded.
	// A blockhash is valid for 150 blocks, and the one in the transaction was fetched before
	// now, so it cannot land past the current finalized height plus this bound.
	unknownExpiryBoundBlocks = 300

	// staleWaitAfter / staleWaitRecheck slow balance polling for intents nobody funded.
	staleWaitAfter   = 10 * time.Minute
	staleWaitRecheck = 30 * time.Second

	// Treasury surplus reconciliation makes one RPC call per group, so each group is checked
	// at most once per surplusReconcileEvery and a tick takes at most surplusReconcileBatch.
	surplusReconcileEvery   = time.Minute
	surplusReconcileBatch   = 5
	surplusReconcileTimeout = 20 * time.Second
)

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
	privy    privy.SweepClient
	rpc      SolanaRPC
	deposits *app.DepositService
	relayer  string
	clock    Clock
	owner    string
	// depositTimeout is sweepDepositTimeout outside tests.
	depositTimeout time.Duration
}

// NewSweepPoller wires sweep polling dependencies.
func NewSweepPoller(store *postgres.Store, privyClient privy.SweepClient, rpc SolanaRPC, deposits *app.DepositService, relayerKey string, clock Clock) *SweepPoller {
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
		owner:    newSweepClaimOwner(),

		depositTimeout: sweepDepositTimeout,
	}
}

// newSweepClaimOwner identifies this poller instance in deposits.claimed_by.
func newSweepClaimOwner() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Sprintf("%s:%d:%d", host, os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("%s:%d:%s", host, os.Getpid(), hex.EncodeToString(nonce[:]))
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

	// No tick-wide deadline: every deposit and every reconcile carries its own timeout, so
	// slow work for one deposit never cancels the deposits behind it.
	runTick := func() {
		telemetry.GuardTick(ctx, PollerSweep, func() error {
			err := poller.Tick(ctx)
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

// Tick works every due pending deposit once, then a staggered slice of surplus reconciles.
// A failing deposit is logged, backed off and skipped; only infrastructure failures
// (claiming work) surface as the tick error.
func (p *SweepPoller) Tick(ctx context.Context) error {
	handled, claimErr := p.processDueDeposits(ctx)
	surplusErr := p.reconcileTreasurySurplus(ctx)

	err := errors.Join(claimErr, surplusErr)
	logSweepPollerTickEnd(handled, err)
	return err
}

// processDueDeposits claims and processes deposits one at a time across sweepWorkers.
// Claiming singly keeps every lease fresh: a deposit is only leased once a worker is free
// to start on it.
func (p *SweepPoller) processDueDeposits(ctx context.Context) (int, error) {
	var (
		mu        sync.Mutex
		handled   []string
		claimErr  error
		panicked  any
		waitGroup sync.WaitGroup
	)

	claimNext := func() (postgres.SweepDepositRow, bool) {
		mu.Lock()
		defer mu.Unlock()
		if claimErr != nil || panicked != nil || ctx.Err() != nil {
			return postgres.SweepDepositRow{}, false
		}
		claimCtx, cancel := context.WithTimeout(ctx, sweepBookkeepingTimeout)
		defer cancel()
		row, found, err := p.store.ClaimNextSweepDeposit(claimCtx, p.owner, p.clock.Now(), sweepClaimLease, handled)
		if err != nil {
			logSweepPollerClaimFailed(err)
			claimErr = err
			return postgres.SweepDepositRow{}, false
		}
		if !found {
			return postgres.SweepDepositRow{}, false
		}
		handled = append(handled, row.ID)
		return row, true
	}

	for i := 0; i < sweepWorkers; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					slog.Error("sweep deposit worker panicked",
						"panic", fmt.Sprint(recovered),
						"stack", string(debug.Stack()),
					)
					mu.Lock()
					if panicked == nil {
						panicked = recovered
					}
					mu.Unlock()
				}
			}()
			for {
				deposit, ok := claimNext()
				if !ok {
					return
				}
				p.processClaimedDeposit(ctx, deposit)
			}
		}()
	}
	waitGroup.Wait()

	if panicked != nil {
		// Re-raise on the tick goroutine so the poller guard counts and alerts it.
		panic(panicked)
	}
	return len(handled), claimErr
}

// processClaimedDeposit runs one deposit under its own timeout and always releases the claim.
func (p *SweepPoller) processClaimedDeposit(ctx context.Context, deposit postgres.SweepDepositRow) {
	defer func() {
		releaseCtx, cancel := p.bookkeepingContext(ctx)
		defer cancel()
		if err := p.store.ReleaseSweepDeposit(releaseCtx, deposit.ID, p.owner); err != nil {
			// The lease expires on its own; the deposit is only delayed.
			logSweepDepositFailed(deposit.ID, deposit.GroupID, "release_claim", err)
		}
	}()

	depositCtx, cancel := context.WithTimeout(ctx, p.depositTimeout)
	defer cancel()

	if err := p.processPendingDeposit(depositCtx, deposit); err != nil {
		p.recordTransientFailure(ctx, deposit, err)
	}
}

// bookkeepingContext outlives a timed-out or cancelled deposit context so retry state, final
// statuses and the claim release still reach the database.
func (p *SweepPoller) bookkeepingContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), sweepBookkeepingTimeout)
}

// sweepStageError is a transient failure at one stage of a deposit's sweep.
// sweepOutstanding means a signed sweep may still land, so the deposit must not be failed.
type sweepStageError struct {
	stage            string
	sweepOutstanding bool
	err              error
}

func (e *sweepStageError) Error() string { return e.stage + ": " + e.err.Error() }

func (e *sweepStageError) Unwrap() error { return e.err }

func transientSweepError(stage string, sweepOutstanding bool, err error) error {
	return &sweepStageError{stage: stage, sweepOutstanding: sweepOutstanding, err: err}
}

// recordTransientFailure backs the deposit off. Once nothing is broadcast and the failures
// keep coming, the deposit is failed so its balance reservation is released.
func (p *SweepPoller) recordTransientFailure(ctx context.Context, deposit postgres.SweepDepositRow, err error) {
	stage := "process"
	sweepOutstanding := depositBroadcastSignature(deposit.DepositRow) != ""
	var stageErr *sweepStageError
	if errors.As(err, &stageErr) {
		stage = stageErr.stage
		sweepOutstanding = stageErr.sweepOutstanding
	}
	logSweepDepositFailed(deposit.ID, deposit.GroupID, stage, err)

	attempts := deposit.AttemptCount + 1
	if !sweepOutstanding && attempts >= maxSweepTransientAttempts {
		telemetry.MoneyEvent(telemetry.EventDepositSweep, "retries_exhausted")
		p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, stage, err)
		return
	}

	bookCtx, cancel := p.bookkeepingContext(ctx)
	defer cancel()
	nextAttemptAt := p.clock.Now().Add(sweepRetryDelay(attempts))
	if recordErr := p.store.RecordSweepAttemptFailure(bookCtx, deposit.ID, err.Error(), nextAttemptAt); recordErr != nil {
		logSweepDepositFailed(deposit.ID, deposit.GroupID, "record_attempt_failure", recordErr)
		return
	}
	logSweepDepositBackoff(deposit.ID, deposit.GroupID, stage, attempts, nextAttemptAt)
}

// sweepRetryDelay doubles from sweepRetryBase per consecutive failure, capped at sweepRetryMax.
func sweepRetryDelay(attempts int) time.Duration {
	delay := sweepRetryBase
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= sweepRetryMax {
			return sweepRetryMax
		}
	}
	return delay
}

// reconcileTreasurySurplus credits uncredited treasury USDC for the few groups that are due.
func (p *SweepPoller) reconcileTreasurySurplus(ctx context.Context) error {
	claimCtx, cancel := context.WithTimeout(ctx, sweepBookkeepingTimeout)
	groupIDs, err := p.store.ClaimTreasuriesForSurplusCheck(claimCtx, p.clock.Now(), surplusReconcileEvery, surplusReconcileBatch)
	cancel()
	if err != nil {
		return err
	}
	for _, groupID := range groupIDs {
		groupCtx, cancel := context.WithTimeout(ctx, surplusReconcileTimeout)
		_, err := p.deposits.CreditUncreditedTreasuryUSDC(groupCtx, groupID)
		cancel()
		if err != nil {
			slog.Warn("sweep treasury surplus reconcile failed",
				"group_id", groupID,
				"stage", "reconcile_surplus",
				"err", err,
			)
		}
	}
	return nil
}

func (p *SweepPoller) processPendingDeposit(ctx context.Context, deposit postgres.SweepDepositRow) error {
	txSignature := depositBroadcastSignature(deposit.DepositRow)
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
		return transientSweepError("treasury_lookup", txSignature != "", err)
	}
	if !found {
		err := fmt.Errorf("treasury not found for group %s", deposit.GroupID)
		if txSignature != "" {
			return transientSweepError("treasury_not_found", true, err)
		}
		p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, "treasury_not_found", err)
		return nil
	}

	if txSignature != "" {
		logSweepDepositResuming(deposit.ID, deposit.GroupID, txSignature)
		return p.resolveBroadcastSweep(ctx, deposit, treasury.SolanaAddress, txSignature)
	}

	balance, err := p.privy.MemberUSDCBalance(ctx, deposit.FromAddress)
	logSweepBalanceCheck(deposit.ID, deposit.FromAddress, balance, deposit.Amount, err)
	if err != nil {
		return transientSweepError("balance_check", false, err)
	}
	if balance < deposit.Amount {
		logSweepDepositSkipped(deposit.ID, deposit.GroupID, "insufficient_balance",
			"balance", balance,
			"required", deposit.Amount,
		)
		return p.deferHealthyDeposit(ctx, deposit, p.waitForFundsRecheck(deposit))
	}

	sweepAmount := deposit.Amount
	logSweepAttempt(deposit.GroupID, deposit.UserID, deposit.ID, sweepAmount, deposit.FromAddress, treasury.SolanaAddress)

	req, err := privy.BuildSweepRequest(deposit.FromAddress, treasury.SolanaAddress, sweepAmount, p.relayer)
	if err != nil {
		p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, "build_sweep_request", err)
		return nil
	}
	prepared, err := p.privy.PrepareSweep(ctx, req)
	if errors.Is(err, privy.ErrBroadcastRejected) {
		telemetry.MoneyEvent(telemetry.EventDepositSweep, telemetry.OutcomeRejected)
		p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, "submit_sweep", err)
		return nil
	}
	if err != nil {
		return transientSweepError("prepare_sweep", false, err)
	}

	// The signature goes to the database before the sweep goes to the chain. Whatever happens
	// next (crash, timeout, lost response) the deposit resumes from this signature and is
	// only swept again once the chain proves this transaction can no longer land.
	recorded, err := p.store.RecordSweepSignature(ctx, deposit.ID, p.owner, prepared.TxSignature, lastValidBlockHeight(prepared))
	if err != nil {
		return transientSweepError("persist_broadcast_signature", false, err)
	}
	if !recorded {
		logSweepDepositSkipped(deposit.ID, deposit.GroupID, "claim_lost_before_broadcast",
			"tx_signature", prepared.TxSignature,
		)
		return nil
	}
	deposit.TxSignature = sql.NullString{String: prepared.TxSignature, Valid: true}
	deposit.LastValidBlockHeight = lastValidBlockHeight(prepared)
	deposit.SweepSubmitCount++

	result, err := p.privy.BroadcastSweep(ctx, prepared)
	if errors.Is(err, privy.ErrBroadcastRejected) {
		telemetry.MoneyEvent(telemetry.EventDepositSweep, telemetry.OutcomeRejected)
		p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, "submit_sweep", err)
		return nil
	}
	if err != nil {
		// Ambiguous: the sweep may have reached the chain. The recorded signature decides.
		return transientSweepError("broadcast_sweep", true, err)
	}
	txSignature = prepared.TxSignature
	if result.TxSignature != "" && result.TxSignature != prepared.TxSignature {
		slog.Error("sweep broadcast signature differs from prepared signature",
			"deposit_id", deposit.ID,
			"group_id", deposit.GroupID,
			"prepared_tx_signature", prepared.TxSignature,
			"broadcast_tx_signature", result.TxSignature,
		)
		replaceCtx, cancel := p.bookkeepingContext(ctx)
		err := p.store.ReplaceSweepSignature(replaceCtx, deposit.ID, prepared.TxSignature, result.TxSignature)
		cancel()
		if err != nil {
			return transientSweepError("persist_broadcast_signature", true, err)
		}
		txSignature = result.TxSignature
		deposit.TxSignature = sql.NullString{String: result.TxSignature, Valid: true}
		deposit.LastValidBlockHeight = sql.NullInt64{}
	}
	logSweepBroadcastSubmitted(deposit.ID, deposit.GroupID, txSignature, treasury.SolanaAddress)

	return p.resolveBroadcastSweep(ctx, deposit, treasury.SolanaAddress, txSignature)
}

func lastValidBlockHeight(prepared privy.PreparedSweep) sql.NullInt64 {
	if prepared.LastValidBlockHeight == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(prepared.LastValidBlockHeight), Valid: true}
}

// waitForFundsRecheck polls fresh intents every tick and abandoned ones every staleWaitRecheck.
func (p *SweepPoller) waitForFundsRecheck(deposit postgres.SweepDepositRow) sql.NullTime {
	now := p.clock.Now()
	if now.Sub(deposit.CreatedAt) < staleWaitAfter {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: now.Add(staleWaitRecheck), Valid: true}
}

// deferHealthyDeposit schedules a deposit that is simply waiting and clears its failure streak.
func (p *SweepPoller) deferHealthyDeposit(ctx context.Context, deposit postgres.SweepDepositRow, nextAttemptAt sql.NullTime) error {
	if deposit.AttemptCount == 0 && !nextAttemptAt.Valid {
		return nil
	}
	if err := p.store.DeferSweepDeposit(ctx, deposit.ID, nextAttemptAt); err != nil {
		return transientSweepError("defer_deposit", depositBroadcastSignature(deposit.DepositRow) != "", err)
	}
	return nil
}

// markDepositSweepFailed is a final status: it is written even if the deposit context expired.
func (p *SweepPoller) markDepositSweepFailed(ctx context.Context, depositID, groupID, stage string, err error) {
	logSweepDepositFailed(depositID, groupID, stage, err)
	failCtx, cancel := p.bookkeepingContext(ctx)
	defer cancel()
	if _, ok, failErr := p.store.FailDeposit(failCtx, depositID, stage); failErr != nil {
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

// resolveBroadcastSweep settles a deposit whose sweep signature is recorded:
// landed -> credit, failed on chain -> fail the deposit, dropped -> allow a re-submit.
func (p *SweepPoller) resolveBroadcastSweep(ctx context.Context, deposit postgres.SweepDepositRow, treasuryAddress, txSignature string) error {
	status, err := p.rpc.SignatureStatus(ctx, txSignature)
	logSweepConfirmationCheck(deposit.ID, txSignature, status.State == SignatureConfirmed, err)
	if err != nil {
		return transientSweepError("confirmation_check", true, err)
	}

	switch status.State {
	case SignatureConfirmed:
		return p.observeConfirmedSweep(ctx, deposit, treasuryAddress, txSignature)
	case SignatureFailed:
		// Final and moved no funds: release the reservation and surface it to the member.
		telemetry.MoneyEvent(telemetry.EventDepositSweep, "failed_on_chain")
		p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, "sweep_failed_on_chain",
			fmt.Errorf("sweep %s failed on chain: %s", txSignature, status.Err))
		return nil
	case SignatureNotFound:
		return p.resolveMissingSweep(ctx, deposit, txSignature)
	default:
		logSweepDepositSkipped(deposit.ID, deposit.GroupID, "awaiting_confirmation",
			"tx_signature", txSignature,
		)
		return p.deferHealthyDeposit(ctx, deposit, sql.NullTime{})
	}
}

// resolveMissingSweep decides whether an unknown signature is still in flight or was dropped.
// Dropped means the finalized chain is past the transaction's last valid block height and
// the signature is still unknown: it can never land, so sweeping again cannot double-spend.
func (p *SweepPoller) resolveMissingSweep(ctx context.Context, deposit postgres.SweepDepositRow, txSignature string) error {
	finalizedHeight, err := p.rpc.FinalizedBlockHeight(ctx)
	if err != nil {
		return transientSweepError("block_height", true, err)
	}

	if !deposit.LastValidBlockHeight.Valid {
		bound := int64(finalizedHeight) + unknownExpiryBoundBlocks
		if err := p.store.SetSweepLastValidBlockHeight(ctx, deposit.ID, txSignature, bound); err != nil {
			return transientSweepError("persist_expiry_bound", true, err)
		}
		logSweepDepositSkipped(deposit.ID, deposit.GroupID, "awaiting_confirmation",
			"tx_signature", txSignature,
			"expiry_bound_block_height", bound,
		)
		return nil
	}

	expiresAfter := uint64(deposit.LastValidBlockHeight.Int64) + sweepExpiryMarginBlocks
	if finalizedHeight <= expiresAfter {
		logSweepDepositSkipped(deposit.ID, deposit.GroupID, "awaiting_confirmation",
			"tx_signature", txSignature,
			"finalized_block_height", finalizedHeight,
			"last_valid_block_height", deposit.LastValidBlockHeight.Int64,
		)
		return p.deferHealthyDeposit(ctx, deposit, sql.NullTime{})
	}

	// The height check and the first status check are separate calls; look once more now
	// that every block the sweep could be in is finalized.
	status, err := p.rpc.SignatureStatus(ctx, txSignature)
	if err != nil {
		return transientSweepError("confirmation_check", true, err)
	}
	if status.State != SignatureNotFound {
		logSweepDepositSkipped(deposit.ID, deposit.GroupID, "landed_at_expiry",
			"tx_signature", txSignature,
			"state", string(status.State),
		)
		return nil
	}

	if deposit.SweepSubmitCount >= maxSweepSubmits {
		telemetry.MoneyEvent(telemetry.EventDepositSweep, "expired")
		p.markDepositSweepFailed(ctx, deposit.ID, deposit.GroupID, "sweep_expired",
			fmt.Errorf("sweep %s dropped after %d submits", txSignature, deposit.SweepSubmitCount))
		return nil
	}

	cleared, err := p.store.ClearDroppedSweepSignature(ctx, deposit.ID, txSignature)
	if err != nil {
		return transientSweepError("clear_dropped_signature", true, err)
	}
	telemetry.MoneyEvent(telemetry.EventDepositSweep, "dropped")
	logSweepDropped(deposit.ID, deposit.GroupID, txSignature, finalizedHeight, deposit.LastValidBlockHeight.Int64, cleared)
	return nil
}

func (p *SweepPoller) observeConfirmedSweep(ctx context.Context, deposit postgres.SweepDepositRow, treasuryAddress, txSignature string) error {
	logSweepConfirm(deposit.GroupID, deposit.UserID, deposit.ID, txSignature)

	_, err := p.deposits.ObserveSweep(ctx, app.ObservedSweep{
		TxSignature: txSignature,
		FromAddress: deposit.FromAddress,
		ToAddress:   treasuryAddress,
		Amount:      deposit.Amount,
		DepositID:   deposit.ID,
		UserID:      deposit.UserID,
		GroupID:     deposit.GroupID,
	})
	if err != nil {
		return transientSweepError("observe_sweep", true, err)
	}

	logSweepDepositCredited(deposit.ID, deposit.GroupID, deposit.UserID, txSignature)
	return nil
}
