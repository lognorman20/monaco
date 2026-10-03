package funding_test

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/modules/funding/app"
	"github.com/monaco/monaco/apps/backend/internal/modules/identity"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain/solana"
	"github.com/monaco/monaco/apps/backend/internal/platform/clock"
	"github.com/monaco/monaco/apps/backend/internal/platform/db"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
	"github.com/monaco/monaco/apps/backend/internal/platform/observability"
	"github.com/monaco/monaco/apps/backend/internal/testkit"
	"github.com/monaco/monaco/apps/backend/internal/testkit/fakes"
)

const depositSignature = "5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW"

func depositBlockTime() time.Time { return time.Unix(1_790_000_000, 0).UTC() }

type depositRPC struct {
	signatures      []solana.SignatureInfo
	transfers       []solana.Transfer
	err             error
	signErr         error
	transferErr     error
	signaturesFor   func(chain.Signature, chain.Signature, int) []solana.SignatureInfo
	signCalls       int
	signatureLimits []int
	transferCalls   int
}

func signaturesFromHistory(
	history []solana.SignatureInfo, before, until chain.Signature, limit int,
) []solana.SignatureInfo {
	start, end := 0, len(history)
	for i, sig := range history {
		if sig.Signature == before {
			start = i + 1
		}
	}
	for i, sig := range history {
		if sig.Signature == until && i >= start {
			end = i
		}
	}
	page := history[start:end]
	if len(page) > limit {
		return page[:limit]
	}
	return page
}

func (r *depositRPC) SignaturesFor(
	_ context.Context,
	_ chain.SolanaAddress,
	before chain.Signature,
	until chain.Signature,
	limit int,
) ([]solana.SignatureInfo, error) {
	r.signCalls++
	r.signatureLimits = append(r.signatureLimits, limit)
	if r.signaturesFor != nil {
		return r.signaturesFor(before, until, limit), r.signErr
	}
	if r.signErr != nil {
		return nil, r.signErr
	}
	return r.signatures, r.err
}

