package app

import (
	"context"
	"math"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/modules/funding/sqlc"
	"github.com/monaco/monaco/apps/backend/internal/modules/identity/port"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain/solana"
	"github.com/monaco/monaco/apps/backend/internal/platform/clock"
	"github.com/monaco/monaco/apps/backend/internal/platform/db"
	"github.com/monaco/monaco/apps/backend/internal/platform/ids"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
	"github.com/monaco/monaco/apps/backend/internal/testkit"
)

type testWallets struct {
	wallets []port.MemberWallet
	pages   [][]port.MemberWallet
	called  int
	err     error
}

func (*testWallets) MemberWallet(context.Context, ids.UserID) (port.MemberWallet, error) {
	return port.MemberWallet{}, nil
}

func (w *testWallets) MemberWallets(context.Context, ids.UserID, int) ([]port.MemberWallet, error) {
	if w.err != nil {
		return nil, w.err
	}
	if len(w.pages) > 0 {
		page := w.pages[w.called]
		w.called++
		return page, nil
	}
	return w.wallets, nil
}

type pollerRPC struct{ pages [][]solana.SignatureInfo }

type closeLimiter struct{ close func() }

func (l closeLimiter) Wait(context.Context) error {
	l.close()
	return nil
}

func (r *pollerRPC) SignaturesFor(
	_ context.Context,
	_ chain.SolanaAddress,
	_ chain.Signature,
	_ chain.Signature,
	_ int,
) ([]solana.SignatureInfo, error) {
	page := r.pages[0]
	r.pages = r.pages[1:]
	return page, nil
}

func (*pollerRPC) InboundTransfersForMint(
	context.Context, chain.Signature, chain.SolanaAddress, chain.SolanaAddress,
) ([]solana.Transfer, error) {
	return nil, nil
}

