package faker

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

type seedEnv struct {
	store    *postgres.Store
	iso      *postgres.TestIsolation
	privy    privy.Client
	seeder   *Seeder
	home     *app.HomeService
	token    string
	operator string
	groupID  string
}

func newSeedEnv(t *testing.T) seedEnv {
	t.Helper()
	db := postgres.OpenTestDB(t)
	iso := postgres.PrepareTestDB(t, db)
	store := postgres.NewStore(db)
	privyClient := privy.NewFakeClient()
	ctx := context.Background()

	token := iso.UniqueToken("operator")
	privy.RegisterToken(privyClient, privy.AccessToken(token), privy.Identity{PrivyUserID: iso.UniquePrivyID("operator"), DisplayName: "Operator"})
	session, err := app.NewSessionService(store, privyClient).OpenSession(ctx, token)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	iso.TrackUser(session.UserID)
	group, err := app.NewGovernanceService(store, privyClient).CreateGroupWithRules(ctx, token, "Operator Club "+iso.Suffix(), app.DefaultGroupRules())
	if err != nil {
		t.Fatalf("CreateGroupWithRules: %v", err)
	}
	iso.TrackGroup(group.GroupID)

	symbols := app.NewSymbolResolver(nil)
	pythClient := pyth.NewFakeClient()
	deposits := app.NewDepositService(store, privyClient, pythClient, symbols)
	fixed := time.Date(2026, 9, 18, 15, 0, 0, 0, time.UTC)
	return seedEnv{
		store: store, iso: iso, privy: privyClient,
		seeder:   NewSeeder(store, nil).WithPrefix("test-" + iso.Suffix() + "-").WithClock(func() time.Time { return fixed }),
		home:     app.NewHomeService(store, privyClient, pythClient, deposits, symbols),
		token:    token,
		operator: session.UserID,
		groupID:  group.GroupID,
	}
}

func (e seedEnv) track(mixed MixedResult, scale ScaleResult) {
	for _, id := range mixed.UserIDs {
		e.iso.TrackUser(id)
	}
	for _, c := range scale.Clubs {
		e.iso.TrackGroup(c.GroupID)
		for _, id := range c.UserIDs {
			e.iso.TrackUser(id)
		}
	}
}

