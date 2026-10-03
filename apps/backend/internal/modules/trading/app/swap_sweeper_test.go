package app

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/events"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/sqlc"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/clock"
	"github.com/monaco/monaco/apps/backend/internal/platform/db"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
	"github.com/monaco/monaco/apps/backend/internal/platform/observability"
	"github.com/monaco/monaco/apps/backend/internal/testkit"
)

type sweeperReader struct {
	statuses    []SigStatus
	statusErr   error
	inbound     money.BaseUnits
	inboundErr  error
	valid       bool
	validErr    error
	sigs        []chain.Signature
	batches     [][]chain.Signature
	owner       chain.SolanaAddress
	mint        chain.Mint
	followups   [][]SigStatus
	followupErr error
	onStatus    func()
	calls       int
}

func (r *sweeperReader) SignatureStatuses(
	_ context.Context,
	sigs []chain.Signature,
) ([]SigStatus, error) {
	r.calls++
	r.sigs = append([]chain.Signature(nil), sigs...)
	r.batches = append(r.batches, r.sigs)
	if r.onStatus != nil {
		r.onStatus()
	}
	if r.calls > 1 && len(r.followups) > 0 {
		reply := r.followups[0]
		r.followups = r.followups[1:]
		return reply, r.statusErr
	}
	if r.calls > 1 && r.followupErr != nil {
		return nil, r.followupErr
	}
	return r.statuses, r.statusErr
}

func (r *sweeperReader) BlockhashValid(context.Context, []byte) (bool, error) {
	return r.valid, r.validErr
}

func (r *sweeperReader) InboundAmount(
	_ context.Context, _ chain.Signature, owner chain.SolanaAddress, mint chain.Mint,
) (money.BaseUnits, error) {
	r.owner, r.mint = owner, mint
	return r.inbound, r.inboundErr
}

type sweeperHints struct {
	sent   int
	cancel context.CancelFunc
}

func (h *sweeperHints) PublishHint(context.Context, string, []byte) {
	h.sent++
	if h.cancel != nil {
		h.cancel()
	}
}

type sweeperEnv struct {
	pool sqlc.DBTX
	clk  *testkit.Clock
	ids  *testkit.IDs
	uow  *db.UnitOfWork
	q    *sqlc.Queries
	read *sweeperReader
	hint *sweeperHints
}

func newSweeperEnv(t *testing.T) *sweeperEnv {
	t.Helper()
	clk := testkit.NewClock(clock.Real{}.Now().Truncate(time.Second))
	pool, ids := testkit.DB(t), testkit.NewIDs(91)
	return &sweeperEnv{
		pool: pool, clk: clk, ids: ids, uow: db.New(pool, ids, clk), q: sqlc.New(pool),
		read: &sweeperReader{inbound: money.NewBaseUnits(104_000_000, 8)}, hint: &sweeperHints{},
	}
}

func (e *sweeperEnv) poller() *SwapSweeper {
	return NewSwapSweeper(e.uow, e.pool, e.clk, e.read, e.hint)
}

