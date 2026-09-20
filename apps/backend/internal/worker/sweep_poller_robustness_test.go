package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// scriptedSweepClient wraps the fake Privy client so a test can hang, fail or drop one step.
// Each hook returns handled=false to fall through to the fake.
type scriptedSweepClient struct {
	inner     privy.SweepClient
	balance   func(ctx context.Context, address string) (int64, bool, error)
	prepare   func(ctx context.Context, req privy.SweepRequest) (privy.PreparedSweep, bool, error)
	broadcast func(ctx context.Context, prepared privy.PreparedSweep) (privy.SweepResult, bool, error)

	mu         sync.Mutex
	prepares   int
	broadcasts int
}

func (c *scriptedSweepClient) MemberUSDCBalance(ctx context.Context, address string) (int64, error) {
	if c.balance != nil {
		if balance, handled, err := c.balance(ctx, address); handled {
			return balance, err
		}
	}
	return c.inner.MemberUSDCBalance(ctx, address)
}

func (c *scriptedSweepClient) PrepareSweep(ctx context.Context, req privy.SweepRequest) (privy.PreparedSweep, error) {
	c.mu.Lock()
	c.prepares++
	c.mu.Unlock()
	if c.prepare != nil {
		if prepared, handled, err := c.prepare(ctx, req); handled {
			return prepared, err
		}
	}
	return c.inner.PrepareSweep(ctx, req)
}

func (c *scriptedSweepClient) BroadcastSweep(ctx context.Context, prepared privy.PreparedSweep) (privy.SweepResult, error) {
	c.mu.Lock()
	c.broadcasts++
	c.mu.Unlock()
	if c.broadcast != nil {
		if result, handled, err := c.broadcast(ctx, prepared); handled {
			return result, err
		}
	}
	return c.inner.BroadcastSweep(ctx, prepared)
}

func (c *scriptedSweepClient) counts() (prepares, broadcasts int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prepares, c.broadcasts
}

type sweepRetryState struct {
	attemptCount    int
	submitCount     int
	nextAttemptAt   sql.NullTime
	lastError       sql.NullString
	lastValidHeight sql.NullInt64
	claimedBy       sql.NullString
	status          string
	txSignature     sql.NullString
}

func readSweepRetryState(t *testing.T, db *sql.DB, depositID string) sweepRetryState {
	t.Helper()
	var state sweepRetryState
	err := db.QueryRowContext(context.Background(), `
SELECT attempt_count, sweep_submit_count, next_attempt_at, last_error,
       sweep_last_valid_block_height, claimed_by, status, tx_signature
FROM deposits WHERE id = $1`, depositID).Scan(
		&state.attemptCount,
		&state.submitCount,
		&state.nextAttemptAt,
		&state.lastError,
		&state.lastValidHeight,
		&state.claimedBy,
		&state.status,
		&state.txSignature,
	)
	if err != nil {
		t.Fatalf("read sweep retry state: %v", err)
	}
	return state
}

func newScriptedPoller(t *testing.T, testApp *workerTestApp, rpc SolanaRPC, clock Clock) (*SweepPoller, *scriptedSweepClient) {
	t.Helper()
	client := &scriptedSweepClient{inner: sweepClient(t, testApp.Privy)}
	return NewSweepPoller(testApp.Store, client, rpc, testApp.Deposits, "relayer-key", clock), client
}

func mustTick(t *testing.T, poller *SweepPoller) {
	t.Helper()
	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
}

func mustPendingReservation(t *testing.T, testApp *workerTestApp, userID string) int64 {
	t.Helper()
	sum, err := testApp.Store.SumPendingDepositAmountByUserID(context.Background(), userID)
	if err != nil {
		t.Fatalf("SumPendingDepositAmountByUserID: %v", err)
	}
	return sum
}

