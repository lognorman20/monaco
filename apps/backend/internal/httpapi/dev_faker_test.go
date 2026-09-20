package httpapi

import (
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/faker"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

const localTestDBURL = "postgres://monaco:x@127.0.0.1:55432/monaco?sslmode=disable"

type fakerTestEnv struct {
	handlers *DevFakerHandlers
	groups   *GroupHandlers
	auth     *AuthHandlers
	privy    wallets.Client
	iso      *postgres.TestIsolation
}

func newFakerTestEnv(t *testing.T) fakerTestEnv {
	t.Helper()
	_, authHandlers, groupHandlers, privyClient, store, iso := integrationHomeApp(t)
	return fakerTestEnv{
		handlers: &DevFakerHandlers{
			Enabled:     true,
			DatabaseURL: localTestDBURL,
			Store:       store,
			Privy:       privyClient,
			Seeder:      faker.NewSeeder(store, nil).WithPrefix("test-" + iso.Suffix() + "-"),
		},
		groups: groupHandlers,
		auth:   authHandlers,
		privy:  privyClient,
		iso:    iso,
	}
}

func (e fakerTestEnv) createGroup(t *testing.T, token auth.AccessToken) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Operator `+e.iso.Suffix()+`"}`))
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	e.groups.CreateGroupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create group = %d %s", rec.Code, rec.Body.String())
	}
	var created createGroupResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	e.iso.TrackGroup(created.GroupID)
	return created.GroupID
}

func (e fakerTestEnv) post(t *testing.T, token auth.AccessToken, body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/dev/faker", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:50000"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	e.handlers.FakerHandler(rec, req)
	if rec.Code == http.StatusOK {
		var resp devFakerResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode faker response: %v", err)
		}
		if resp.Mixed != nil {
			for _, id := range resp.Mixed.UserIDs {
				e.iso.TrackUser(id)
			}
		}
		if resp.Scale != nil {
			for _, c := range resp.Scale.Clubs {
				e.iso.TrackGroup(c.GroupID)
				for _, id := range c.UserIDs {
					e.iso.TrackUser(id)
				}
			}
		}
	}
	return rec
}