func TestDepositPollerStartsNewWalletsAtTheChainTip(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	now := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
	old := solana.SignatureInfo{Signature: "old", Slot: 1, BlockTime: depositBlockTime()}
	later := solana.SignatureInfo{Signature: "later", Slot: 2, BlockTime: depositBlockTime()}
	rpc := depositRPC{
		signaturesFor: func(_ chain.Signature, until chain.Signature, _ int) []solana.SignatureInfo {
			if until == "old" {
				return []solana.SignatureInfo{later}
			}
			return []solana.SignatureInfo{old}
		},
		transfers: []solana.Transfer{{
			Mint: chain.Mint{Address: testkit.USDCMint, Decimals: 6},
			Net:  money.NewBaseUnits(1, 6),
		}},
	}
	p := app.NewDepositPoller(
		pool, db.New(pool, testkit.NewIDs(70), testkit.NewClock(now)), testkit.NewIDs(71), testkit.NewClock(now),
		fakes.NewIdentity(nil, []identity.MemberWallet{{UserID: user.ID, Address: user.Address}}),
		&rpc, testkit.USDCMint, app.DepositPollInterval, unlimited(), &hints{},
	)
	ctx := observability.WithActor(t.Context(), "system:poller.funding.deposits")
	if report, err := p.Tick(ctx); err != nil || report.Changed != 0 {
		t.Fatalf("first Tick = %+v, %v", report, err)
	}
	if report, err := p.Tick(ctx); err != nil || report.Changed != 1 {
		t.Fatalf("second Tick = %+v, %v", report, err)
	}
	if got, want := fmt.Sprint(rpc.signatureLimits), "[1 1000]"; got != want {
		t.Fatalf("signature limits = %s, want %s", got, want)
	}
	var count int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM deposits WHERE tx_signature = 'later'`,
	).Scan(&count); err != nil || count != 1 {
		t.Fatalf("later deposits = %d, %v", count, err)
	}
}

func TestDepositPollerResumesBackfillOnePageAtATime(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	now := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(
		t.Context(),
		`INSERT INTO deposit_cursors (wallet_address, last_signature, cursor_slot, scanned_at)
		VALUES ($1, 'baseline', 0, $2)`,
		user.Address,
		now,
	); err != nil {
		t.Fatal(err)
	}
	rpc, beforeCalls, untilCalls := pagedBackfillRPC()
	budget := &rpcBudget{}
	p := app.NewDepositPoller(
		pool, db.New(pool, testkit.NewIDs(72), testkit.NewClock(now)), testkit.NewIDs(73), testkit.NewClock(now),
		fakes.NewIdentity(nil, []identity.MemberWallet{{UserID: user.ID, Address: user.Address}}),
		&rpc, testkit.USDCMint, app.DepositPollInterval, budget, &hints{},
	)
	ctx := observability.WithActor(t.Context(), "system:poller.funding.deposits")
	for _, allowance := range []int{3, 3, 2} {
		budget.refill(allowance)
		if _, err := p.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertBackfillProgress(ctx, t, pool, user.Address, rpc.signCalls, *beforeCalls, *untilCalls)
}

func TestDepositPollerCheckpointsCompletedSignaturesWhenTheTickEndsMidPage(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	now := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(
		t.Context(),
		`INSERT INTO deposit_cursors (wallet_address, last_signature, cursor_slot, scanned_at)
		VALUES ($1, 'baseline', 0, $2)`,
		user.Address,
		now,
	); err != nil {
		t.Fatal(err)
	}
	history := make([]solana.SignatureInfo, 0, 1000)
	for i := 1000; i > 0; i-- {
		history = append(history, solana.SignatureInfo{
			Signature: chain.Signature(fmt.Sprintf("sig%d", i)), Slot: uint64(i), Failed: true,
		})
	}
	for i := range 3 {
		history[i].Failed = false
	}
	rpc := depositRPC{
		signaturesFor: func(before, until chain.Signature, limit int) []solana.SignatureInfo {
			return signaturesFromHistory(history, before, until, limit)
		},
		transfers: []solana.Transfer{{
			Mint: chain.Mint{Address: testkit.USDCMint, Decimals: 6},
			Net:  money.NewBaseUnits(1, 6),
		}},
	}
	budget := &deadlineBudget{}
	pollerClock := testkit.NewClock(now)
	p := app.NewDepositPoller(
		pool, db.New(pool, testkit.NewIDs(76), pollerClock), testkit.NewIDs(77), pollerClock,
		fakes.NewIdentity(nil, []identity.MemberWallet{{UserID: user.ID, Address: user.Address}}),
		&rpc, testkit.USDCMint, app.DepositPollInterval, budget, &hints{},
	)
	ctx, cancel := context.WithCancel(observability.WithActor(t.Context(), "system:poller.funding.deposits"))
	pollerClock.Advance(time.Second)
	budget.left, budget.cancel = 3, cancel
	if _, err := p.Tick(ctx); err == nil {
		t.Fatal("Tick deadline error = nil")
	}
	resumeCtx := observability.WithActor(t.Context(), "system:poller.funding.deposits")
	assertBackfillFrontier(resumeCtx, t, pool, user.Address, "sig999", "sig1000")
	assertCursorScannedAt(resumeCtx, t, pool, user.Address, now.Add(time.Second))
	budget.left = 2
	if _, err := p.Tick(resumeCtx); err != nil {
		t.Fatal(err)
	}
	assertMidPageProgress(resumeCtx, t, pool, user.Address, rpc.transferCalls)
}

func assertBackfillFrontier(
	ctx context.Context, t *testing.T, pool *pgxpool.Pool, address chain.SolanaAddress, wantBefore, wantHead string,
) {
	t.Helper()
	var before, head string
	err := pool.QueryRow(
		ctx,
		`SELECT backfill_before_signature, backfill_head_signature FROM deposit_cursors WHERE wallet_address = $1`,
		address,
	).Scan(&before, &head)
	if err != nil || before != wantBefore || head != wantHead {
		t.Fatalf("frontier = %q/%q, %v; want %s/%s", before, head, err, wantBefore, wantHead)
	}
}

func assertCursorScannedAt(
	ctx context.Context, t *testing.T, pool *pgxpool.Pool, address chain.SolanaAddress, want time.Time,
) {
	t.Helper()
	var got time.Time
	err := pool.QueryRow(
		ctx, `SELECT scanned_at FROM deposit_cursors WHERE wallet_address = $1`, address,
	).Scan(&got)
	if err != nil || !got.Equal(want) {
		t.Fatalf("scanned_at = %s, %v; want %s", got, err, want)
	}
}

func assertMidPageProgress(
	ctx context.Context, t *testing.T, pool *pgxpool.Pool, address chain.SolanaAddress, transferCalls int,
) {
	t.Helper()
	if transferCalls != 3 {
		t.Fatalf("transfer calls = %d, want 3", transferCalls)
	}
	var deposits int
	err := pool.QueryRow(
		ctx, `SELECT count(*) FROM deposits WHERE wallet_address = $1`, address,
	).Scan(&deposits)
	if err != nil || deposits != 3 {
		t.Fatalf("deposits = %d, %v; want 3", deposits, err)
	}
	var cursor string
	err = pool.QueryRow(
		ctx, `SELECT last_signature FROM deposit_cursors WHERE wallet_address = $1`, address,
	).Scan(&cursor)
	if err != nil || cursor != "sig1000" {
		t.Fatalf("cursor = %q, %v; want sig1000", cursor, err)
	}
}

func TestDepositPollerDoesNotCreditBeforeTheCursorAfterACompletedBackfillCrash(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	now := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(
		t.Context(),
		`INSERT INTO deposit_cursors (
			wallet_address, last_signature, cursor_slot, scanned_at,
			backfill_before_signature, backfill_head_signature, backfill_head_slot
		) VALUES ($1, 'sig10', 10, $2, 'sig9', 'sig10', 10)`,
		user.Address,
		now,
	); err != nil {
		t.Fatal(err)
	}
	history := []solana.SignatureInfo{
		{Signature: "sig10", Slot: 10, BlockTime: depositBlockTime()},
		{Signature: "sig9", Slot: 9, BlockTime: depositBlockTime()},
		{Signature: "old", Slot: 8, BlockTime: depositBlockTime()},
		{Signature: "before-registration-1", Slot: 5, BlockTime: depositBlockTime()},
		{Signature: "before-registration-2", Slot: 4, BlockTime: depositBlockTime()},
	}
	rpc := depositRPC{
		signaturesFor: func(before, until chain.Signature, limit int) []solana.SignatureInfo {
			return signaturesFromHistory(history, before, until, limit)
		},
		transfers: []solana.Transfer{{
			Mint: chain.Mint{Address: testkit.USDCMint, Decimals: 6},
			Net:  money.NewBaseUnits(1, 6),
		}},
	}
	p := app.NewDepositPoller(
		pool, db.New(pool, testkit.NewIDs(74), testkit.NewClock(now)), testkit.NewIDs(75), testkit.NewClock(now),
		fakes.NewIdentity(nil, []identity.MemberWallet{{UserID: user.ID, Address: user.Address}}),
		&rpc, testkit.USDCMint, app.DepositPollInterval, unlimited(), &hints{},
	)
	ctx := observability.WithActor(t.Context(), "system:poller.funding.deposits")
	for range 3 {
		if _, err := p.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var deposits int
	if err := pool.QueryRow(
		ctx, `SELECT count(*) FROM deposits WHERE wallet_address = $1`, user.Address,
	).Scan(&deposits); err != nil {
		t.Fatal(err)
	}
	if deposits != 0 {
		t.Fatalf("deposits before the cursor = %d, want 0", deposits)
	}
}

func pagedBackfillRPC() (depositRPC, *[]chain.Signature, *[]chain.Signature) {
	pages := map[chain.Signature][]solana.SignatureInfo{}
	creditable := map[chain.Signature]bool{
		"sig2500": true,
		"sig1501": true,
		"sig1500": true,
		"sig501":  true,
		"sig1":    true,
	}
	for page, start := range []int{2500, 1500, 500} {
		items := make([]solana.SignatureInfo, 0, 1000)
		for i := start; i > start-1000 && i > 0; i-- {
			signature := chain.Signature(fmt.Sprintf("sig%d", i))
			items = append(items, solana.SignatureInfo{
				Signature: signature,
				Slot:      uint64(i),
				Failed:    !creditable[signature],
			})
		}
		if page == 0 {
			pages[""] = items
		} else {
			pages[chain.Signature(fmt.Sprintf("sig%d", start+1))] = items
		}
	}
	var beforeCalls []chain.Signature
	var untilCalls []chain.Signature
	rpc := depositRPC{
		signaturesFor: func(before, until chain.Signature, _ int) []solana.SignatureInfo {
			beforeCalls = append(beforeCalls, before)
			untilCalls = append(untilCalls, until)
			page := pages[before]
			for i, sig := range page {
				if sig.Signature == until {
					return page[:i]
				}
			}
			return page
		},
		transfers: []solana.Transfer{{
			Mint: chain.Mint{Address: testkit.USDCMint, Decimals: 6},
			Net:  money.NewBaseUnits(1, 6),
		}},
	}
	return rpc, &beforeCalls, &untilCalls
}

func assertBackfillProgress(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	address chain.SolanaAddress,
	calls int,
	beforeCalls []chain.Signature,
	untilCalls []chain.Signature,
) {
	t.Helper()
	if calls != 3 {
		t.Fatalf("SignaturesFor calls = %d, want 3", calls)
	}
	if got, want := fmt.Sprint(beforeCalls), "[ sig1501 sig501]"; got != want {
		t.Fatalf("before calls = %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(untilCalls), "[baseline baseline baseline]"; got != want {
		t.Fatalf("until calls = %s, want %s", got, want)
	}
	rows, err := pool.Query(
		ctx,
		`SELECT tx_signature, count(*) FROM deposits WHERE wallet_address = $1 GROUP BY tx_signature`,
		address,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var (
			signature string
			count     int
		)
		if err := rows.Scan(&signature, &count); err != nil {
			t.Fatal(err)
		}
		got[signature] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"sig2500": 1, "sig1501": 1, "sig1500": 1, "sig501": 1, "sig1": 1}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("deposits = %v, want %v", got, want)
	}
	var cursor string
	if err := pool.QueryRow(
		ctx,
		`SELECT last_signature FROM deposit_cursors WHERE wallet_address = $1`,
		address,
	).Scan(&cursor); err != nil || cursor != "sig2500" {
		t.Fatalf("cursor = %q, %v, want sig2500", cursor, err)
	}
}

func (r *depositRPC) InboundTransfersForMint(
	context.Context,
	chain.Signature,
	chain.SolanaAddress,
	chain.SolanaAddress,
) ([]solana.Transfer, error) {
	r.transferCalls++
	if r.transferErr != nil {
		return nil, r.transferErr
	}
	return r.transfers, r.err
}

func TestDepositPollerReportsSignatureAndTransferFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		rpc  depositRPC
	}{
		{"signatures", depositRPC{signErr: errs.New(errs.CodeRPCUnavailable, "test.rpc")}},
		{"transfers", depositRPC{signatures: []solana.SignatureInfo{{Signature: depositSignature, Slot: 42, BlockTime: depositBlockTime()}}, transferErr: errs.New(errs.CodeRPCUnavailable, "test.rpc")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pool := testkit.DB(t)
			user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
			now := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
			p := newDepositPoller(t, pool, user, now, &tc.rpc, 31, 32)
			if _, err := p.Tick(t.Context()); err == nil {
				t.Fatal("Tick error = nil")
			}
		})
	}
}

func unlimited() *rate.Limiter { return rate.NewLimiter(rate.Inf, 0) }

type rpcBudget struct {
	mu   sync.Mutex
	left int
}

type deadlineBudget struct {
	left   int
	cancel context.CancelFunc
}

func (b *deadlineBudget) Wait(context.Context) error {
	if b.left == 0 {
		b.cancel()
		return context.Canceled
	}
	b.left--
	return nil
}

func (b *rpcBudget) refill(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.left = n
}

func (b *rpcBudget) Wait(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.left == 0 {
		return errs.New(errs.CodeInternal, "test.rpcBudget")
	}
	b.left--
	return nil
}

func newDepositPoller(
	t *testing.T,
	pool *pgxpool.Pool,
	user testkit.SeededUser,
	now time.Time,
	rpc *depositRPC,
	uowID, pollerID uint64,
) *app.DepositPoller {
	t.Helper()
	if _, err := pool.Exec(
		t.Context(),
		`INSERT INTO deposit_cursors (wallet_address, last_signature, cursor_slot, scanned_at)
		VALUES ($1, 'baseline', 1, $2)`,
		user.Address,
		now,
	); err != nil {
		t.Fatal(err)
	}
	return app.NewDepositPoller(
		pool, db.New(pool, testkit.NewIDs(uowID), testkit.NewClock(now)),
		testkit.NewIDs(pollerID), testkit.NewClock(now),
		fakes.NewIdentity(nil, []identity.MemberWallet{{UserID: user.ID, Address: user.Address}}),
		rpc, testkit.USDCMint, app.DepositPollInterval, unlimited(), &hints{},
	)
}

func TestDepositPollerAdvancesFailedSignatureAndRejectsOverflow(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	now := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
	rpc := depositRPC{signatures: []solana.SignatureInfo{{
		Signature: depositSignature, Slot: 42, Failed: true, BlockTime: depositBlockTime(),
	}}}
	p := newDepositPoller(t, pool, user, now, &rpc, 33, 34)
	if report, err := p.Tick(t.Context()); err != nil || report.Changed != 0 {
		t.Fatalf("failed Tick = %+v, %v", report, err)
	}
	rpc.signatures = []solana.SignatureInfo{{Signature: "overflow", Slot: 43, BlockTime: depositBlockTime()}}
	rpc.transfers = []solana.Transfer{
		{Mint: chain.Mint{Address: testkit.USDCMint, Decimals: 6}, Net: money.NewBaseUnits(math.MaxUint64, 6)},
		{Mint: chain.Mint{Address: testkit.USDCMint, Decimals: 6}, Net: money.NewBaseUnits(1, 6)},
	}
	if _, err := p.Tick(t.Context()); err == nil {
		t.Fatal("overflow Tick error = nil")
	}
}

func TestDepositPollerReportsCreditFailureWithoutActor(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	now := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
	rpc := depositRPC{
		signatures: []solana.SignatureInfo{{Signature: depositSignature, Slot: 42, BlockTime: depositBlockTime()}},
		transfers: []solana.Transfer{
			{Mint: chain.Mint{Address: testkit.USDCMint, Decimals: 6}, Net: money.NewBaseUnits(1, 6)},
		},
	}
	p := newDepositPoller(t, pool, user, now, &rpc, 35, 36)
	if _, err := p.Tick(t.Context()); err == nil {
		t.Fatal("Tick error = nil")
	}
}

func TestDepositPollerTouchesCursorAfterAnEmptyScan(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	now := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
	rpc := depositRPC{}
	p := newDepositPoller(t, pool, user, now, &rpc, 37, 38)
	ctx := observability.WithActor(t.Context(), "system:poller.funding.deposits")
	if report, err := p.Tick(ctx); err != nil || report.Changed != 0 {
		t.Fatalf("Tick = %+v, %v", report, err)
	}
	var scannedAt time.Time
	const cursorScan = `SELECT scanned_at FROM deposit_cursors WHERE wallet_address = $1`
	err := pool.QueryRow(ctx, cursorScan, user.Address).Scan(&scannedAt)
	if err != nil || !scannedAt.Equal(now) {
		t.Fatalf("scanned_at = %s, %v; want %s", scannedAt, err, now)
	}
}

func TestDepositPoller_creditsUSDCAndAdvancesPastOtherTokens(t *testing.T) {
	t.Parallel()
	testDepositPollerAdvancesPastOtherTokens(t)
}

func testDepositPollerAdvancesPastOtherTokens(t *testing.T) {
	t.Helper()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	now := clock.Real{}.Now().UTC()
	rpc := depositRPC{
		signatures: []solana.SignatureInfo{{Signature: depositSignature, Slot: 42, BlockTime: depositBlockTime()}},
		transfers: []solana.Transfer{{
			Mint: chain.Mint{Address: "not-usdc", Decimals: 6}, Net: money.NewBaseUnits(4, 6),
		}},
	}
	p := newDepositPoller(t, pool, user, now, &rpc, 11, 12)
	report, err := p.Tick(t.Context())
	if err != nil || report.Scanned != 1 || report.Changed != 0 {
		t.Fatalf("Tick = %+v, %v calls=%d/%d", report, err, rpc.signCalls, rpc.transferCalls)
	}
	var cursor string
	const readCursor = `SELECT last_signature FROM deposit_cursors WHERE wallet_address = $1`
	err = pool.QueryRow(t.Context(), readCursor, user.Address).Scan(&cursor)
	if err != nil || cursor != depositSignature {
		t.Fatalf("cursor = %q, %v", cursor, err)
	}
}

func TestDepositPoller_creditsOneDepositOnlyOnce(t *testing.T) {
	t.Parallel()
	testDepositPollerCreditsOneDepositOnlyOnce(t)
}

func testDepositPollerCreditsOneDepositOnlyOnce(t *testing.T) {
	t.Helper()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	now := clock.Real{}.Now().UTC()
	rpc := depositRPC{
		signatures: []solana.SignatureInfo{{Signature: depositSignature, Slot: 42, BlockTime: depositBlockTime()}},
		transfers: []solana.Transfer{{
			Mint: chain.Mint{Address: testkit.USDCMint, Decimals: 6}, Net: money.NewBaseUnits(25_000_000, 6),
		}},
	}
	p := newDepositPoller(t, pool, user, now, &rpc, 13, 14)
	ctx := observability.WithActor(t.Context(), "system:poller.funding.deposits")
	if report, err := p.Tick(ctx); err != nil || report.Changed != 1 {
		t.Fatalf("first Tick = %+v, %v calls=%d/%d", report, err, rpc.signCalls, rpc.transferCalls)
	}
	if report, err := p.Tick(ctx); err != nil || report.Changed != 0 {
		t.Fatalf("second Tick = %+v, %v", report, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM deposits`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("deposits = %d, %v", count, err)
	}
}

