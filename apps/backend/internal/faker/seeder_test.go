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
	if len(mixed.UserIDs) != 3 || len(scale.Clubs) != len(scaleClubs) || len(scaleClubs) != 6 {
		t.Fatalf("seeded %d ghosts, %d clubs; want 3 and 6", len(mixed.UserIDs), len(scale.Clubs))
	}

	snapshot := func() [6]int {
		var clubIDs []string
		for _, c := range scale.Clubs {
			clubIDs = append(clubIDs, c.GroupID)
		}
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
	// Like the API, a proposal without a thesis stores NULL, never an empty string.
	if n := countRows(t, e, `SELECT count(*) FROM proposals WHERE thesis = '' AND group_id = $1`, e.groupID); n != 0 {
		t.Errorf("faker proposals with empty-string thesis = %d, want 0 (NULL)", n)
	}
	if n := countRows(t, e, `SELECT count(*) FROM proposals WHERE thesis IS NULL AND group_id = $1`, e.groupID); n != 2 {
		t.Errorf("faker proposals without thesis = %d, want the failed and expired ones", n)
	}
	if first[5] != 4 {
		t.Errorf("real club members = %d, want operator + 3 ghosts", first[5])
	}
	if first[4] != 8 {
		t.Errorf("transactions = %d, want 6 buys + 2 sells in scale clubs, none in mixed", first[4])
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
	for i, c := range scale.Clubs {
		spec := scaleClubs[i]
		var netIn float64
		for _, d := range spec.Deposits {
			netIn += float64(d.USDC)
		}
		if !onHome[c.GroupID] {
			t.Errorf("home missing %s", c.Name)
		}
		view, err := e.home.GetGroupView(ctx, e.token, c.GroupID)
		if err != nil {
			t.Fatalf("GetGroupView(%s): %v", c.Name, err)
		}
		// Sanity: pot NAV (ledger USDC + 8-decimal holdings at cost) sits near net deposits,
		// never 100x off.
		if pot, err := strconv.ParseFloat(view.PotTotalUsd, 64); err != nil || pot < netIn*0.6 || pot > netIn*1.6 {
			t.Errorf("%s pot total = %s, want within 60-160%% of %.0f USD in", c.Name, view.PotTotalUsd, netIn)
		}
		if len(view.Members) != 1+len(spec.Members) || view.TreasuryAddress != "" {
			t.Errorf("%s view members=%d treasury=%q", c.Name, len(view.Members), view.TreasuryAddress)
		}
		activity, err := e.home.ListGroupActivity(ctx, e.token, c.GroupID)
		if err != nil || len(activity) < len(spec.Deposits)+1 {
			t.Errorf("%s activity rows=%d err=%v", c.Name, len(activity), err)
		}
		t.Logf("%s pot %s from %.0f in", c.Name, view.PotTotalUsd, netIn)
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

func TestSeedDemo_seedsConversationAndStaysIdempotent(t *testing.T) {
	e := newSeedEnv(t)
	ctx := context.Background()
	seeder := e.seeder.WithPhotoBaseURL("https://cdn.example.test/avatars/faker/")

	// A real, votable proposal by the operator (the demo's account B stands in here).
	var realProposal string
	tx, err := e.store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	err = tx.QueryRowContext(ctx, `
INSERT INTO proposals (group_id, proposer_id, symbol, kind, usdc_micros, status, expires_at, thesis)
VALUES ($1, $2, 'AAPLx', 'buy', 50000000, 'open', now() + interval '22 hours', 'Earnings Thursday.') RETURNING id`,
		e.groupID, e.operator).Scan(&realProposal)
	if err == nil {
		err = tx.Commit()
	} else {
		_ = tx.Rollback()
	}
	if err != nil {
		t.Fatalf("insert real proposal: %v", err)
	}

	opts := MixedOptions{Demo: true, ProposalID: realProposal}
	demo, err := seeder.SeedMixed(ctx, e.groupID, opts)
	e.track(demo, ScaleResult{})
	if err != nil {
		t.Fatalf("SeedMixed(demo): %v", err)
	}

	counts := func() [6]int {
		return [6]int{
			countRows(t, e, `SELECT count(*) FROM group_messages WHERE group_id = $1`, e.groupID),
			countRows(t, e, `SELECT count(*) FROM proposal_comments c JOIN proposals p ON p.id = c.proposal_id WHERE p.group_id = $1`, e.groupID),
			countRows(t, e, `SELECT count(*) FROM proposal_comments WHERE proposal_id = $1`, realProposal),
			countRows(t, e, `SELECT count(*) FROM deposits WHERE group_id = $1 AND status <> 'confirmed'`, e.groupID),
			countRows(t, e, `SELECT count(*) FROM proposals WHERE group_id = $1 AND status IN ('failed', 'expired')`, e.groupID),
			countRows(t, e, `SELECT count(*) FROM proposals WHERE group_id = $1`, e.groupID),
		}
	}
	first := counts()
	if first[0] != 6 {
		t.Errorf("messages = %d, want 6", first[0])
	}
	if first[1] != 3 || first[2] != 2 {
		t.Errorf("comments = %d (real proposal %d), want 3 (2 on the real proposal, 1 on the ghost)", first[1], first[2])
	}
	if first[3] != 0 {
		t.Errorf("pending/failed ghost deposits = %d, want 0 in demo", first[3])
	}
	if first[4] != 0 {
		t.Errorf("failed/expired proposals = %d, want 0 in demo", first[4])
	}
	if first[5] != 3 {
		t.Errorf("proposals = %d, want real + ghost open + ghost passed", first[5])
	}
	if len(demo.CommentIDs) != 3 || demo.Messages != 6 || !demo.Demo {
		t.Errorf("result = %+v, want 3 comment ids, 6 messages, demo", demo)
	}

	// Thesis on the ghost open proposal; the reply threads under Maya's question.
	if n := countRows(t, e, `SELECT count(*) FROM proposals p JOIN users u ON u.id = p.proposer_id
		WHERE p.group_id = $1 AND u.is_faker AND p.status = 'open' AND p.thesis <> ''`, e.groupID); n != 1 {
		t.Errorf("ghost open proposals with thesis = %d, want 1", n)
	}
	if n := countRows(t, e, `SELECT count(*) FROM proposal_comments c JOIN proposal_comments parent ON parent.id = c.parent_comment_id
		WHERE c.proposal_id = $1`, realProposal); n != 1 {
		t.Errorf("threaded replies on the real proposal = %d, want 1", n)
	}
	// Timestamps are recent and never in the future.
	if n := countRows(t, e, `SELECT count(*) FROM group_messages WHERE group_id = $1
		AND created_at BETWEEN $2::timestamptz - interval '90 minutes' AND $2::timestamptz`, e.groupID, time.Date(2026, 9, 18, 15, 0, 0, 0, time.UTC)); n != 6 {
		t.Errorf("messages within the last 90 minutes of seed time = %d, want 6", n)
	}
	// Photos only when a base URL is configured.
	if n := countRows(t, e, `SELECT count(*) FROM users WHERE id = ANY($1::uuid[]) AND profile_photo_url LIKE 'https://cdn.example.test/avatars/faker/%.jpg'`, demo.UserIDs); n != 3 {
		t.Errorf("ghost photos = %d, want 3", n)
	}

	// A real member replies to a ghost comment; the re-run must keep it.
	tx, err = e.store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO proposal_comments (proposal_id, author_id, parent_comment_id, body) VALUES ($1, $2, $3, 'Agreed.')`,
		realProposal, e.operator, demo.CommentIDs[1])
	if err == nil {
		err = tx.Commit()
	} else {
		_ = tx.Rollback()
	}
	if err != nil {
		t.Fatalf("insert real reply: %v", err)
	}

	again, err := seeder.SeedMixed(ctx, e.groupID, opts)
	e.track(again, ScaleResult{})
	if err != nil {
		t.Fatalf("SeedMixed(demo) rerun: %v", err)
	}
	second := counts()
	want := first
	want[1]++ // the operator's reply survives
	want[2]++
	if second != want {
		t.Fatalf("rerun counts = %v, want %v", second, want)
	}

	// Plain mixed afterwards drops the chat and comments again and restores its own rows.
	mixed, err := e.seeder.SeedMixed(ctx, e.groupID)
	e.track(mixed, ScaleResult{})
	if err != nil {
		t.Fatalf("SeedMixed after demo: %v", err)
	}
	if n := countRows(t, e, `SELECT count(*) FROM group_messages WHERE group_id = $1`, e.groupID); n != 0 {
		t.Errorf("messages after mixed = %d, want 0", n)
	}
	if n := countRows(t, e, `SELECT count(*) FROM users WHERE id = ANY($1::uuid[]) AND profile_photo_url IS NOT NULL`, mixed.UserIDs); n != 0 {
		t.Errorf("ghost photos without a base URL = %d, want 0", n)
	}
}

func TestSeedDemo_rejectsGhostOrForeignProposal(t *testing.T) {
	e := newSeedEnv(t)
	ctx := context.Background()
	mixed, err := e.seeder.SeedMixed(ctx, e.groupID)
	e.track(mixed, ScaleResult{})
	if err != nil {
		t.Fatalf("SeedMixed: %v", err)
	}
	if _, err := e.seeder.SeedMixed(ctx, e.groupID, MixedOptions{Demo: true, ProposalID: mixed.ProposalIDs[0]}); !errors.Is(err, ErrProposalNotReal) {
		t.Errorf("ghost proposal err = %v, want ErrProposalNotReal", err)
	}
	if _, err := e.seeder.SeedMixed(ctx, e.groupID, MixedOptions{Demo: true, ProposalID: "00000000-0000-0000-0000-000000000000"}); !errors.Is(err, ErrProposalNotInGroup) {
		t.Errorf("unknown proposal err = %v, want ErrProposalNotInGroup", err)
	}
}

// Every seeded thesis must pass the same limit the API and the proposals_thesis_length_check
// constraint enforce.
func TestProfiles_thesesFitTheProposalLimit(t *testing.T) {
	check := func(where string, specs []proposalSpec) {
		for _, p := range specs {
			if len(p.Thesis) > app.MaxProposalThesisLength {
				t.Errorf("%s proposal %q thesis is %d bytes, over %d", where, p.Key, len(p.Thesis), app.MaxProposalThesisLength)
			}
		}
	}
	check("mixed", mixedProposals)
	for _, club := range scaleClubs {
		check(club.Key, club.Proposals)
	}
}
