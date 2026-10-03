package identity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/monaco/monaco/apps/backend/internal/events"
	"github.com/monaco/monaco/apps/backend/internal/modules/identity"
	"github.com/monaco/monaco/apps/backend/internal/modules/identity/adapters"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/clock"
	"github.com/monaco/monaco/apps/backend/internal/platform/config"
	"github.com/monaco/monaco/apps/backend/internal/platform/db"
	"github.com/monaco/monaco/apps/backend/internal/platform/ids"
	"github.com/monaco/monaco/apps/backend/internal/platform/module"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
	"github.com/monaco/monaco/apps/backend/internal/platform/observability"
	"github.com/monaco/monaco/apps/backend/internal/testkit"
)

type firstDepositFixture struct {
	pool *pgxpool.Pool
	uow  *db.UnitOfWork
	ids  *testkit.IDs
	at   time.Time
	logs *bytes.Buffer
}

func newFirstDepositFixture(t *testing.T) firstDepositFixture {
	t.Helper()
	pool := testkit.DB(t)
	at := clock.Real{}.Now().UTC().Truncate(time.Microsecond)
	return firstDepositFixture{
		pool: pool, uow: db.New(pool, testkit.NewIDs(602), testkit.NewClock(at)), ids: testkit.NewIDs(6020),
		at: at, logs: &bytes.Buffer{},
	}
}

func (f firstDepositFixture) deposit(user ids.UserID, micros uint64, blockTime *time.Time) events.DepositCredited {
	return events.DepositCredited{
		V: 1, DepositID: f.ids.NewV7(), UserID: user.UUID(), WalletAddress: chain.SolanaAddress("wallet"),
		AmountMicros: money.MicrosFromUint64(micros), TxSignature: chain.Signature("sig"), Slot: 42,
		BlockTime: blockTime,
	}
}

func (f firstDepositFixture) handle(t *testing.T, e events.DepositCredited) {
	t.Helper()
	ctx := observability.WithLogger(t.Context(), observability.NewLogger(config.Config{Env: config.EnvTest}, f.logs))
	if err := f.uow.Do(ctx, func(ctx context.Context, tx db.Tx) error {
		return adapters.FirstDeposit{}.Handle(ctx, tx, e, f.at)
	}); err != nil {
		t.Fatalf("handle deposit of %s micros: %v", e.AmountMicros, err)
	}
}