func TestDepositPollerFetchesOnePageAndHonorsCancellation(t *testing.T) {
	t.Parallel()
	first := make([]solana.SignatureInfo, 1000)
	first[999].Signature = "before"
	rpc := &pollerRPC{pages: [][]solana.SignatureInfo{first}}
	p := &DepositPoller{rpc: rpc, limit: rate.NewLimiter(rate.Inf, 1)}
	if got, err := p.signaturesPage(t.Context(), "wallet", "before", "until", 1000); err != nil || len(got) != 1000 {
		t.Fatalf("signaturesPage = %d, %v", len(got), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p.limit = rate.NewLimiter(rate.Limit(1), 0)
	if _, err := p.signaturesPage(ctx, "wallet", "", "until", 1000); err == nil {
		t.Fatal("cancelled signaturesPage error = nil")
	}
	if _, err := p.scanSignature(ctx, port.MemberWallet{}, solana.SignatureInfo{}); err == nil {
		t.Fatal("cancelled scanSignature error = nil")
	}
}

func TestNewRPCLimiterDefaultsToTwentyPerSecond(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in    int32
		limit rate.Limit
		burst int
	}{{0, 20, 20}, {-1, 20, 20}, {7, 7, 7}} {
		if l := NewRPCLimiter(tc.in); l.Limit() != tc.limit || l.Burst() != tc.burst {
			t.Fatalf("NewRPCLimiter(%d) = %v/%d, want %v/%d", tc.in, l.Limit(), l.Burst(), tc.limit, tc.burst)
		}
	}
	if p := NewDepositPoller(nil, nil, nil, nil, nil, nil, "usdc", time.Second, nil, nil); p.Interval() != time.Second {
		t.Fatalf("interval = %s", p.Interval())
	}
}

func TestDepositPollerPagesWalletsAndReportsCursorAndLimiterFailures(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	user := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	p := &DepositPoller{
		reads: pool,
		wallets: &testWallets{wallets: []port.MemberWallet{
			{UserID: user.ID, Address: user.Address},
		}},
	}
	other := testkit.SeedUser(t, pool, testkit.UserOpts{WithWallet: true})
	p.wallets = &testWallets{wallets: []port.MemberWallet{
		{UserID: user.ID, Address: user.Address},
		{UserID: other.ID, Address: other.Address},
	}}
	const insertCursor = `INSERT INTO deposit_cursors (wallet_address, last_signature, cursor_slot, scanned_at) VALUES ($1, '', 0, now())`
	if _, err := pool.Exec(t.Context(), insertCursor, user.Address); err != nil {
		t.Fatal(err)
	}
	if got, err := p.memberWallets(t.Context()); err != nil || got[0].wallet.Address != other.Address {
		t.Fatalf("ordered wallets = %+v, %v", got, err)
	}
	p.wallets = &testWallets{err: errs.New(errs.CodeInternal, "test.wallets")}
	if _, err := p.memberWallets(t.Context()); err == nil {
		t.Fatal("reader error = nil")
	}
	p.wallets = &testWallets{wallets: []port.MemberWallet{
		{UserID: user.ID, Address: user.Address},
		{UserID: other.ID, Address: other.Address},
	}}
	if got, err := p.memberWallets(t.Context()); err != nil || len(got) != 2 {
		t.Fatalf("memberWallets = %d, %v; want two wallets", len(got), err)
	}
	testkit.AssertQueries(t, "DepositPoller memberWallets", func() {
		got, err := p.memberWallets(t.Context())
		if err != nil || len(got) != 2 {
			t.Fatalf("memberWallets = %d, %v; want two wallets", len(got), err)
		}
	})
}

func TestDepositPollerReportsWalletSchedulingErrors(t *testing.T) {
	t.Parallel()
	readerErr := errs.New(errs.CodeInternal, "test.wallets")
	p := &DepositPoller{wallets: &testWallets{err: readerErr}}
	if _, err := p.Tick(t.Context()); err == nil {
		t.Fatal("tick reader error = nil")
	}

	pool := testkit.DB(t)
	p = &DepositPoller{
		reads:   pool,
		wallets: &testWallets{wallets: []port.MemberWallet{{Address: "wallet"}}},
	}
	pool.Close()
	if _, err := p.memberWallets(t.Context()); err == nil {
		t.Fatal("cursor reader error = nil")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := p.scanWallets(ctx, nil); err == nil {
		t.Fatal("cancelled scanWallets error = nil")
	}
}

func TestDepositPollerPagesMemberWalletReader(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	first := make([]port.MemberWallet, port.MaxWalletPage)
	for i := range first {
		first[i] = port.MemberWallet{Address: "wallet"}
	}
	p := &DepositPoller{
		reads:   pool,
		wallets: &testWallets{pages: [][]port.MemberWallet{first, nil}},
	}
	if wallets, err := p.memberWallets(t.Context()); err != nil || len(wallets) != len(first) {
		t.Fatalf("memberWallets = %d, %v; want %d wallets", len(wallets), err, len(first))
	}
}

func TestDepositPollerAmountForRejectsOverflow(t *testing.T) {
	t.Parallel()
	p := DepositPoller{usdc: "usdc"}
	_, err := p.amountFor([]solana.Transfer{
		{Mint: chain.Mint{Address: "usdc", Decimals: 6}, Net: money.NewBaseUnits(math.MaxUint64, 6)},
		{Mint: chain.Mint{Address: "usdc", Decimals: 6}, Net: money.NewBaseUnits(1, 6)},
	})
	if err == nil {
		t.Fatal("amountFor overflow error = nil")
	}
}

func TestDepositPollerRejectsSlotsAboveInt64(t *testing.T) {
	t.Parallel()
	p := DepositPoller{}
	sig := solana.SignatureInfo{Slot: uint64(math.MaxInt64) + 1}
	if _, err := p.credit(t.Context(), port.MemberWallet{}, sig, money.MicrosFromUint64(1)); err == nil {
		t.Fatal("credit overflow error = nil")
	}
	if err := p.advance(t.Context(), "wallet", "signature", sig.Slot); err == nil {
		t.Fatal("advance overflow error = nil")
	}
	if err := p.finishBackfill(t.Context(), "wallet", backfillCursor{head: "signature", slot: sig.Slot}); err == nil {
		t.Fatal("finishBackfill overflow error = nil")
	}
}

func TestDepositPollerWrapsAdvanceFailures(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	pool.Close()
	p := NewDepositPoller(
		pool, db.New(pool, testkit.NewIDs(1), clock.Real{}), testkit.NewIDs(2), clock.Real{}, nil, nil,
		"usdc", time.Second, NewRPCLimiter(1), nil,
	)
	if err := p.advance(t.Context(), "wallet", "signature", 1); err == nil || errs.CodeOf(err) != errs.CodeInternal {
		t.Fatalf("advance error = %v", err)
	}
	if err := p.finishBackfill(t.Context(), "wallet", backfillCursor{head: "signature", slot: 1}); err == nil {
		t.Fatal("finishBackfill error = nil")
	}
	if _, err := p.processBackfillPage(
		t.Context(),
		port.MemberWallet{Address: "wallet"},
		[]solana.SignatureInfo{{Signature: "signature", Slot: 1, Failed: true}},
		&backfillCursor{},
	); err == nil {
		t.Fatal("processBackfillPage error = nil")
	}
	if err := p.setBackfill(t.Context(), "wallet", "before", "head", uint64(math.MaxInt64)+1); err == nil {
		t.Fatal("setBackfill overflow error = nil")
	}
}

func TestDepositPollerRejectsInvalidBackfill(t *testing.T) {
	t.Parallel()
	p := DepositPoller{usdc: testkit.USDCMint}
	if _, err := p.scanMember(t.Context(), memberWallet{wallet: port.MemberWallet{Address: "bad"}}); err == nil {
		t.Fatal("scanMember invalid address error = nil")
	}
	if _, err := newBackfillCursor(sqlc.DepositCursorsForWalletsRow{BackfillBeforeSignature: "before"}); err == nil {
		t.Fatal("newBackfillCursor invalid frontier error = nil")
	}
	if _, err := p.scanMember(t.Context(), memberWallet{
		wallet: port.MemberWallet{Address: testkit.USDCMint},
		cursor: sqlc.DepositCursorsForWalletsRow{
			Exists:                  true,
			BackfillBeforeSignature: "before",
		},
	}); err == nil {
		t.Fatal("scanMember invalid frontier error = nil")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.walkBackfill(ctx, "ata", port.MemberWallet{}, backfillCursor{}); err == nil {
		t.Fatal("walkBackfill cancelled error = nil")
	}
}

func TestDepositPollerReturnsFinishBackfillError(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	if _, err := pool.Exec(
		t.Context(),
		`INSERT INTO deposit_cursors (wallet_address, last_signature, cursor_slot, scanned_at)
		VALUES ('wallet', '', 0, now())`,
	); err != nil {
		t.Fatal(err)
	}
	p := DepositPoller{
		uow:   db.New(pool, testkit.NewIDs(90), clock.Real{}),
		rpc:   &pollerRPC{},
		limit: closeLimiter{close: pool.Close},
	}
	_, err := p.processBackfillPage(
		t.Context(),
		port.MemberWallet{Address: "wallet"},
		[]solana.SignatureInfo{{Signature: "signature", Slot: 1}},
		&backfillCursor{},
	)
	if err == nil {
		t.Fatal("processBackfillPage finish error = nil")
	}
}

func TestDepositPollerReturnsBackfillQueryFailures(t *testing.T) {
	t.Parallel()
	pool := testkit.DB(t)
	p := DepositPoller{uow: db.New(pool, testkit.NewIDs(91), clock.Real{}), clock: clock.Real{}}
	if _, err := pool.Exec(t.Context(), `DROP TABLE deposit_cursors`); err != nil {
		t.Fatal(err)
	}
	if err := p.finishBackfill(t.Context(), "wallet", backfillCursor{head: "signature", slot: 1}); err == nil {
		t.Fatal("finishBackfill query error = nil")
	}
	if err := p.setBackfill(t.Context(), "wallet", "before", "head", 1); err == nil {
		t.Fatal("setBackfill query error = nil")
	}
}