func TestSweepPoller_sweepFailedOnChain_failsDepositAndDoesNotStarveLaterDeposits(t *testing.T) {
	// Arrange: the OLDEST pending deposit carries a sweep that landed and failed on chain.
	poller, testApp, rpc := setupPoller(t)
	bad, _, _ := seedPendingDepositAs(t, testApp, "bad")
	good, goodAddress, _ := seedPendingDepositAs(t, testApp, "good")
	mustExec(t, testApp.DB, `UPDATE deposits SET tx_signature = $2 WHERE id = $1`, bad.ID, "FAILEDSIG-"+testApp.ISO.Suffix())
	rpc.FailOnChain("FAILEDSIG-"+testApp.ISO.Suffix(), "InstructionError[1, Custom(1)]")
	privy.SetMemberUSDCBalance(testApp.Privy, goodAddress, 2_000_000)

	// Act
	mustTick(t, poller)

	// Assert: bad deposit failed and released its reservation; the later deposit was swept.
	badState := readSweepRetryState(t, testApp.DB, bad.ID)
	if badState.status != "failed: sweep_failed_on_chain" {
		t.Fatalf("bad deposit status = %q, want failed: sweep_failed_on_chain", badState.status)
	}
	if reserved := mustPendingReservation(t, testApp, bad.UserID); reserved != 0 {
		t.Fatalf("reservation after on-chain failure = %d, want 0", reserved)
	}
	goodState := readSweepRetryState(t, testApp.DB, good.ID)
	if !goodState.txSignature.Valid || goodState.status != "pending" {
		t.Fatalf("later deposit = %+v, want pending with a broadcast signature", goodState)
	}
	if got := privy.SweepCount(testApp.Privy); got != 1 {
		t.Fatalf("sweep count = %d, want 1", got)
	}
}

func TestSweepPoller_rpcError_backsOffThatDepositAndStillProcessesOthers(t *testing.T) {
	// Arrange: oldest deposit resumes a signature; RPC is down.
	testApp := integrationWorkerApp(t)
	rpc := NewFakeSolanaRPC()
	clock := NewStubClock(testApp.Now)
	poller, client := newScriptedPoller(t, testApp, rpc, clock)
	stuck, _, _ := seedPendingDepositAs(t, testApp, "stuck")
	waiting, waitingAddress, _ := seedPendingDepositAs(t, testApp, "waiting")
	mustExec(t, testApp.DB, `UPDATE deposits SET tx_signature = $2 WHERE id = $1`, stuck.ID, "STUCKSIG-"+testApp.ISO.Suffix())
	privy.SetMemberUSDCBalance(testApp.Privy, waitingAddress, 2_000_000)
	rpc.SetErrors(errors.New("solana rpc status 503"), nil)

	// Act
	mustTick(t, poller)

	// Assert: tick did not abort; the other deposit was still broadcast.
	stuckState := readSweepRetryState(t, testApp.DB, stuck.ID)
	if stuckState.status != "pending" || stuckState.attemptCount != 1 || !stuckState.nextAttemptAt.Valid {
		t.Fatalf("stuck deposit = %+v, want pending, attempt 1, backoff scheduled", stuckState)
	}
	if want := testApp.Now.Add(sweepRetryBase); !stuckState.nextAttemptAt.Time.Equal(want) {
		t.Fatalf("next_attempt_at = %s, want %s", stuckState.nextAttemptAt.Time.UTC(), want)
	}
	if !strings.Contains(stuckState.lastError.String, "503") {
		t.Fatalf("last_error = %q, want rpc error recorded", stuckState.lastError.String)
	}
	if stuckState.claimedBy.Valid {
		t.Fatalf("claim not released: claimed_by = %q", stuckState.claimedBy.String)
	}
	if state := readSweepRetryState(t, testApp.DB, waiting.ID); !state.txSignature.Valid {
		t.Fatal("deposit behind the failing one was not swept")
	}
	if _, broadcasts := client.counts(); broadcasts != 1 {
		t.Fatalf("broadcasts = %d, want 1", broadcasts)
	}

	// Act: a tick inside the backoff window must not touch the backed-off deposit.
	rpc.SetErrors(nil, nil)
	rpc.Confirm("STUCKSIG-" + testApp.ISO.Suffix())
	mustTick(t, poller)
	if state := readSweepRetryState(t, testApp.DB, stuck.ID); state.status != "pending" {
		t.Fatalf("status inside backoff = %q, want pending (not retried yet)", state.status)
	}

	// Act: after the backoff the deposit is retried and credited.
	clock.Advance(sweepRetryBase + time.Second)
	mustTick(t, poller)
	if state := readSweepRetryState(t, testApp.DB, stuck.ID); state.status != "confirmed" {
		t.Fatalf("status after backoff = %q, want confirmed", state.status)
	}
}