func (f firstDepositFixture) firstDepositAt(t *testing.T, user ids.UserID) *time.Time {
	t.Helper()
	var got *time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT first_deposit_at FROM users WHERE id = $1`, user.UUID()).
		Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestFirstDepositBelowThresholdLeavesTheColumnNull(t *testing.T) {
	t.Parallel()
	f := newFirstDepositFixture(t)
	user := testkit.SeedUser(t, f.pool, testkit.UserOpts{})
	block := f.at.Add(-time.Hour)
	f.handle(t, f.deposit(user.ID, 9_990_000, &block))
	if got := f.firstDepositAt(t, user.ID); got != nil {
		t.Fatalf("first_deposit_at after a $9.99 deposit = %v, want null", got)
	}
	for _, want := range []string{
		`"msg":"identity.first_deposit.below_threshold"`, `"amount_micros":"9990000"`, `"threshold_micros":"10000000"`,
	} {
		if !strings.Contains(f.logs.String(), want) {
			t.Fatalf("logs lack %s:\n%s", want, f.logs)
		}
	}
}

func TestFirstDepositAtTheThresholdRecordsBlockTimeAndKeepsIt(t *testing.T) {
	t.Parallel()
	f := newFirstDepositFixture(t)
	user := testkit.SeedUser(t, f.pool, testkit.UserOpts{})
	first, later := f.at.Add(-2*time.Hour), f.at.Add(-time.Hour)
	f.handle(t, f.deposit(user.ID, 10_000_000, &first))
	if got := f.firstDepositAt(t, user.ID); got == nil || !got.Equal(first) {
		t.Fatalf("first_deposit_at after a $10.00 deposit = %v, want block time %v", got, first)
	}
	f.handle(t, f.deposit(user.ID, 50_000_000, &later))
	if got := f.firstDepositAt(t, user.ID); got == nil || !got.Equal(first) {
		t.Fatalf("first_deposit_at after a second $50 deposit = %v, want the first %v", got, first)
	}
	if !strings.Contains(f.logs.String(), `"msg":"identity.first_deposit.already_set"`) ||
		!strings.Contains(f.logs.String(), `"user_id":"`+user.ID.String()+`"`) {
		t.Fatalf("logs lack identity.first_deposit.already_set for %s:\n%s", user.ID, f.logs)
	}
	earlier := first.Add(-time.Hour)
	f.handle(t, f.deposit(user.ID, 20_000_000, &earlier))
	if got := f.firstDepositAt(t, user.ID); got == nil || !got.Equal(earlier) {
		t.Fatalf("first_deposit_at after an earlier $20 deposit = %v, want %v", got, earlier)
	}
}

func TestFirstDepositWithoutBlockTimeUsesTheDeliveryTime(t *testing.T) {
	t.Parallel()
	f := newFirstDepositFixture(t)
	user := testkit.SeedUser(t, f.pool, testkit.UserOpts{})
	f.handle(t, f.deposit(user.ID, 25_000_000, nil))
	if got := f.firstDepositAt(t, user.ID); got == nil || !got.Equal(f.at) {
		t.Fatalf("first_deposit_at without a block time = %v, want delivery time %v", got, f.at)
	}
}

func TestFirstDepositForADeletedUserWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFirstDepositFixture(t)
	user := testkit.SeedUser(t, f.pool, testkit.UserOpts{AccountStatus: "deleted"})
	block := f.at.Add(-time.Hour)
	f.handle(t, f.deposit(user.ID, 25_000_000, &block))
	if got := f.firstDepositAt(t, user.ID); got != nil {
		t.Fatalf("first_deposit_at for a deleted user = %v, want null", got)
	}
}

func TestFirstDepositReturnsTheDatabaseError(t *testing.T) {
	t.Parallel()
	f := newFirstDepositFixture(t)
	user := testkit.SeedUser(t, f.pool, testkit.UserOpts{})
	block := f.at.Add(-time.Hour)
	e := f.deposit(user.ID, 25_000_000, &block)
	err := f.uow.Do(t.Context(), func(ctx context.Context, tx db.Tx) error {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		return adapters.FirstDeposit{}.Handle(canceled, tx, e, f.at)
	})
	if err == nil {
		t.Fatal("handle with a canceled context = nil, want the update's error")
	}
	if got := f.firstDepositAt(t, user.ID); got != nil {
		t.Fatalf("first_deposit_at after a failed update = %v, want null", got)
	}
}

func TestFirstDepositSeedReplaysA25USDCDepositThroughTheConsumer(t *testing.T) {
	t.Parallel()
	f := newFirstDepositFixture(t)
	user := testkit.SeedUser(t, f.pool, testkit.UserOpts{})
	block := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	payload, err := json.Marshal(f.deposit(user.ID, 25_000_000, &block))
	if err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"id":%q,"type":"deposit.credited","actor":"system:funding",`+
		`"created_at":"2026-03-01T12:00:05Z","payload":%s}`, f.ids.NewV7(), payload)
	consumers := identity.New(module.Deps{Pool: f.pool}).Consumers()
	testkit.SeedJSONL(t, f.pool, "user-with-first-deposit", []byte(line), consumers...)
	if got := f.firstDepositAt(t, user.ID); got == nil || !got.Equal(block) {
		t.Fatalf("first_deposit_at after the seeded 25 USDC deposit = %v, want %v", got, block)
	}
}
