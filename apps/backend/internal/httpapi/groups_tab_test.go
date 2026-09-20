package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/chainlink"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/marks"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

type groupsTabHarness struct {
	tab    *GroupsTabHandlers
	auth   *AuthHandlers
	groups *GroupHandlers
	privy  wallets.Client
	pyth   marks.Client
	store  *postgres.Store
	iso    *postgres.TestIsolation
}

func newGroupsTabHarness(t *testing.T) groupsTabHarness {
	t.Helper()
	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	pythClient := chainlink.NewFakeClient()
	symbols := app.NewSymbolResolver(nil)
	deposits := app.NewDepositService(store, authHandlers.Verifier, privyClient, pythClient, symbols)
	home := app.NewHomeService(store, authHandlers.Verifier, privyClient, pythClient, deposits, symbols)
	return groupsTabHarness{
		tab:    &GroupsTabHandlers{GroupsTab: app.NewGroupsTabService(home, store)},
		auth:   authHandlers,
		groups: &GroupHandlers{Groups: app.NewGroupService(store, authHandlers.Verifier, privyClient), Governance: app.NewGovernanceService(store, authHandlers.Verifier, privyClient)},
		privy:  privyClient,
		pyth:   pythClient,
		store:  store,
		iso:    iso,
	}
}

func (h groupsTabHarness) user(t *testing.T, label, name string) (authSessionResponse, auth.AccessToken) {
	t.Helper()
	return seedAuthenticatedUser(t, h.iso, h.auth, h.privy, label, name)
}