func TestSweepRetryDelay_doublesAndCaps(t *testing.T) {
	cases := map[int]time.Duration{
		1:  5 * time.Second,
		2:  10 * time.Second,
		3:  20 * time.Second,
		7:  5 * time.Minute,
		50: 5 * time.Minute,
	}
	for attempts, want := range cases {
		if got := sweepRetryDelay(attempts); got != want {
			t.Errorf("sweepRetryDelay(%d) = %s, want %s", attempts, got, want)
		}
	}
}

func TestSweepPoller_prepareKeepsFailing_failsDepositAndReleasesReservation(t *testing.T) {
	// Arrange: nothing was ever broadcast and the last allowed attempt fails too.
	testApp := integrationWorkerApp(t)
	poller, client := newScriptedPoller(t, testApp, NewFakeSolanaRPC(), NewStubClock(testApp.Now))
	deposit, address, _ := seedPendingDeposit(t, testApp)
	privy.SetMemberUSDCBalance(testApp.Privy, address, 2_000_000)
	client.prepare = func(context.Context, privy.SweepRequest) (privy.PreparedSweep, bool, error) {
		return privy.PreparedSweep{}, true, fmt.Errorf("%w: blockhash rpc timeout", privy.ErrAPI)
	}
	mustExec(t, testApp.DB, `UPDATE deposits SET attempt_count = $2 WHERE id = $1`, deposit.ID, maxSweepTransientAttempts-1)

	// Act
	mustTick(t, poller)

	// Assert
	state := readSweepRetryState(t, testApp.DB, deposit.ID)
	if state.status != "failed: prepare_sweep" {
		t.Fatalf("status = %q, want failed: prepare_sweep", state.status)
	}
	if reserved := mustPendingReservation(t, testApp, deposit.UserID); reserved != 0 {
		t.Fatalf("reservation = %d, want 0", reserved)
	}
	if _, broadcasts := client.counts(); broadcasts != 0 {
		t.Fatalf("broadcasts = %d, want 0", broadcasts)
	}
}

func TestSweepPoller_signatureIsPersistedBeforeBroadcast(t *testing.T) {
	// Arrange
	testApp := integrationWorkerApp(t)
	poller, client := newScriptedPoller(t, testApp, NewFakeSolanaRPC(), NewStubClock(testApp.Now))
	deposit, address, _ := seedPendingDeposit(t, testApp)
	privy.SetMemberUSDCBalance(testApp.Privy, address, 2_000_000)
	privy.SetSweepLastValidBlockHeight(testApp.Privy, 1_000)

	var atBroadcast sweepRetryState
	client.broadcast = func(_ context.Context, prepared privy.PreparedSweep) (privy.SweepResult, bool, error) {
		atBroadcast = readSweepRetryState(t, testApp.DB, deposit.ID)
		if atBroadcast.txSignature.String != prepared.TxSignature {
			t.Errorf("tx_signature at broadcast = %q, want %q", atBroadcast.txSignature.String, prepared.TxSignature)
		}
		return privy.SweepResult{}, false, nil
	}

	// Act
	mustTick(t, poller)

	// Assert
	if !atBroadcast.txSignature.Valid {
		t.Fatal("signature was not in the database when the sweep was broadcast")
	}
	if atBroadcast.lastValidHeight.Int64 != 1_000 || atBroadcast.submitCount != 1 {
		t.Fatalf("state at broadcast = %+v, want last valid height 1000 and submit count 1", atBroadcast)
	}
}