func (e *sweeperEnv) insert(t *testing.T) sqlc.InsertCreatedParams {
	t.Helper()
	p := sqlc.InsertCreatedParams{
		ID: e.ids.NewV7(), SourceKind: "proposal", SourceID: e.ids.NewV7(), CabalID: e.ids.NewV7(),
		TreasuryAddress: "treasury", Action: "buy", Symbol: "AAPLx", InMint: "usdc", OutMint: "aaplx",
		OutDecimals: 8, InAmount: 25_000_000,
		SlippageBps: 100, SourceBatchSize: 3, CreatedAt: e.clk.Now(),
	}
	if err := e.q.InsertCreated(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *sweeperEnv) submit(t *testing.T) sqlc.InsertCreatedParams {
	t.Helper()
	p := e.insert(t)
	params := sqlc.MarkSubmittedParams{
		ID: p.ID, ExecuteRequestID: p.ID.String(), SignedTx: []byte{1},
		TxSignature: "sig-" + p.ID.String(), SubmittedAt: e.clk.Now(),
	}
	if n, err := e.q.MarkSubmitted(t.Context(), params); err != nil || n != 1 {
		t.Fatalf("MarkSubmitted = %d, %v", n, err)
	}
	return p
}

func (e *sweeperEnv) advance() { e.clk.Advance(SwapSweepAge + time.Second) }

func (e *sweeperEnv) status(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var status string
	if err := e.pool.QueryRow(t.Context(), `SELECT status FROM swaps WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func (e *sweeperEnv) terminalEvents(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var n int
	const terminalEvents = `SELECT count(*) FROM events
		WHERE aggregate_id = $1 AND type IN ('trade.confirmed', 'trade.failed')`
	if err := e.pool.QueryRow(t.Context(), terminalEvents, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *sweeperEnv) terminalEvent(t *testing.T, id uuid.UUID) events.Event {
	t.Helper()
	var typ events.Type
	var payload []byte
	const terminalEvent = `SELECT type, payload FROM events
		WHERE aggregate_id = $1 AND type IN ('trade.confirmed', 'trade.failed')`
	if err := e.pool.QueryRow(t.Context(), terminalEvent, id).Scan(&typ, &payload); err != nil {
		t.Fatal(err)
	}
	event, err := events.Decode(typ, 1, payload)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func tickContext(t *testing.T) context.Context {
	t.Helper()
	return observability.WithActor(t.Context(), "system:poller.trading.swap_sweeper")
}

func TestSwapSweeper_resolvesCreatedAndEveryStaleSignatureState(t *testing.T) {
	t.Parallel()
	e := newSweeperEnv(t)
	created, finalized, missing, processing := e.insert(t), e.submit(t), e.submit(t), e.submit(t)
	e.read.statuses = []SigStatus{
		{State: SigFinalized},
		{State: SigNotFound},
		{State: SigProcessing},
	}
	e.read.followups = [][]SigStatus{{{State: SigNotFound}}}
	e.advance()
	report, err := e.poller().Tick(tickContext(t))
	if err != nil || report.Scanned != 4 || report.Changed != 3 {
		t.Fatalf("Tick = %+v, %v", report, err)
	}
	for _, tc := range []struct {
		id     uuid.UUID
		status string
		events int
	}{{created.ID, "failed", 1}, {finalized.ID, "confirmed", 1}, {missing.ID, "failed", 1}, {processing.ID, "submitted", 0}} {
		if got := e.status(t, tc.id); got != tc.status || e.terminalEvents(t, tc.id) != tc.events {
			t.Fatalf("swap %s = %s with %d events", tc.id, got, e.terminalEvents(t, tc.id))
		}
	}
	wantMint := chain.Mint{Address: "aaplx", Decimals: 8}
	if len(e.read.batches) == 0 || len(e.read.batches[0]) != 3 || e.read.owner != "treasury" ||
		e.read.mint != wantMint ||
		e.hint.sent != 3 {
		t.Fatalf(
			"reader %+v owner %q mint %+v hints %d",
			e.read.batches,
			e.read.owner,
			e.read.mint,
			e.hint.sent,
		)
	}
	e.assertRecoveredEvents(t, created.ID, finalized.ID, wantMint.Address)
}

func (e *sweeperEnv) assertRecoveredEvents(t *testing.T, created, finalized uuid.UUID, outMint chain.SolanaAddress) {
	t.Helper()
	createdEvent, ok := e.terminalEvent(t, created).(events.TradeFailed)
	if !ok || createdEvent.SourceBatchSize != 3 {
		t.Fatalf("created recovery event = %+v", createdEvent)
	}
	confirmedEvent, ok := e.terminalEvent(t, finalized).(events.TradeConfirmed)
	if !ok || confirmedEvent.OutMint != outMint {
		t.Fatalf("confirmed recovery event = %+v", confirmedEvent)
	}
}

func TestSwapSweeper_hasTheConfiguredNameAndInterval(t *testing.T) {
	t.Parallel()
	e := newSweeperEnv(t)
	p := e.poller()
	if p.Name() != "trading.swap_sweeper" || p.Interval() != 30*time.Second {
		t.Fatalf("poller = %s every %s", p.Name(), p.Interval())
	}
	if report, err := p.Tick(tickContext(t)); err != nil || report.Scanned != 0 ||
		report.Changed != 0 {
		t.Fatalf("empty Tick = %+v, %v", report, err)
	}
}

func TestSwapSweeper_reportsAStaleSubmittedQueryFailure(t *testing.T) {
	t.Parallel()
	e := newSweeperEnv(t)
	e.insert(t)
	e.advance()
	ctx, cancel := context.WithCancel(t.Context())
	e.hint.cancel = cancel
	p := e.poller()
	if _, err := p.Tick(observability.WithActor(ctx, "system:poller.trading.swap_sweeper")); err == nil {
		t.Fatal("Tick succeeded after the created recovery canceled its context")
	}
}

func TestSwapSweeper_keepsFailuresAndRacesConvergent(t *testing.T) {
	t.Parallel()
	e := newSweeperEnv(t)
	failed, raced := e.submit(t), e.submit(t)
	e.read.statuses = []SigStatus{{State: SigFinalized, Failed: true}}
	params := sqlc.FinishFailedParams{
		ID: raced.ID, FailureCode: "force_resolved", FailedAt: e.clk.Now(),
	}
	if n, err := e.q.FinishFailed(t.Context(), params); err != nil || n != 1 {
		t.Fatalf("FinishFailed = %d, %v", n, err)
	}
	e.advance()
	report, err := e.poller().Tick(tickContext(t))
	if err != nil || report.Scanned != 1 || report.Changed != 1 ||
		e.status(t, failed.ID) != "failed" || e.terminalEvents(t, failed.ID) != 1 {
		t.Fatalf("Tick = %+v, %v", report, err)
	}
}

func TestSwapSweeper_reportsReaderFailuresWithoutChangingTheRow(t *testing.T) {
	t.Parallel()
	for name, configure := range map[string]func(*sweeperReader){
		"rpc unavailable": func(r *sweeperReader) { r.statusErr = errs.New(errs.CodeRPCUnavailable, "test") },
		"bad statuses":    func(r *sweeperReader) { r.statuses = nil },
		"inbound unavailable": func(r *sweeperReader) {
			r.statuses, r.inboundErr = []SigStatus{{State: SigFinalized}}, errs.New(errs.CodeRPCUnavailable, "test")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newSweeperEnv(t)
			row := e.submit(t)
			configure(e.read)
			e.advance()
			if _, err := e.poller().Tick(tickContext(t)); errs.CodeOf(err) == "" ||
				e.status(t, row.ID) != "submitted" {
				t.Fatalf("Tick = %v, status %s", err, e.status(t, row.ID))
			}
		})
	}
}

func TestSwapSweeper_keepsANotFoundSignatureWhileItsBlockhashIsValid(t *testing.T) {
	t.Parallel()
	e := newSweeperEnv(t)
	row := e.submit(t)
	e.read.statuses, e.read.valid = []SigStatus{{State: SigNotFound}}, true
	e.advance()
	report, err := e.poller().Tick(tickContext(t))
	if err != nil || report.Scanned != 1 || report.Changed != 0 ||
		e.status(t, row.ID) != "submitted" {
		t.Fatalf("Tick = %+v, %v", report, err)
	}
}

func TestSwapSweeper_rechecksAnExpiredBlockhashBeforeFailing(t *testing.T) {
	t.Parallel()
	e := newSweeperEnv(t)
	row := e.submit(t)
	e.read.statuses = []SigStatus{{State: SigNotFound}}
	e.read.followups = [][]SigStatus{{{State: SigFinalized}}}
	e.advance()
	if report, err := e.poller().Tick(tickContext(t)); err != nil || report.Changed != 1 ||
		e.status(t, row.ID) != "confirmed" {
		t.Fatalf("Tick = %+v, %v; status = %s", report, err, e.status(t, row.ID))
	}
}

func TestSwapSweeper_reportsExpiryRecheckAndRotationFailures(t *testing.T) {
	t.Parallel()
	for name, configure := range map[string]func(*sweeperEnv, context.CancelFunc){
		"expiry recheck": func(e *sweeperEnv, _ context.CancelFunc) {
			e.read.statuses = []SigStatus{{State: SigNotFound}}
			e.read.followupErr = errs.New(errs.CodeRPCUnavailable, "test")
		},
		"expiry status count": func(e *sweeperEnv, _ context.CancelFunc) {
			e.read.statuses, e.read.followups = []SigStatus{{State: SigNotFound}}, [][]SigStatus{{}}
		},
		"rotation": func(e *sweeperEnv, cancel context.CancelFunc) {
			e.read.statuses, e.read.onStatus = []SigStatus{{State: SigProcessing}}, cancel
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newSweeperEnv(t)
			e.submit(t)
			e.advance()
			ctx, cancel := context.WithCancel(t.Context())
			configure(e, cancel)
			tickCtx := observability.WithActor(ctx, "system:poller.trading.swap_sweeper")
			if _, err := e.poller().Tick(tickCtx); err == nil {
				t.Fatalf("%s Tick succeeded", name)
			}
		})
	}
}

func TestSwapSweeper_rejectsMalformedRecoveryRows(t *testing.T) {
	t.Parallel()
	e := newSweeperEnv(t)
	row := e.submit(t)
	if _, err := e.pool.Exec(t.Context(), `UPDATE swaps SET tx_signature = NULL WHERE id = $1`, row.ID); err != nil {
		t.Fatal(err)
	}
	e.advance()
	if _, err := e.poller().Tick(tickContext(t)); errs.CodeOf(err) != errs.CodeDecodeFailed ||
		e.status(t, row.ID) != "submitted" {
		t.Fatalf("Tick = %v, status %s", err, e.status(t, row.ID))
	}
}

func TestSwapSweeper_helpersRejectInvalidRows(t *testing.T) {
	t.Parallel()
	e := newSweeperEnv(t)
	for _, tc := range []struct {
		kind, action string
		amount       int64
		batch        int32
	}{{"bad", "buy", 1, 1}, {"proposal", "bad", 1, 1}, {"proposal", "buy", -1, 1}, {"proposal", "buy", 1, 0}} {
		if _, err := sweepRequest(
			e.ids.NewV7(), tc.kind, e.ids.NewV7(), tc.action, "AAPLx", "usdc", tc.amount, tc.batch,
		); errs.CodeOf(err) == "" {
			t.Fatalf("sweepRequest %+v succeeded", tc)
		}
	}
	p := e.poller()
	row := sqlc.ListStaleSubmittedRow{
		ID: e.ids.NewV7(), CabalID: e.ids.NewV7(), SourceKind: "proposal", SourceID: e.ids.NewV7(),
		Action: "buy", Symbol: "AAPLx", InMint: "usdc", InAmount: 1, SourceBatchSize: 1, OutDecimals: 300,
	}
	if _, err := p.resolveSubmitted(
		t.Context(), time.Time{}, row, SigStatus{State: SigFinalized},
	); errs.CodeOf(err) != errs.CodeDecodeFailed {
		t.Fatalf("invalid decimals = %v", err)
	}
	if _, err := p.resolveSubmitted(
		t.Context(), time.Time{}, row, SigStatus{State: 99},
	); errs.CodeOf(err) != errs.CodeDecodeFailed {
		t.Fatalf("invalid state = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := e.poller().Tick(ctx); err == nil {
		t.Fatal("Tick on cancelled context succeeded")
	}
}

func TestSwapSweeper_coversRejectedRowsAndTransactionFailures(t *testing.T) {
	t.Parallel()
	e := newSweeperEnv(t)
	bad := sqlc.ListStaleCreatedRow{
		ID: e.ids.NewV7(), CabalID: e.ids.NewV7(), SourceKind: "bad", SourceID: e.ids.NewV7(),
	}
	if moved, err := e.poller().failCreated(tickContext(t), e.clk.Now(), bad); moved ||
		errs.CodeOf(err) != errs.CodeDecodeFailed {
		t.Fatalf("failCreated = %t, %v", moved, err)
	}
	if _, err := e.poller().resolveSubmitted(tickContext(t), e.clk.Now(), sqlc.ListStaleSubmittedRow{
		CabalID: e.ids.NewV7(), SourceKind: "bad", SourceID: e.ids.NewV7(), SourceBatchSize: 1,
	}, SigStatus{State: SigProcessing}); errs.CodeOf(
		err,
	) != errs.CodeDecodeFailed {
		t.Fatalf("invalid submitted row = %v", err)
	}
	row := e.insert(t)
	req, err := sweepRequest(
		row.CabalID,
		row.SourceKind,
		row.SourceID,
		row.Action,
		row.Symbol,
		row.InMint,
		row.InAmount,
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	step := failed(req, e.ids.NewV7(), "never_submitted", "")
	moved, err := e.poller().move(tickContext(t), e.clk.Now(), req, step)
	if moved || err != nil {
		t.Fatalf("zero-row move = %t, %v", moved, err)
	}
	if moved, err := e.poller().failCreated(t.Context(), e.clk.Now(), sqlc.ListStaleCreatedRow{
		ID: row.ID, CabalID: row.CabalID, SourceKind: row.SourceKind, SourceID: row.SourceID, Action: row.Action,
		Symbol: row.Symbol, InMint: row.InMint, SourceBatchSize: row.SourceBatchSize, InAmount: row.InAmount,
	}); moved || err == nil || e.status(t, row.ID) != "created" {
		t.Fatalf("event failure = %t, %v", moved, err)
	}
	testSweepResolverFailures(t, e)
}

func testSweepResolverFailures(t *testing.T, e *sweeperEnv) {
	t.Helper()
	for _, tc := range []struct {
		status SigStatus
		row    sqlc.ListStaleSubmittedRow
	}{
		{SigStatus{State: SigNotFound}, sqlc.ListStaleSubmittedRow{CabalID: e.ids.NewV7(), SourceKind: "proposal", SourceID: e.ids.NewV7(), Action: "buy", InMint: "usdc", InAmount: 1, SourceBatchSize: 1, SignedTx: []byte{1}}},
		{SigStatus{State: SigFinalized}, sqlc.ListStaleSubmittedRow{CabalID: e.ids.NewV7(), SourceKind: "proposal", SourceID: e.ids.NewV7(), Action: "buy", InMint: "usdc", InAmount: 1, SourceBatchSize: 1, OutDecimals: 8, SignedTx: []byte{1}, ID: e.ids.NewV7()}},
	} {
		e.read.validErr, e.read.inboundErr = errs.New(errs.CodeRPCUnavailable, "test"), nil
		if tc.status.State == SigFinalized {
			e.read.validErr, e.read.inboundErr, e.read.inbound = nil, nil, money.NewBaseUnits(
				math.MaxUint64,
				8,
			)
		}
		if _, err := e.poller().resolveSubmitted(tickContext(t), e.clk.Now(), tc.row, tc.status); errs.CodeOf(
			err,
		) == "" {
			t.Fatalf("resolveSubmitted = %v", err)
		}
	}
}
