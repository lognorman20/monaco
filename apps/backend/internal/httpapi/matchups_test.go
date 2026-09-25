package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

// stubMatchupMarks prices every cabal from a table and draws one live point per cabal.
type stubMatchupMarks struct {
	pots   map[string]int64
	points map[string][]app.GroupPnLPoint
}

func (s *stubMatchupMarks) Marks(_ context.Context, ids []string) (map[string]app.MatchupMark, error) {
	out := map[string]app.MatchupMark{}
	for _, id := range ids {
		if pot, ok := s.pots[id]; ok {
			out[id] = app.MatchupMark{PotMicros: pot, NetInMicros: pot, Live: true}
		}
	}
	return out, nil
}

func (s *stubMatchupMarks) SeriesSince(_ context.Context, ids []string, since time.Time) (map[string][]app.GroupPnLPoint, error) {
	out := map[string][]app.GroupPnLPoint{}
	for _, id := range ids {
		for _, p := range s.points[id] {
			if p.At.After(since) {
				out[id] = append(out[id], p)
			}
		}
	}
	return out, nil
}

type matchupHTTPHarness struct {
	handlers *MatchupHandlers
	auth     *AuthHandlers
	privy    privy.Client
	store    *postgres.Store
	db       *sql.DB
	iso      *postgres.TestIsolation
	marks    *stubMatchupMarks
	clock    time.Time
	cabals   []string
}