func TestSweepPoller_crashAfterBroadcast_restartCreditsWithoutSecondSweep(t *testing.T) {
	// Arrange: the sweep reaches the chain but the process dies before it sees the response.
	testApp := integrationWorkerApp(t)
	rpc := NewFakeSolanaRPC()
	crashing, crashingClient := newScriptedPoller(t, testApp, rpc, NewStubClock(testApp.Now))
	deposit, address, _ := seedPendingDeposit(t, testApp)
	privy.SetMemberUSDCBalance(testApp.Privy, address, 2_000_000)
	crashingClient.broadcast = func(ctx context.Context, prepared privy.PreparedSweep) (privy.SweepResult, bool, error) {
		if _, err := crashingClient.inner.BroadcastSweep(ctx, prepared); err != nil {
			t.Errorf("inner BroadcastSweep: %v", err)
		}
		return privy.SweepResult{}, true, context.DeadlineExceeded
	}
	mustTick(t, crashing)

	state := readSweepRetryState(t, testApp.DB, deposit.ID)
	if state.status != "pending" || !state.txSignature.Valid {
		t.Fatalf("after ambiguous broadcast = %+v, want pending with signature kept", state)
	}

	// Act: a fresh process picks the deposit up once its backoff passes.
	clock := NewStubClock(testApp.Now.Add(time.Minute))
	restarted, restartedClient := newScriptedPoller(t, testApp, rpc, clock)
	mustTick(t, restarted)
	rpc.Confirm(state.txSignature.String)
	mustTick(t, restarted)

	// Assert
	if prepares, broadcasts := restartedClient.counts(); prepares != 0 || broadcasts != 0 {
		t.Fatalf("restart prepared %d / broadcast %d sweeps, want 0 / 0", prepares, broadcasts)
	}
	if got := privy.SweepCount(testApp.Privy); got != 1 {
		t.Fatalf("sweeps on chain = %d, want exactly 1", got)
	}
	if state := readSweepRetryState(t, testApp.DB, deposit.ID); state.status != "confirmed" {
		t.Fatalf("status = %q, want confirmed", state.status)
	}
	position, found, err := testApp.Store.GetPosition(context.Background(), deposit.UserID, deposit.GroupID)
	if err != nil || !found {
		t.Fatalf("GetPosition: found=%v err=%v", found, err)
	}
	if position.ShareUnits != deposit.Amount {
		t.Fatalf("share_units = %d, want %d (credited once)", position.ShareUnits, deposit.Amount)
	}
}