func TestDepositPoller_creditsWhenRPCOmitsBlockTime(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	now := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
	rpc := depositRPC{
		signatures: []solana.SignatureInfo{{Signature: depositSignature, Slot: 42}},
		transfers: []solana.Transfer{{
			Mint: chain.Mint{Address: testkit.USDCMint, Decimals: 6}, Net: money.NewBaseUnits(1, 6),
		}},
	}
	p := newDepositPoller(t, pool, user, now, &rpc, 39, 40)
	ctx := observability.WithActor(t.Context(), "system:poller.funding.deposits")
	if report, err := p.Tick(ctx); err != nil || report.Changed != 1 {
		t.Fatalf("Tick = %+v, %v", report, err)
	}
	var blockTime *time.Time
	err := pool.QueryRow(ctx, `SELECT block_time FROM deposits WHERE tx_signature = $1`, depositSignature).
		Scan(&blockTime)
	if err != nil {
		t.Fatal(err)
	}
	if blockTime != nil {
		t.Fatalf("block_time = %s, want NULL", blockTime)
	}
}

type emptyRPC struct{}

func (emptyRPC) SignaturesFor(
	context.Context, chain.SolanaAddress, chain.Signature, chain.Signature, int,
) ([]solana.SignatureInfo, error) {
	return nil, nil
}