func newMatchupHTTPHarness(t *testing.T) *matchupHTTPHarness {
	t.Helper()
	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	pythClient := pyth.NewFakeClient()
	symbols := app.NewSymbolResolver(nil)
	deposits := app.NewDepositService(store, privyClient, pythClient, symbols)
	home := app.NewHomeService(store, privyClient, pythClient, deposits, symbols)
	h := &matchupHTTPHarness{
		auth:  authHandlers,
		privy: privyClient,
		store: store,
		db:    db,
		iso:   iso,
		marks: &stubMatchupMarks{pots: map[string]int64{}, points: map[string][]app.GroupPnLPoint{}},
	}
	service := app.NewMatchupService(store, home).
		WithMarks(h.marks).
		WithClock(func() time.Time { return h.clock }).
		WithCandidates(func(ctx context.Context) ([]postgres.GroupDirectoryRow, error) {
			return store.ListGroupDirectoryByIDs(ctx, h.cabals)
		})
	h.handlers = &MatchupHandlers{Matchups: service}

	// A Monday in the 2300s, clear of the weeks the app package's tests draw.
	n, err := rand.Int(rand.Reader, big.NewInt(4000))
	if err != nil {
		t.Fatalf("random week: %v", err)
	}
	week := app.MatchupWeekStart(time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)).Add(time.Duration(n.Int64()) * 2 * app.MatchupWeek)
	h.clock = week.Add(time.Minute)
	t.Cleanup(func() {
		for _, w := range []time.Time{week, week.Add(app.MatchupWeek)} {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM matchup_weeks WHERE week_start = $1::date`, w.Format("2006-01-02"))
		}
	})
	return h
}

func (h *matchupHTTPHarness) week() time.Time { return app.MatchupWeekStart(h.clock) }

func (h *matchupHTTPHarness) cabal(t *testing.T, label string, potMicros int64, memberIDs ...string) string {
	t.Helper()
	ctx := context.Background()
	group, err := h.store.InsertGroup(ctx, h.iso.Suffix()+" "+label, memberIDs[0])
	if err != nil {
		t.Fatalf("insert cabal: %v", err)
	}
	trackCreatedGroup(h.iso, group.ID)
	tx, err := h.store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, id := range memberIDs {
		if err := h.store.InsertGroupMemberTx(ctx, tx, group.ID, id); err != nil {
			_ = tx.Rollback()
			t.Fatalf("member: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE groups SET created_at = $2 WHERE id = $1`, group.ID, h.week().Add(-time.Hour)); err != nil {
		t.Fatalf("backdate cabal: %v", err)
	}
	h.marks.pots[group.ID] = potMicros
	h.cabals = append(h.cabals, group.ID)
	return group.ID
}

func (h *matchupHTTPHarness) serve(handler http.HandlerFunc, method, path, pattern, token, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	mux := http.NewServeMux()
	mux.HandleFunc(method+" "+pattern, handler)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeMatchupJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

func TestMatchupRoutes_requireASignedInUser(t *testing.T) {
	h := newMatchupHTTPHarness(t)
	cases := []struct {
		handler http.HandlerFunc
		method  string
		path    string
		pattern string
	}{
		{h.handlers.GroupMatchupHandler, http.MethodGet, "/v1/groups/00000000-0000-4000-8000-000000000000/matchup", "/v1/groups/{id}/matchup"},
		{h.handlers.HomeMatchupsHandler, http.MethodGet, "/v1/home/matchups", "/v1/home/matchups"},
		{h.handlers.MatchupTableHandler, http.MethodGet, "/v1/matchups/table", "/v1/matchups/table"},
		{h.handlers.CreateMatchupChallengeHandler, http.MethodPost, "/v1/groups/00000000-0000-4000-8000-000000000000/matchups/challenge", "/v1/groups/{id}/matchups/challenge"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if rec := h.serve(tc.handler, tc.method, tc.path, tc.pattern, "", ""); rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestGroupMatchupRoute_servesTheLiveMatchupFromTheCabalsSide(t *testing.T) {
	// Arrange: two cabals drawn against each other; b is up 2% by Wednesday.
	h := newMatchupHTTPHarness(t)
	ada, adaToken := seedAuthenticatedUser(t, h.iso, h.auth, h.privy, "ada", "Ada")
	bo, _ := seedAuthenticatedUser(t, h.iso, h.auth, h.privy, "bo", "Bo")
	a := h.cabal(t, "Weekend investors", 2_000_000_000, ada.UserID, bo.UserID)
	b := h.cabal(t, "Semis or bust", 1_000_000_000, bo.UserID, ada.UserID)
	if _, err := h.handlers.Matchups.DrawWeek(context.Background(), h.week()); err != nil {
		t.Fatalf("draw: %v", err)
	}
	wednesday := h.week().Add(2*24*time.Hour + 12*time.Hour)
	h.marks.points[b] = []app.GroupPnLPoint{{At: wednesday, PotNavMicros: 1_020_000_000, NetInMicros: 1_000_000_000}}
	h.clock = wednesday.Add(time.Hour)

	// Act
	rec := h.serve(h.handlers.GroupMatchupHandler, http.MethodGet, "/v1/groups/"+b+"/matchup", "/v1/groups/{id}/matchup", string(adaToken), "")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	got := decodeMatchupJSON[groupMatchupResponse](t, rec)
	if got.Current == nil || got.Current.A.GroupID != b || got.Current.B == nil || got.Current.B.GroupID != a {
		t.Fatalf("current = %+v, want b first against a", got.Current)
	}
	if got.Current.A.Score == nil || *got.Current.A.Score != "0.02" || got.Current.B.Score == nil || *got.Current.B.Score != "0" {
		t.Fatalf("scores = %v / %v, want 0.02 / 0", got.Current.A.Score, got.Current.B.Score)
	}
	if got.Current.Leading == nil || *got.Current.Leading != "a" || got.Current.DaysLeft != 5 {
		t.Fatalf("leading %v with %d days left, want a with 5", got.Current.Leading, got.Current.DaysLeft)
	}
	if got.Current.A.MemberCount != 2 || got.Current.A.Name == "" {
		t.Fatalf("side a face = %+v", got.Current.A)
	}
	if !got.CanChallenge || got.Recent == nil || got.Challenges == nil {
		t.Fatalf("member view = %+v, want canChallenge and empty lists (not null)", got)
	}
	if !strings.HasSuffix(got.NextDrawAt, "T00:00:00Z") {
		t.Fatalf("nextDrawAt = %q, want Monday midnight UTC", got.NextDrawAt)
	}
}

func TestMatchupChallengeRoutes_sendAcceptAndRefuse(t *testing.T) {
	// Arrange
	h := newMatchupHTTPHarness(t)
	ada, adaToken := seedAuthenticatedUser(t, h.iso, h.auth, h.privy, "ada", "Ada")
	bo, boToken := seedAuthenticatedUser(t, h.iso, h.auth, h.privy, "bo", "Bo")
	cy, _ := seedAuthenticatedUser(t, h.iso, h.auth, h.privy, "cy", "Cy")
	a := h.cabal(t, "Weekend investors", 500_000_000, ada.UserID, cy.UserID)
	b := h.cabal(t, "Semis or bust", 400_000_000, bo.UserID, cy.UserID)
	challengePath := "/v1/groups/" + a + "/matchups/challenge"
	challengePattern := "/v1/groups/{id}/matchups/challenge"
	acceptPattern := "/v1/groups/{id}/matchups/challenges/{challengeId}/accept"

	// Act
	sent := h.serve(h.handlers.CreateMatchupChallengeHandler, http.MethodPost, challengePath, challengePattern, string(adaToken), `{"groupId":"`+b+`"}`)
	again := h.serve(h.handlers.CreateMatchupChallengeHandler, http.MethodPost, challengePath, challengePattern, string(adaToken), `{"groupId":"`+b+`"}`)
	missing := h.serve(h.handlers.CreateMatchupChallengeHandler, http.MethodPost, challengePath, challengePattern, string(adaToken), `{}`)
	self := h.serve(h.handlers.CreateMatchupChallengeHandler, http.MethodPost, challengePath, challengePattern, string(adaToken), `{"groupId":"`+a+`"}`)
	notMember := h.serve(h.handlers.CreateMatchupChallengeHandler, http.MethodPost, challengePath, challengePattern, string(boToken), `{"groupId":"`+b+`"}`)
	challenge := decodeMatchupJSON[matchupChallengeResponse](t, sent)
	acceptPath := "/v1/groups/" + b + "/matchups/challenges/" + challenge.ID + "/accept"
	accepted := h.serve(h.handlers.AcceptMatchupChallengeHandler, http.MethodPost, acceptPath, acceptPattern, string(boToken), "")
	unknown := h.serve(h.handlers.AcceptMatchupChallengeHandler, http.MethodPost, "/v1/groups/"+b+"/matchups/challenges/00000000-0000-4000-8000-000000000000/accept", acceptPattern, string(boToken), "")
	reverse := h.serve(h.handlers.CreateMatchupChallengeHandler, http.MethodPost, "/v1/groups/"+b+"/matchups/challenge", challengePattern, string(boToken), `{"groupId":"`+a+`"}`)
	view := h.serve(h.handlers.GroupMatchupHandler, http.MethodGet, "/v1/groups/"+b+"/matchup", "/v1/groups/{id}/matchup", string(boToken), "")

	// Assert
	if sent.Code != http.StatusCreated || again.Code != http.StatusOK {
		t.Fatalf("send twice = %d, %d; want 201 then 200", sent.Code, again.Code)
	}
	if challenge.Direction != "outgoing" || challenge.Status != "pending" || challenge.Opponent.GroupID != b {
		t.Fatalf("challenge = %+v", challenge)
	}
	if missing.Code != http.StatusBadRequest || self.Code != http.StatusBadRequest || notMember.Code != http.StatusForbidden {
		t.Fatalf("refusals = %d, %d, %d; want 400, 400, 403", missing.Code, self.Code, notMember.Code)
	}
	if accepted.Code != http.StatusOK || decodeMatchupJSON[matchupChallengeResponse](t, accepted).Status != "accepted" {
		t.Fatalf("accept = %d %s", accepted.Code, accepted.Body.String())
	}
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown challenge = %d, want 404", unknown.Code)
	}
	if reverse.Code != http.StatusConflict || decodeMatchupJSON[errorResponse](t, reverse).Reason != app.MatchupConflictAlreadyMatched {
		t.Fatalf("challenge once matched = %d %s, want 409 already_matched", reverse.Code, reverse.Body.String())
	}
	got := decodeMatchupJSON[groupMatchupResponse](t, view)
	if len(got.Challenges) != 1 || got.Challenges[0].Direction != "incoming" || got.Challenges[0].Status != "accepted" {
		t.Fatalf("b's challenges = %+v, want the accepted incoming one", got.Challenges)
	}
}

func TestHomeMatchupsAndTableRoutes(t *testing.T) {
	// Arrange
	h := newMatchupHTTPHarness(t)
	ada, adaToken := seedAuthenticatedUser(t, h.iso, h.auth, h.privy, "ada", "Ada")
	bo, _ := seedAuthenticatedUser(t, h.iso, h.auth, h.privy, "bo", "Bo")
	a := h.cabal(t, "Weekend investors", 500_000_000, ada.UserID, bo.UserID)
	h.cabal(t, "Semis or bust", 400_000_000, bo.UserID, ada.UserID)
	h.cabal(t, "Index huggers", 300_000_000, bo.UserID, ada.UserID)
	if _, err := h.handlers.Matchups.DrawWeek(context.Background(), h.week()); err != nil {
		t.Fatalf("draw: %v", err)
	}

	// Act
	home := h.serve(h.handlers.HomeMatchupsHandler, http.MethodGet, "/v1/home/matchups", "/v1/home/matchups", string(adaToken), "")
	table := h.serve(h.handlers.MatchupTableHandler, http.MethodGet, "/v1/matchups/table?limit=5", "/v1/matchups/table", string(adaToken), "")
	badLimit := h.serve(h.handlers.MatchupTableHandler, http.MethodGet, "/v1/matchups/table?limit=0", "/v1/matchups/table", string(adaToken), "")

	// Assert
	if home.Code != http.StatusOK {
		t.Fatalf("home = %d %s", home.Code, home.Body.String())
	}
	homeBody := decodeMatchupJSON[homeMatchupsResponse](t, home)
	if !homeBody.Drawn || !homeBody.HasCabals || len(homeBody.Matchups) != 2 || homeBody.DaysLeft != 7 {
		t.Fatalf("home = %+v, want a pair and a bye in a fresh week", homeBody)
	}
	if homeBody.Matchups[0].A.GroupID != a {
		t.Fatalf("first home matchup leads with %s, want the cabal ada joined first", homeBody.Matchups[0].A.GroupID)
	}
	var bye *matchupCurrentResponse
	for i := range homeBody.Matchups {
		if homeBody.Matchups[i].B == nil {
			bye = &homeBody.Matchups[i]
		}
	}
	if bye == nil || bye.Leading != nil || bye.A.Score != nil {
		t.Fatalf("bye = %+v, want no opponent, no score and no leader", bye)
	}
	if table.Code != http.StatusOK {
		t.Fatalf("table = %d %s", table.Code, table.Body.String())
	}
	if badLimit.Code != http.StatusBadRequest {
		t.Fatalf("limit=0 = %d, want 400", badLimit.Code)
	}
}