func TestSweepPoller_crashBeforeBroadcast_resubmitsOnlyAfterBlockhashExpires(t *testing.T) {
	// Arrange: signature persisted, process dies, nothing ever reaches the chain.
	testApp := integrationWorkerApp(t)
	rpc := NewFakeSolanaRPC()
	rpc.SetBlockHeight(900)
	privy.SetSweepLastValidBlockHeight(testApp.Privy, 1_000)
	crashing, crashingClient := newScriptedPoller(t, testApp, rpc, NewStubClock(testApp.Now))
	deposit, address, _ := seedPendingDeposit(t, testApp)
	privy.SetMemberUSDCBalance(testApp.Privy, address, 2_000_000)
	crashingClient.broadcast = func(context.Context, privy.PreparedSweep) (privy.SweepResult, bool, error) {
		return privy.SweepResult{}, true, errors.New("connection reset by peer")
	}
	mustTick(t, crashing)
	first := readSweepRetryState(t, testApp.DB, deposit.ID)
	if !first.txSignature.Valid {
		t.Fatal("expected signature persisted before the failed broadcast")
	}

	clock := NewStubClock(testApp.Now.Add(time.Minute))
	restarted, restartedClient := newScriptedPoller(t, testApp, rpc, clock)

	// Act + Assert: blockhash still valid -> never re-submit, even though the chain has no trace.
	mustTick(t, restarted)
	rpc.SetBlockHeight(1_000 + sweepExpiryMarginBlocks)
	mustTick(t, restarted)
	if prepares, _ := restartedClient.counts(); prepares != 0 {
		t.Fatalf("re-submitted %d times while the first sweep could still land", prepares)
	}
	if state := readSweepRetryState(t, testApp.DB, deposit.ID); state.txSignature.String != first.txSignature.String {
		t.Fatalf("signature changed before expiry: %q -> %q", first.txSignature.String, state.txSignature.String)
	}

	// Act + Assert: finalized chain is past the last valid height -> dropped, then one re-submit.
	rpc.SetBlockHeight(1_000 + sweepExpiryMarginBlocks + 1)
	mustTick(t, restarted)
	if state := readSweepRetryState(t, testApp.DB, deposit.ID); state.txSignature.Valid || state.status != "pending" {
		t.Fatalf("after expiry = %+v, want pending with signature cleared", state)
	}
	privy.SetSweepLastValidBlockHeight(testApp.Privy, 2_000)
	mustTick(t, restarted)
	second := readSweepRetryState(t, testApp.DB, deposit.ID)
	if !second.txSignature.Valid || second.txSignature.String == first.txSignature.String {
		t.Fatalf("second signature = %q, want a new one (first %q)", second.txSignature.String, first.txSignature.String)
	}
	if second.submitCount != 2 || second.lastValidHeight.Int64 != 2_000 {
		t.Fatalf("state after re-submit = %+v, want submit count 2 and height 2000", second)
	}
	if got := privy.SweepCount(testApp.Privy); got != 1 {
		t.Fatalf("sweeps on chain = %d, want exactly 1", got)
	}
}

func TestSweepPoller_sweepLandsAtExpiry_isNotResubmitted(t *testing.T) {
	// Arrange: the first status read misses the sweep, the re-check after expiry finds it.
	testApp := integrationWorkerApp(t)
	inner := NewFakeSolanaRPC()
	inner.SetBlockHeight(5_000)
	rpc := &lateStatusRPC{fakeSolanaRPC: inner}
	poller, client := newScriptedPoller(t, testApp, rpc, NewStubClock(testApp.Now))
	deposit, _, _ := seedPendingDeposit(t, testApp)
	sig := "LATESIG-" + testApp.ISO.Suffix()
	mustExec(t, testApp.DB, `UPDATE deposits SET tx_signature = $2, sweep_last_valid_block_height = 1000, sweep_submit_count = 1 WHERE id = $1`, deposit.ID, sig)
	inner.Confirm(sig)

	// Act
	mustTick(t, poller)

	// Assert
	state := readSweepRetryState(t, testApp.DB, deposit.ID)
	if state.txSignature.String != sig || state.status != "pending" {
		t.Fatalf("state = %+v, want signature kept", state)
	}
	if prepares, _ := client.counts(); prepares != 0 {
		t.Fatalf("prepares = %d, want 0", prepares)
	}
}

// lateStatusRPC reports not-found on the first status read of each signature.
type lateStatusRPC struct {
	*fakeSolanaRPC
	seenMu sync.Mutex
	seen   map[string]bool
}

func (r *lateStatusRPC) SignatureStatus(ctx context.Context, sig string) (SignatureStatus, error) {
	r.seenMu.Lock()
	if r.seen == nil {
		r.seen = make(map[string]bool)
	}
	first := !r.seen[sig]
	r.seen[sig] = true
	r.seenMu.Unlock()
	if first {
		return SignatureStatus{State: SignatureNotFound}, nil
	}
	return r.fakeSolanaRPC.SignatureStatus(ctx, sig)
}