func (h groupsTabHarness) createGroup(t *testing.T, token auth.AccessToken, name, joinMode string) createGroupResponse {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"joinPolicy":{"mode":%q}}`, name, joinMode)
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	h.groups.CreateGroupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create group status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var created createGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create group: %v", err)
	}
	trackCreatedGroup(h.iso, created.GroupID)
	return created
}

// fundUSDCOnly credits shareMicros share units against depositedMicros net in
// and sets the treasury balance. A USDC-only pot is valued at min(treasury,
// shares), so shares > deposits models a pot that already gained.
func (h groupsTabHarness) fundUSDCOnly(t *testing.T, userID string, group createGroupResponse, shareMicros, depositedMicros, treasuryMicros int64) {
	t.Helper()
	ctx := context.Background()
	tx, err := h.store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := h.store.IncrementPositionTx(ctx, tx, userID, group.GroupID, shareMicros, depositedMicros); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	wallets.SetTreasuryUSDCBalance(h.privy, group.TreasuryAddress, treasuryMicros)
}

func (h groupsTabHarness) get(t *testing.T, handler http.HandlerFunc, target string, token auth.AccessToken, pathID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	if pathID != "" {
		req.SetPathValue("id", pathID)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, want, rec.Body.String())
	}
}

// ---- auth and validation -------------------------------------------------

func TestGroupsTabRoutes_missingOrInvalidAuth_return401(t *testing.T) {
	t.Parallel()
	h := newGroupsTabHarness(t)
	routes := []struct {
		name    string
		handler http.HandlerFunc
		target  string
		id      string
	}{
		{"search", h.tab.SearchGroupsHandler, "/v1/groups/search?q=club", ""},
		{"leaderboard", h.tab.GroupLeaderboardHandler, "/v1/groups/leaderboard", ""},
		{"my pnl", h.tab.MyGroupsPnLHistoryHandler, "/v1/groups/pnl-history", ""},
		{"group pnl", h.tab.GroupPnLHistoryHandler, "/v1/groups/550e8400-e29b-41d4-a716-446655440001/pnl-history", "550e8400-e29b-41d4-a716-446655440001"},
	}
	for _, route := range routes {
		t.Run(route.name+" missing", func(t *testing.T) {
			assertStatus(t, h.get(t, route.handler, route.target, "", route.id), http.StatusUnauthorized)
		})
		t.Run(route.name+" invalid", func(t *testing.T) {
			assertStatus(t, h.get(t, route.handler, route.target, "not-a-real-token", route.id), http.StatusUnauthorized)
		})
	}
}

func TestGET_groupsSearch_rejectsBadQueryLimitAndCursor(t *testing.T) {
	t.Parallel()
	h := newGroupsTabHarness(t)
	_, token := h.user(t, "search-bad", "Ada")
	long := strings.Repeat("a", 65)
	cases := map[string]string{
		"missing q":      "/v1/groups/search",
		"one rune":       "/v1/groups/search?q=a",
		"padded one":     "/v1/groups/search?q=%20a%20",
		"too long":       "/v1/groups/search?q=" + long,
		"zero limit":     "/v1/groups/search?q=club&limit=0",
		"negative limit": "/v1/groups/search?q=club&limit=-3",
		"text limit":     "/v1/groups/search?q=club&limit=ten",
		"forged cursor":  "/v1/groups/search?q=club&cursor=bm90LWEtY3Vyc29y",
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			assertStatus(t, h.get(t, h.tab.SearchGroupsHandler, target, token, ""), http.StatusBadRequest)
		})
	}
}

func TestGET_groupsSearch_twoRuneUnicodeQueryIsAccepted(t *testing.T) {
	t.Parallel()
	h := newGroupsTabHarness(t)
	_, token := h.user(t, "search-unicode", "Ada")
	rec := h.get(t, h.tab.SearchGroupsHandler, "/v1/groups/search?q=%C3%A9%C3%A9", token, "")
	assertStatus(t, rec, http.StatusOK)
}

// ---- search ---------------------------------------------------------------

func TestGET_groupsSearch_returnsPublicRowsWithJoinedFlagAndJoinMode(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newGroupsTabHarness(t)
	ada, adaToken := h.user(t, "search-ada", "Ada")
	_, benToken := h.user(t, "search-ben", "Ben")
	token := "zs" + h.iso.Suffix()
	mine := h.createGroup(t, adaToken, token+" Weekend", "open")
	theirs := h.createGroup(t, benToken, "The "+strings.ToUpper(token)+" desk", "request")
	h.fundUSDCOnly(t, ada.UserID, mine, 12_000_000, 10_000_000, 12_000_000)

	// Act
	rec := h.get(t, h.tab.SearchGroupsHandler, "/v1/groups/search?q="+token, adaToken, "")

	// Assert
	assertStatus(t, rec, http.StatusOK)
	payload := decodeBody[groupSearchResponse](t, rec)
	if len(payload.Groups) != 2 || payload.NextCursor != nil {
		t.Fatalf("groups = %+v next = %v; want 2 rows, no next page", payload.Groups, payload.NextCursor)
	}
	first, second := payload.Groups[0], payload.Groups[1]
	if first.GroupID != mine.GroupID || !first.IsJoined || first.JoinMode != "open" {
		t.Fatalf("first = %+v; want viewer's open cabal (prefix match ranks first)", first)
	}
	if first.PotValueUsd != "12.00" || first.DollarPnL != "+2.00" || first.PercentReturn == nil || *first.PercentReturn != "0.2" {
		t.Fatalf("first money = %+v; want pot 12.00, +2.00, 0.2", first)
	}
	if first.MemberCount != 1 {
		t.Fatalf("member count = %d, want 1", first.MemberCount)
	}
	if second.GroupID != theirs.GroupID || second.IsJoined || second.JoinMode != "request" {
		t.Fatalf("second = %+v; want Ben's approval cabal, not joined", second)
	}
	if second.PercentReturn != nil {
		t.Fatalf("unfunded percent = %v, want nil", *second.PercentReturn)
	}
	body := rec.Body.String()
	for _, secret := range []string{mine.TreasuryAddress, theirs.TreasuryAddress, ada.UserID} {
		if strings.Contains(body, secret) {
			t.Fatalf("search response leaked %q: %s", secret, body)
		}
	}
}

func TestGET_groupsSearch_cursorWalksEveryMatchOnce(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newGroupsTabHarness(t)
	_, token := h.user(t, "search-page", "Ada")
	q := "zg" + h.iso.Suffix()
	want := map[string]bool{}
	for _, suffix := range []string{"a", "b", "c"} {
		want[h.createGroup(t, token, q+" "+suffix, "open").GroupID] = true
	}

	// Act
	seen := map[string]bool{}
	target := "/v1/groups/search?limit=2&q=" + q
	for page := 0; page < 5; page++ {
		rec := h.get(t, h.tab.SearchGroupsHandler, target, token, "")
		assertStatus(t, rec, http.StatusOK)
		payload := decodeBody[groupSearchResponse](t, rec)
		for _, row := range payload.Groups {
			if seen[row.GroupID] {
				t.Fatalf("group %s returned twice", row.GroupID)
			}
			seen[row.GroupID] = true
		}
		if payload.NextCursor == nil {
			break
		}
		target = "/v1/groups/search?limit=2&q=" + q + "&cursor=" + *payload.NextCursor
	}

	// Assert
	if len(seen) != len(want) {
		t.Fatalf("saw %d groups, want %d", len(seen), len(want))
	}
}

func TestGET_groupsSearch_limitAboveMaxIsCapped(t *testing.T) {
	t.Parallel()
	h := newGroupsTabHarness(t)
	_, token := h.user(t, "search-cap", "Ada")
	rec := h.get(t, h.tab.SearchGroupsHandler, "/v1/groups/search?limit=5000&q=zz"+h.iso.Suffix(), token, "")
	assertStatus(t, rec, http.StatusOK)
	if got := decodeBody[groupSearchResponse](t, rec); got.Groups == nil {
		t.Fatal("expected an empty groups array, not null")
	}
}

// ---- leaderboard ------------------------------------------------------------

func TestGET_groupsLeaderboard_ranksByPercentThenDollarPnL(t *testing.T) {
	t.Parallel()
	// Arrange: three funded cabals far above any other test's returns.
	h := newGroupsTabHarness(t)
	ada, adaToken := h.user(t, "lb-ada", "Ada")
	ben, benToken := h.user(t, "lb-ben", "Ben")
	name := "lb" + h.iso.Suffix()
	bigTie := h.createGroup(t, adaToken, name+" big", "open")     // +900%, +$900
	smallTie := h.createGroup(t, benToken, name+" small", "open") // +900%, +$90
	third := h.createGroup(t, adaToken, name+" third", "request") // +800%
	unfunded := h.createGroup(t, adaToken, name+" empty", "open")
	h.fundUSDCOnly(t, ada.UserID, bigTie, 1_000_000_000, 100_000_000, 1_000_000_000)
	h.fundUSDCOnly(t, ben.UserID, smallTie, 100_000_000, 10_000_000, 100_000_000)
	h.fundUSDCOnly(t, ada.UserID, third, 90_000_000, 10_000_000, 90_000_000)

	// Act
	rec := h.get(t, h.tab.GroupLeaderboardHandler, "/v1/groups/leaderboard?limit=50", adaToken, "")

	// Assert
	assertStatus(t, rec, http.StatusOK)
	payload := decodeBody[groupLeaderboardResponse](t, rec)
	position := map[string]int{}
	for i, row := range payload.Groups {
		if row.Rank != i+1 {
			t.Fatalf("row %d rank = %d, want %d", i, row.Rank, i+1)
		}
		position[row.GroupID] = i
	}
	if _, ok := position[unfunded.GroupID]; ok {
		t.Fatal("unfunded cabal should not be ranked")
	}
	pb, ps, pt := position[bigTie.GroupID], position[smallTie.GroupID], position[third.GroupID]
	if !(pb < ps && ps < pt) {
		t.Fatalf("positions big=%d small=%d third=%d; want big < small < third", pb, ps, pt)
	}
	big := payload.Groups[pb]
	if !big.IsJoined || big.PotValueUsd != "1000.00" || big.DollarPnL != "+900.00" || *big.PercentReturn != "9" {
		t.Fatalf("big row = %+v", big)
	}
	if payload.Groups[ps].IsJoined {
		t.Fatal("Ben's cabal should not be joined for Ada")
	}
	if payload.Groups[pt].JoinMode != "request" {
		t.Fatalf("third join mode = %q, want request", payload.Groups[pt].JoinMode)
	}
	for _, secret := range []string{bigTie.TreasuryAddress, ben.UserID, ada.UserID} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("leaderboard leaked %q", secret)
		}
	}
}

func TestGET_groupsLeaderboard_respectsLimit(t *testing.T) {
	t.Parallel()
	h := newGroupsTabHarness(t)
	ada, token := h.user(t, "lb-limit", "Ada")
	for i := 0; i < 2; i++ {
		g := h.createGroup(t, token, fmt.Sprintf("lblimit%s %d", h.iso.Suffix(), i), "open")
		h.fundUSDCOnly(t, ada.UserID, g, 20_000_000, 10_000_000, 20_000_000)
	}
	rec := h.get(t, h.tab.GroupLeaderboardHandler, "/v1/groups/leaderboard?limit=1", token, "")
	assertStatus(t, rec, http.StatusOK)
	if got := decodeBody[groupLeaderboardResponse](t, rec); len(got.Groups) != 1 {
		t.Fatalf("rows = %d, want 1", len(got.Groups))
	}
}

func TestGET_groupsLeaderboard_invalidLimit_returns400(t *testing.T) {
	t.Parallel()
	h := newGroupsTabHarness(t)
	_, token := h.user(t, "lb-bad", "Ada")
	assertStatus(t, h.get(t, h.tab.GroupLeaderboardHandler, "/v1/groups/leaderboard?limit=abc", token, ""), http.StatusBadRequest)
}

func TestGET_groupsLeaderboard_pythOutageValuesStockAtCostBasis(t *testing.T) {
	t.Parallel()
	// Arrange: $100 funded, $60 of it in AAPLx; Pyth is down.
	h := newGroupsTabHarness(t)
	ada, token := h.user(t, "lb-pyth", "Ada")
	g := h.createGroup(t, token, "lbpyth"+h.iso.Suffix(), "open")
	h.fundUSDCOnly(t, ada.UserID, g, 100_000_000, 100_000_000, 40_000_000)
	if _, _, err := h.store.ConfirmBuyTransaction(context.Background(), postgres.ConfirmBuyTransactionParams{
		GroupID: g.GroupID, Amount: 60_000_000, InputToken: dex.USDCAddress(), OutputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash: "sig-lbpyth-" + h.iso.Suffix(), ExecuteRequestID: "req-lbpyth-" + h.iso.Suffix(),
		CostBasisPrice: 60_000_000, CostBasisAmount: 30_000_000,
	}); err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}
	chainlink.RegisterMarkedPotError(h.pyth, marks.TreasuryRef{GroupID: g.GroupID}, fmt.Errorf("hermes down"))

	// Act
	rec := h.get(t, h.tab.GroupLeaderboardHandler, "/v1/groups/leaderboard?limit=50", token, "")

	// Assert
	assertStatus(t, rec, http.StatusOK)
	for _, row := range decodeBody[groupLeaderboardResponse](t, rec).Groups {
		if row.GroupID == g.GroupID {
			if row.PotValueUsd != "100.00" || row.DollarPnL != "+0.00" {
				t.Fatalf("row = %+v; want pot 100.00 at cost basis", row)
			}
			return
		}
	}
	t.Fatal("cabal missing from leaderboard")
}

// ---- pnl history ------------------------------------------------------------

func TestGET_groupPnLHistory_badInputs(t *testing.T) {
	t.Parallel()
	h := newGroupsTabHarness(t)
	_, token := h.user(t, "pnl-bad", "Ada")
	unknown := "550e8400-e29b-41d4-a716-446655440099"
	cases := []struct {
		name   string
		target string
		id     string
		want   int
	}{
		{"malformed id", "/v1/groups/not-a-uuid/pnl-history", "not-a-uuid", http.StatusNotFound},
		{"sql-ish id", "/v1/groups/x/pnl-history", "1' OR '1'='1", http.StatusNotFound},
		{"unknown id", "/v1/groups/" + unknown + "/pnl-history", unknown, http.StatusNotFound},
		{"bad range", "/v1/groups/" + unknown + "/pnl-history?range=ALL", unknown, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStatus(t, h.get(t, h.tab.GroupPnLHistoryHandler, tc.target, token, tc.id), tc.want)
		})
	}
	assertStatus(t, h.get(t, h.tab.MyGroupsPnLHistoryHandler, "/v1/groups/pnl-history?range=5Y", token, ""), http.StatusBadRequest)
}

// TestGET_groupPnLHistory_fundBuyPriceMoveWithdrawal runs the real money path
// through the store: a $100 fund, a $60 buy of 0.3 AAPLx ($200/share), a 25%
// price move to $250, then a $23 payout. Expected P&L per point:
//
//	fund     pot 100 net 100 -> +0
//	buy      pot 100 net 100 -> +0   (snapshot marks stock at cost)
//	payout   pot  77 net  77 -> +0
//	live     pot  92 net  77 -> +15  (17 cash + 0.3 x $250)
func TestGET_groupPnLHistory_fundBuyPriceMoveWithdrawal(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newGroupsTabHarness(t)
	ada, adaToken := h.user(t, "pnl-ada", "Ada")
	_, benToken := h.user(t, "pnl-ben", "Ben")
	g := h.createGroup(t, adaToken, "pnl"+h.iso.Suffix(), "open")
	ctx := context.Background()
	sfx := h.iso.Suffix()

	deposit, err := h.store.InsertDeposit(ctx, ada.UserID, g.GroupID, 100_000_000, "member-"+sfx)
	if err != nil {
		t.Fatalf("InsertDeposit: %v", err)
	}
	tx, err := h.store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, _, err := h.store.ConfirmDepositTx(ctx, tx, deposit.ID, "sig-dep-"+sfx); err != nil {
		t.Fatalf("ConfirmDepositTx: %v", err)
	}
	if _, err := h.store.IncrementPositionTx(ctx, tx, ada.UserID, g.GroupID, 100_000_000, 100_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := h.store.WriteNavSnapshotOnDepositConfirmTx(ctx, tx, g.GroupID, 100_000_000); err != nil {
		t.Fatalf("deposit snapshot: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	const aaplAtomics = 30_000_000 // 0.3 share at 8 decimals
	if _, _, err := h.store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID: g.GroupID, Amount: 60_000_000, InputToken: dex.USDCAddress(), OutputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash: "sig-buy-" + sfx, ExecuteRequestID: "req-buy-" + sfx,
		CostBasisPrice: 60_000_000, CostBasisAmount: aaplAtomics,
	}); err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}
	if err := h.store.WriteNavSnapshotOnTransactionConfirm(ctx, g.GroupID, 40_000_000); err != nil {
		t.Fatalf("buy snapshot: %v", err)
	}

	withdrawal, err := h.store.InsertWithdrawal(ctx, ada.UserID, g.GroupID, 23_000_000, "payout-"+sfx)
	if err != nil {
		t.Fatalf("InsertWithdrawal: %v", err)
	}
	tx, err = h.store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, _, err := h.store.ConfirmWithdrawalPayoutTx(ctx, tx, withdrawal.ID, "sig-wd-"+sfx, 17_000_000); err != nil {
		t.Fatalf("ConfirmWithdrawalPayoutTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	wallets.SetTreasuryUSDCBalance(h.privy, g.TreasuryAddress, 17_000_000)
	chainlink.RegisterMarkedPot(h.pyth, marks.TreasuryRef{GroupID: g.GroupID}, marks.NavInput{
		Holdings: []marks.MarkedHolding{{
			Symbol: "AAPLx", Token: "0xb200000000000000000000c2e324d24d7eecd1fb", Units: aaplAtomics, MarkUsdc: 250_000_000, CostBasis: 60_000_000,
		}},
	})

	// Act: Ben is not a member; group-level P&L is public to signed-in users.
	rec := h.get(t, h.tab.GroupPnLHistoryHandler, "/v1/groups/"+g.GroupID+"/pnl-history?range=1D", benToken, g.GroupID)

	// Assert
	assertStatus(t, rec, http.StatusOK)
	payload := decodeBody[groupPnLSeriesResponse](t, rec)
	if payload.GroupID != g.GroupID || payload.Range != "1D" {
		t.Fatalf("payload header = %+v", payload)
	}
	want := []struct{ pot, net, pnl string }{
		{"100.00", "100.00", "+0.00"},
		{"100.00", "100.00", "+0.00"},
		{"77.00", "77.00", "+0.00"},
		{"92.00", "77.00", "+15.00"},
	}
	if len(payload.Points) != len(want) {
		t.Fatalf("points = %+v, want %d", payload.Points, len(want))
	}
	var prev time.Time
	for i, w := range want {
		p := payload.Points[i]
		if p.PotValueUsd != w.pot || p.NetInUsd != w.net || p.DollarPnL != w.pnl {
			t.Fatalf("point %d = %+v, want pot %s net %s pnl %s", i, p, w.pot, w.net, w.pnl)
		}
		at, err := time.Parse(time.RFC3339, p.At)
		if err != nil || !strings.HasSuffix(p.At, "Z") {
			t.Fatalf("point %d at = %q, want RFC3339 UTC", i, p.At)
		}
		if at.Before(prev) {
			t.Fatalf("point %d at %s precedes previous %s", i, at, prev)
		}
		prev = at
	}
}

func TestGET_groupPnLHistory_windowIsCappedAndBaselineCarriedForward(t *testing.T) {
	t.Parallel()
	// Arrange: snapshots 120 and 95 days ago; the 3M window starts 90 days ago.
	h := newGroupsTabHarness(t)
	ada, token := h.user(t, "pnl-window", "Ada")
	g := h.createGroup(t, token, "pnlwin"+h.iso.Suffix(), "open")
	h.fundUSDCOnly(t, ada.UserID, g, 50_000_000, 50_000_000, 50_000_000)
	ctx := context.Background()
	db := postgres.OpenTestDB(t)
	for _, snap := range []struct {
		daysAgo int
		pot     int64
	}{{120, 10_000_000}, {95, 50_000_000}} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO nav_snapshots (group_id, pot_nav_micros, nav_per_share_micros, total_shares, reason, created_at, net_contributed_micros)
VALUES ($1, $2, 1000000, $2, 'deposit', now() - make_interval(days => $3), $2)`, g.GroupID, snap.pot, snap.daysAgo); err != nil {
			t.Fatalf("insert snapshot: %v", err)
		}
	}

	// Act
	rec := h.get(t, h.tab.GroupPnLHistoryHandler, "/v1/groups/"+g.GroupID+"/pnl-history?range=3M", token, g.GroupID)

	// Assert
	assertStatus(t, rec, http.StatusOK)
	payload := decodeBody[groupPnLSeriesResponse](t, rec)
	if len(payload.Points) != 2 {
		t.Fatalf("points = %+v; want baseline at window start + live", payload.Points)
	}
	start, _ := time.Parse(time.RFC3339, payload.Points[0].At)
	if age := time.Since(start); age < 89*24*time.Hour || age > 91*24*time.Hour {
		t.Fatalf("first point is %s old, want ~90 days (window start)", age)
	}
	if payload.Points[0].PotValueUsd != "50.00" {
		t.Fatalf("baseline pot = %s, want the 95-day snapshot (50.00), not the 120-day one", payload.Points[0].PotValueUsd)
	}
}