func countRows(t *testing.T, e seedEnv, q string, args ...any) int {
	t.Helper()
	tx, err := e.store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestSeedMixedAndScale_idempotentAndInert(t *testing.T) {
	e := newSeedEnv(t)
	ctx := context.Background()

	mixed, err := e.seeder.SeedMixed(ctx, e.groupID)
	if err != nil {
		t.Fatalf("SeedMixed: %v", err)
	}
	scale, err := e.seeder.SeedScale(ctx)
	e.track(mixed, scale)
	if err != nil {
		t.Fatalf("SeedScale: %v", err)
	}
	if len(mixed.UserIDs) != 3 || len(scale.Clubs) != 3 {
		t.Fatalf("seeded %d ghosts, %d clubs; want 3 and 3", len(mixed.UserIDs), len(scale.Clubs))
	}

	snapshot := func() [6]int {
		clubIDs := []string{scale.Clubs[0].GroupID, scale.Clubs[1].GroupID, scale.Clubs[2].GroupID}
		return [6]int{
			countRows(t, e, `SELECT count(*) FROM users WHERE privy_user_id LIKE $1`, "faker:user:test-"+e.iso.Suffix()+"-%"),
			countRows(t, e, `SELECT count(*) FROM groups WHERE faker_key LIKE $1`, "scale:test-"+e.iso.Suffix()+"-%"),
			countRows(t, e, `SELECT count(*) FROM deposits WHERE group_id = ANY($1::uuid[]) OR group_id = $2`, clubIDs, e.groupID),
			countRows(t, e, `SELECT count(*) FROM proposals WHERE group_id = ANY($1::uuid[]) OR group_id = $2`, clubIDs, e.groupID),
			countRows(t, e, `SELECT count(*) FROM transactions WHERE group_id = ANY($1::uuid[]) OR group_id = $2`, clubIDs, e.groupID),
			countRows(t, e, `SELECT count(*) FROM group_members WHERE group_id = $1`, e.groupID),
		}
	}
	first := snapshot()

	// Re-run both profiles: same clubs, users, and row counts.
	mixed2, err := e.seeder.SeedMixed(ctx, e.groupID)
	if err != nil {
		t.Fatalf("SeedMixed rerun: %v", err)
	}
	scale2, err := e.seeder.SeedScale(ctx)
	e.track(mixed2, scale2)
	if err != nil {
		t.Fatalf("SeedScale rerun: %v", err)
	}
	if second := snapshot(); second != first {
		t.Fatalf("rerun counts = %v, want %v (idempotent)", second, first)
	}
	for i := range scale.Clubs {
		if scale.Clubs[i].GroupID != scale2.Clubs[i].GroupID {
			t.Fatalf("club %d id changed on rerun", i)
		}
	}
	if first[5] != 4 {
		t.Errorf("real club members = %d, want operator + 3 ghosts", first[5])
	}
	if first[4] != 5 {
		t.Errorf("transactions = %d, want 3 buys + 2 sells in scale clubs, none in mixed", first[4])
	}

	// Never any member wallets for faker users, never FAKE* treasuries on faker clubs.
	if n := countRows(t, e, `SELECT count(*) FROM member_wallets w JOIN users u ON u.id = w.user_id WHERE u.is_faker`); n != 0 {
		t.Errorf("faker member_wallets = %d, want 0", n)
	}
	if n := countRows(t, e, `SELECT count(*) FROM treasuries t JOIN groups g ON g.id = t.group_id WHERE g.is_faker AND t.solana_address LIKE 'FAKE%'`); n != 0 {
		t.Errorf("FAKE* faker treasuries = %d, want 0", n)
	}
	// No faker transactions in the mixed (real) club.
	if n := countRows(t, e, `SELECT count(*) FROM transactions WHERE group_id = $1`, e.groupID); n != 0 {
		t.Errorf("mixed club transactions = %d, want 0", n)
	}

	// Ghost shares stay out of the real pot; operator not a member of scale clubs.
	realShares, err := e.store.SumShareUnitsByGroup(ctx, e.groupID)
	if err != nil || realShares != 0 {
		t.Errorf("real pot shares = %d (err %v), want 0", realShares, err)
	}
	for _, c := range scale.Clubs {
		if member, _ := e.store.IsGroupMember(ctx, c.GroupID, e.operator); member {
			t.Errorf("operator is member of %s", c.Name)
		}
	}

	// Surfaces: operator home lists scale clubs; club view readable with activity and board.
	home, err := e.home.GetHome(ctx, e.token)
	if err != nil {
		t.Fatalf("GetHome: %v", err)
	}
	onHome := map[string]bool{}
	for _, g := range home.Groups {
		onHome[g.GroupID] = true
	}
	for _, c := range scale.Clubs {
		if !onHome[c.GroupID] {
			t.Errorf("home missing %s", c.Name)
		}
		view, err := e.home.GetGroupView(ctx, e.token, c.GroupID)
		if err != nil {
			t.Fatalf("GetGroupView(%s): %v", c.Name, err)
		}
		// Sanity: pot NAV (ledger USDC + 8-decimal holdings at cost) sits near net deposits,
		// never 100x off. Clubs take in 8.5k-14k USDC.
		if pot, err := strconv.ParseFloat(view.PotTotalUsd, 64); err != nil || pot < 7_000 || pot > 16_000 {
			t.Errorf("%s pot total = %s, want within 7000-16000 USD", c.Name, view.PotTotalUsd)
		}
		if len(view.Members) != 6 || view.TreasuryAddress != "" {
			t.Errorf("%s view members=%d treasury=%q", c.Name, len(view.Members), view.TreasuryAddress)
		}
		activity, err := e.home.ListGroupActivity(ctx, e.token, c.GroupID)
		if err != nil || len(activity) < 7 {
			t.Errorf("%s activity rows=%d err=%v", c.Name, len(activity), err)
		}
		if n := countRows(t, e, `SELECT count(*) FROM nav_snapshots WHERE group_id = $1`, c.GroupID); n < 10 {
			t.Errorf("%s nav snapshots = %d, want a week of history", c.Name, n)
		}
		if n := countRows(t, e, `SELECT count(*) FROM nav_snapshots WHERE group_id = $1 AND net_contributed_micros IS NULL`, c.GroupID); n != 0 {
			t.Errorf("%s nav snapshots missing net contributed = %d, want 0", c.Name, n)
		}
	}
	realView, err := e.home.GetGroupView(ctx, e.token, e.groupID)
	if err != nil {
		t.Fatalf("GetGroupView(real): %v", err)
	}
	if len(realView.Members) != 4 {
		t.Errorf("real club board = %d rows, want operator + 3 ghosts", len(realView.Members))
	}
}

func TestSeedMixed_rejectsFakerAndMissingGroups(t *testing.T) {
	e := newSeedEnv(t)
	ctx := context.Background()
	scale, err := e.seeder.SeedScale(ctx)
	e.track(MixedResult{}, scale)
	if err != nil {
		t.Fatalf("SeedScale: %v", err)
	}
	if _, err := e.seeder.SeedMixed(ctx, scale.Clubs[0].GroupID); !errors.Is(err, ErrGroupIsFaker) {
		t.Errorf("SeedMixed(faker club) err = %v, want ErrGroupIsFaker", err)
	}
	if _, err := e.seeder.SeedMixed(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("SeedMixed(missing) err = %v, want ErrGroupNotFound", err)
	}
}