func TestSweepPoller_droppedTooManyTimes_failsDepositAndReleasesReservation(t *testing.T) {
	// Arrange
	poller, testApp, rpc := setupPoller(t)
	deposit, _, _ := seedPendingDeposit(t, testApp)
	sig := "DROPPEDSIG-" + testApp.ISO.Suffix()
	mustExec(t, testApp.DB, `UPDATE deposits SET tx_signature = $2, sweep_last_valid_block_height = 1000, sweep_submit_count = $3 WHERE id = $1`,
		deposit.ID, sig, maxSweepSubmits)
	rpc.SetBlockHeight(5_000)

	// Act
	mustTick(t, poller)

	// Assert
	if state := readSweepRetryState(t, testApp.DB, deposit.ID); state.status != "failed: sweep_expired" {
		t.Fatalf("status = %q, want failed: sweep_expired", state.status)
	}
	if reserved := mustPendingReservation(t, testApp, deposit.UserID); reserved != 0 {
		t.Fatalf("reservation = %d, want 0", reserved)
	}
}

func TestSweepPoller_signatureWithoutExpiryHeight_isBoundedBeforeItCanExpire(t *testing.T) {
	// Arrange: a row broadcast before expiry heights were recorded.
	poller, testApp, rpc := setupPoller(t)
	deposit, _, _ := seedPendingDeposit(t, testApp)
	sig := "LEGACYSIG-" + testApp.ISO.Suffix()
	mustExec(t, testApp.DB, `UPDATE deposits SET tx_signature = $2, sweep_submit_count = 1 WHERE id = $1`, deposit.ID, sig)
	rpc.SetBlockHeight(10_000)

	// Act
	mustTick(t, poller)

	// Assert: bounded from the current height, not dropped on sight.
	state := readSweepRetryState(t, testApp.DB, deposit.ID)
	if state.txSignature.String != sig {
		t.Fatalf("signature = %q, want kept", state.txSignature.String)
	}
	if state.lastValidHeight.Int64 != 10_000+unknownExpiryBoundBlocks {
		t.Fatalf("expiry bound = %d, want %d", state.lastValidHeight.Int64, 10_000+unknownExpiryBoundBlocks)
	}
}

func TestSweepPoller_broadcastReturnsDifferentSignature_tracksTheBroadcastOne(t *testing.T) {
	// Arrange
	testApp := integrationWorkerApp(t)
	rpc := NewFakeSolanaRPC()
	poller, client := newScriptedPoller(t, testApp, rpc, NewStubClock(testApp.Now))
	deposit, address, _ := seedPendingDeposit(t, testApp)
	privy.SetMemberUSDCBalance(testApp.Privy, address, 2_000_000)
	privy.SetSweepLastValidBlockHeight(testApp.Privy, 1_000)
	rpc.SetBlockHeight(7_000)
	resigned := "RESIGNED-" + testApp.ISO.Suffix()
	client.broadcast = func(ctx context.Context, prepared privy.PreparedSweep) (privy.SweepResult, bool, error) {
		if _, err := client.inner.BroadcastSweep(ctx, prepared); err != nil {
			t.Errorf("inner BroadcastSweep: %v", err)
		}
		return privy.SweepResult{TxSignature: resigned}, true, nil
	}

	// Act
	mustTick(t, poller)

	// Assert: the recorded expiry height described the replaced transaction, so the new
	// signature is bounded from the current chain height instead of inheriting it.
	state := readSweepRetryState(t, testApp.DB, deposit.ID)
	if state.txSignature.String != resigned {
		t.Fatalf("signature = %q, want %q", state.txSignature.String, resigned)
	}
	if want := int64(7_000 + unknownExpiryBoundBlocks); state.lastValidHeight.Int64 != want {
		t.Fatalf("expiry bound = %d, want %d (not the replaced sweep's 1000)", state.lastValidHeight.Int64, want)
	}
}