func TestDevFaker_guardsAndUnhappyPaths(t *testing.T) {
	e := newFakerTestEnv(t)
	_, owner := seedAuthenticatedUser(t, e.iso, e.auth, e.privy, "owner", "Owner")
	_, other := seedAuthenticatedUser(t, e.iso, e.auth, e.privy, "other", "Other")
	groupID := e.createGroup(t, owner)

	cases := []struct {
		name   string
		setup  func()
		token  auth.AccessToken
		body   string
		mutate func(*http.Request)
		want   int
	}{
		{name: "disabled returns 404", setup: func() { e.handlers.Enabled = false }, token: owner, body: `{"profile":"all"}`, want: http.StatusNotFound},
		{name: "non-loopback caller", token: owner, body: `{"profile":"scale"}`, mutate: func(r *http.Request) { r.RemoteAddr = "192.168.1.20:4000" }, want: http.StatusForbidden},
		{name: "proxied loopback caller", token: owner, body: `{"profile":"scale"}`, mutate: func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.9") }, want: http.StatusForbidden},
		{name: "non-local database", setup: func() { e.handlers.DatabaseURL = "postgres://u:p@db.example.supabase.co:5432/postgres" }, token: owner, body: `{"profile":"scale"}`, want: http.StatusForbidden},
		{name: "missing auth", body: `{"profile":"scale"}`, want: http.StatusUnauthorized},
		{name: "unknown profile", token: owner, body: `{"profile":"everything"}`, want: http.StatusBadRequest},
		{name: "mixed without group", token: owner, body: `{"profile":"mixed"}`, want: http.StatusBadRequest},
		{name: "mixed bad uuid", token: owner, body: `{"profile":"mixed","group_id":"nope"}`, want: http.StatusBadRequest},
		{name: "mixed unknown group", token: owner, body: `{"profile":"mixed","group_id":"00000000-0000-0000-0000-000000000000"}`, want: http.StatusNotFound},
		{name: "mixed non-creator", token: other, body: `{"profile":"mixed","group_id":"` + groupID + `"}`, want: http.StatusForbidden},
		{name: "demo without group", token: owner, body: `{"profile":"demo"}`, want: http.StatusBadRequest},
		{name: "demo non-creator", token: other, body: `{"profile":"demo","group_id":"` + groupID + `"}`, want: http.StatusForbidden},
		{name: "demo bad proposal uuid", token: owner, body: `{"profile":"demo","group_id":"` + groupID + `","proposal_id":"nope"}`, want: http.StatusBadRequest},
		{name: "demo unknown proposal", token: owner, body: `{"profile":"demo","group_id":"` + groupID + `","proposal_id":"00000000-0000-0000-0000-000000000000"}`, want: http.StatusNotFound},
		{name: "proposal id outside demo", token: owner, body: `{"profile":"mixed","group_id":"` + groupID + `","proposal_id":"00000000-0000-0000-0000-000000000000"}`, want: http.StatusBadRequest},
	}
	for _, tc := range cases {
		e.handlers.Enabled = true
		e.handlers.DatabaseURL = localTestDBURL
		if tc.setup != nil {
			tc.setup()
		}
		rec := e.post(t, tc.token, tc.body, tc.mutate)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d; body = %s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestDevFaker_allProfileSeedsIdempotently(t *testing.T) {
	e := newFakerTestEnv(t)
	_, owner := seedAuthenticatedUser(t, e.iso, e.auth, e.privy, "owner", "Owner")
	groupID := e.createGroup(t, owner)
	body := `{"profile":"all","group_id":"` + groupID + `"}`

	first := e.post(t, owner, body, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first seed = %d %s", first.Code, first.Body.String())
	}
	second := e.post(t, owner, body, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("second seed = %d %s", second.Code, second.Body.String())
	}
	var a, b devFakerResponse
	_ = json.Unmarshal(first.Body.Bytes(), &a)
	_ = json.Unmarshal(second.Body.Bytes(), &b)
	if len(a.Scale.Clubs) != 6 || len(b.Scale.Clubs) != 6 {
		t.Fatalf("clubs = %d / %d, want 6", len(a.Scale.Clubs), len(b.Scale.Clubs))
	}
	for i := range a.Scale.Clubs {
		if a.Scale.Clubs[i].GroupID != b.Scale.Clubs[i].GroupID {
			t.Errorf("club %d id changed across runs", i)
		}
	}
	for i := range a.Mixed.UserIDs {
		if a.Mixed.UserIDs[i] != b.Mixed.UserIDs[i] {
			t.Errorf("ghost %d id changed across runs", i)
		}
	}

	// A scale club is not a valid mixed target.
	rec := e.post(t, owner, `{"profile":"mixed","group_id":"`+a.Scale.Clubs[0].GroupID+`"}`, nil)
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
		t.Errorf("mixed on scale club = %d, want 400/403", rec.Code)
	}
}

func TestConfig_isLocalDatabaseURL(t *testing.T) {
	for url, want := range map[string]bool{
		"postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable": true,
		"postgres://monaco:monaco@127.0.0.1:55432/monaco":                 true,
		"postgres://u:p@[::1]:5432/monaco":                                true,
		"postgres://u:p@10.0.0.4:5432/monaco":                             false,
		"postgres://u:p@db.abc.supabase.co:5432/postgres":                 false,
		"postgres://u:p@localhost.supabase.com:5432/postgres":             false,
		"mysql://u:p@localhost/x":                                         false,
		"":                                                                false,
	} {
		if got := config.IsLocalDatabaseURL(url); got != want {
			t.Errorf("IsLocalDatabaseURL(%q) = %v, want %v", url, got, want)
		}
	}
}