func (emptyRPC) InboundTransfersForMint(
	context.Context, chain.Signature, chain.SolanaAddress, chain.SolanaAddress,
) ([]solana.Transfer, error) {
	return nil, nil
}

func TestDepositPollerDefersWalletsPastTheRateBudgetWithoutFailingTheTick(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	wallets := make([]identity.MemberWallet, 0, 4)
	addrs := make([]string, 0, 4)
	for range 4 {
		user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
		wallets = append(wallets, identity.MemberWallet{UserID: user.ID, Address: user.Address})
		addrs = append(addrs, string(user.Address))
	}
	now := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
	budget := &rpcBudget{}
	p := app.NewDepositPoller(
		pool, db.New(pool, testkit.NewIDs(51), testkit.NewClock(now)), testkit.NewIDs(52), testkit.NewClock(now),
		fakes.NewIdentity(nil, wallets), emptyRPC{}, testkit.USDCMint, app.DepositPollInterval, budget, &hints{},
	)
	tick := func() {
		t.Helper()
		budget.refill(1)
		ctx := observability.WithActor(t.Context(), "system:poller.funding.deposits")
		if report, err := p.Tick(ctx); err != nil || report.Scanned != 1 {
			t.Fatalf("Tick = %+v, %v; want one wallet scanned and no error", report, err)
		}
	}
	scanned := func() int {
		t.Helper()
		var n int
		const q = `SELECT count(*) FROM deposit_cursors WHERE wallet_address = ANY($1)`
		if err := pool.QueryRow(t.Context(), q, addrs).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	tick()
	if got := scanned(); got != 1 {
		t.Fatalf("cursors after first tick = %d, want 1: deferred wallets keep no cursor so they sort first", got)
	}
	tick()
}