func TestSweepPoller_hungDeposit_timesOutAloneAndOthersProceed(t *testing.T) {
	// Arrange: the oldest deposit's balance call hangs until its context ends.
	testApp := integrationWorkerApp(t)
	poller, client := newScriptedPoller(t, testApp, NewFakeSolanaRPC(), NewStubClock(testApp.Now))
	poller.depositTimeout = 200 * time.Millisecond
	hung, hungAddress, _ := seedPendingDepositAs(t, testApp, "hung")
	healthy, healthyAddress, _ := seedPendingDepositAs(t, testApp, "healthy")
	privy.SetMemberUSDCBalance(testApp.Privy, healthyAddress, 2_000_000)
	client.balance = func(ctx context.Context, address string) (int64, bool, error) {
		if address != hungAddress {
			return 0, false, nil
		}
		<-ctx.Done()
		return 0, true, ctx.Err()
	}

	// Act
	started := time.Now()
	mustTick(t, poller)
	elapsed := time.Since(started)

	// Assert
	if elapsed > 5*time.Second {
		t.Fatalf("tick took %s; hung deposit was not bounded by its own timeout", elapsed)
	}
	hungState := readSweepRetryState(t, testApp.DB, hung.ID)
	if hungState.status != "pending" || hungState.attemptCount != 1 || hungState.claimedBy.Valid {
		t.Fatalf("hung deposit = %+v, want pending, attempt 1, claim released", hungState)
	}
	if state := readSweepRetryState(t, testApp.DB, healthy.ID); !state.txSignature.Valid {
		t.Fatal("healthy deposit was not swept while another deposit hung")
	}
}

func TestSweepPoller_twoPollersConcurrently_sweepAndCreditEachDepositOnce(t *testing.T) {
	// Arrange: two API processes share one database, Privy app and RPC.
	testApp := integrationWorkerApp(t)
	rpc := NewFakeSolanaRPC()
	clock := NewStubClock(testApp.Now)
	slowBroadcast := func(client *scriptedSweepClient) {
		client.broadcast = func(context.Context, privy.PreparedSweep) (privy.SweepResult, bool, error) {
			time.Sleep(20 * time.Millisecond) // widen the window a second poller would race into
			return privy.SweepResult{}, false, nil
		}
	}
	pollerA, clientA := newScriptedPoller(t, testApp, rpc, clock)
	pollerB, clientB := newScriptedPoller(t, testApp, rpc, clock)
	slowBroadcast(clientA)
	slowBroadcast(clientB)

	const depositCount = 8
	deposits := make([]postgres.DepositRow, 0, depositCount)
	for i := 0; i < depositCount; i++ {
		deposit, address, _ := seedPendingDepositAs(t, testApp, fmt.Sprintf("member-%d", i))
		privy.SetMemberUSDCBalance(testApp.Privy, address, 2_000_000)
		deposits = append(deposits, deposit)
	}

	tickBoth := func() {
		t.Helper()
		var wg sync.WaitGroup
		for _, poller := range []*SweepPoller{pollerA, pollerB} {
			wg.Add(1)
			go func(poller *SweepPoller) {
				defer wg.Done()
				if err := poller.Tick(context.Background()); err != nil {
					t.Errorf("Tick: %v", err)
				}
			}(poller)
		}
		wg.Wait()
	}

	// Act: race the broadcast phase, then the confirm/credit phase, several rounds each.
	for round := 0; round < 3; round++ {
		tickBoth()
	}
	for _, deposit := range deposits {
		state := readSweepRetryState(t, testApp.DB, deposit.ID)
		if !state.txSignature.Valid {
			t.Fatalf("deposit %s has no sweep signature", deposit.ID)
		}
		rpc.Confirm(state.txSignature.String)
	}
	for round := 0; round < 3; round++ {
		tickBoth()
	}

	// Assert
	if got := privy.SweepCount(testApp.Privy); got != depositCount {
		t.Fatalf("sweeps broadcast = %d, want %d (one per deposit)", got, depositCount)
	}
	_, broadcastsA := clientA.counts()
	_, broadcastsB := clientB.counts()
	if broadcastsA+broadcastsB != depositCount {
		t.Fatalf("broadcasts A=%d B=%d, want %d in total", broadcastsA, broadcastsB, depositCount)
	}
	for _, deposit := range deposits {
		state := readSweepRetryState(t, testApp.DB, deposit.ID)
		if state.status != "confirmed" || state.submitCount != 1 {
			t.Fatalf("deposit %s = %+v, want confirmed after exactly one submit", deposit.ID, state)
		}
		position, found, err := testApp.Store.GetPosition(context.Background(), deposit.UserID, deposit.GroupID)
		if err != nil || !found {
			t.Fatalf("GetPosition: found=%v err=%v", found, err)
		}
		if position.ShareUnits != deposit.Amount || position.AmountDeposited != deposit.Amount {
			t.Fatalf("position = %+v, want credited exactly once for %d", position, deposit.Amount)
		}
	}
}