func TestGET_myGroupsPnLHistory_returnsOneSeriesPerJoinedCabal(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newGroupsTabHarness(t)
	ada, adaToken := h.user(t, "mine-ada", "Ada")
	_, benToken := h.user(t, "mine-ben", "Ben")
	first := h.createGroup(t, adaToken, "mine1"+h.iso.Suffix(), "open")
	second := h.createGroup(t, adaToken, "mine2"+h.iso.Suffix(), "open")
	h.createGroup(t, benToken, "notmine"+h.iso.Suffix(), "open")
	h.fundUSDCOnly(t, ada.UserID, first, 10_000_000, 10_000_000, 10_000_000)

	// Act
	rec := h.get(t, h.tab.MyGroupsPnLHistoryHandler, "/v1/groups/pnl-history", adaToken, "")

	// Assert
	assertStatus(t, rec, http.StatusOK)
	payload := decodeBody[myGroupsPnLResponse](t, rec)
	if payload.Range != "1M" || len(payload.Series) != 2 {
		t.Fatalf("payload = %+v; want 1M with 2 series", payload)
	}
	if payload.Series[0].GroupID != first.GroupID || payload.Series[1].GroupID != second.GroupID {
		t.Fatalf("series order = %s, %s; want join order", payload.Series[0].GroupID, payload.Series[1].GroupID)
	}
	if len(payload.Series[0].Points) != 1 || payload.Series[0].Points[0].PotValueUsd != "10.00" {
		t.Fatalf("funded series = %+v; want a single live point", payload.Series[0].Points)
	}
	if payload.Series[1].Points == nil || len(payload.Series[1].Points) != 0 {
		t.Fatalf("unfunded series points = %v; want empty array", payload.Series[1].Points)
	}
}

func TestGET_myGroupsPnLHistory_noCabalsReturnsEmptySeries(t *testing.T) {
	t.Parallel()
	h := newGroupsTabHarness(t)
	_, token := h.user(t, "mine-none", "Ada")
	rec := h.get(t, h.tab.MyGroupsPnLHistoryHandler, "/v1/groups/pnl-history?range=1W", token, "")
	assertStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"series":[]`) {
		t.Fatalf("body = %s; want an empty series array", rec.Body.String())
	}
}