func TestSweepPoller_surplusReconcile_isStaggeredPerGroupAndPerTick(t *testing.T) {
	// Arrange: more real groups than one tick may reconcile.
	testApp := integrationWorkerApp(t)
	rec := &recordingPrivy{Client: testApp.Privy}
	deposits := app.NewDepositService(testApp.Store, rec, nil, app.NewSymbolResolver(nil))
	clock := NewStubClock(testApp.Now)
	poller := NewSweepPoller(testApp.Store, rec, NewFakeSolanaRPC(), deposits, "relayer-key", clock)

	const groupCount = surplusReconcileBatch + 2
	treasuries := make(map[string]bool, groupCount)
	groupIDs := make([]string, 0, groupCount)
	for i := 0; i < groupCount; i++ {
		deposit, _, _ := seedPendingDeposit(t, testApp)
		mustExec(t, testApp.DB, `UPDATE deposits SET status = 'failed' WHERE id = $1`, deposit.ID)
		treasury, found, err := testApp.Store.GetTreasuryByGroupID(context.Background(), deposit.GroupID)
		if err != nil || !found {
			t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
		}
		treasuries[treasury.SolanaAddress] = true
		groupIDs = append(groupIDs, deposit.GroupID)
	}
	markOtherTreasuriesSurplusChecked(t, testApp.DB, testApp.Now, groupIDs...)
	callsPerTreasury := func() map[string]int {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		calls := make(map[string]int)
		for _, address := range rec.treasuryBalance {
			if treasuries[address] {
				calls[address]++
			}
		}
		return calls
	}
	total := func(calls map[string]int) int {
		sum := 0
		for _, n := range calls {
			sum += n
		}
		return sum
	}

	// Act + Assert: one tick reads at most a batch of treasuries.
	mustTick(t, poller)
	if got := total(callsPerTreasury()); got > surplusReconcileBatch {
		t.Fatalf("treasury reads in one tick = %d, want at most %d", got, surplusReconcileBatch)
	}

	// Further ticks inside the interval finish the round and then go quiet.
	for i := 0; i < 5; i++ {
		mustTick(t, poller)
	}
	calls := callsPerTreasury()
	if len(calls) != groupCount {
		t.Fatalf("treasuries reconciled = %d, want all %d", len(calls), groupCount)
	}
	for address, n := range calls {
		if n != 1 {
			t.Fatalf("treasury %s read %d times within one interval, want 1", address, n)
		}
	}

	// After the interval each group is due again.
	clock.Advance(surplusReconcileEvery + time.Second)
	for i := 0; i < 5; i++ {
		mustTick(t, poller)
	}
	for address, n := range callsPerTreasury() {
		if n != 2 {
			t.Fatalf("treasury %s read %d times after second interval, want 2", address, n)
		}
	}
}
