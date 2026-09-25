package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

type inviteHarness struct {
	invites *InviteHandlers
	groups  *GroupHandlers
	auth    *AuthHandlers
	privy   privy.Client
	store   *postgres.Store
	iso     *postgres.TestIsolation
}

func newInviteHarness(t *testing.T) inviteHarness {
	t.Helper()
	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	pythClient := pyth.NewFakeClient()
	symbols := app.NewSymbolResolver(nil)
	deposits := app.NewDepositService(store, privyClient, pythClient, symbols)
	home := app.NewHomeService(store, privyClient, pythClient, deposits, symbols)
	governance := app.NewGovernanceService(store, privyClient)
	return inviteHarness{
		invites: &InviteHandlers{Invites: app.NewInviteService(store, privyClient, governance, app.NewGroupsTabService(home, store))},
		groups:  &GroupHandlers{Groups: app.NewGroupService(store, privyClient), Governance: governance},
		auth:    authHandlers,
		privy:   privyClient,
		store:   store,
		iso:     iso,
	}
}

func (h inviteHarness) user(t *testing.T, label string) (authSessionResponse, privy.AccessToken) {
	t.Helper()
	return seedAuthenticatedUser(t, h.iso, h.auth, h.privy, label, label)
}

func (h inviteHarness) cabal(t *testing.T, token privy.AccessToken, name, joinMode string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"`+name+`","joinPolicy":{"mode":"`+joinMode+`"}}`))
	req.Header.Set("Authorization", "Bearer "+string(token))
	h.groups.CreateGroupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create cabal status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var created createGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode cabal: %v", err)
	}
	trackCreatedGroup(h.iso, created.GroupID)
	return created.GroupID
}

// sendInvite calls handler with an optional bearer token, body and path values (name, value, ...).
func sendInvite(handler http.HandlerFunc, method, path string, token privy.AccessToken, body string, pathValues ...string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	for i := 0; i+1 < len(pathValues); i += 2 {
		req.SetPathValue(pathValues[i], pathValues[i+1])
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func decodeInvite(t *testing.T, rec *httptest.ResponseRecorder) inviteResponse {
	t.Helper()
	var invite inviteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &invite); err != nil {
		t.Fatalf("decode invite: %v; body = %s", err, rec.Body.String())
	}
	return invite
}

var inviteCodePattern = regexp.MustCompile(`^[2-9A-HJ-NP-Z]{8}$`)

func TestGET_groupInvites_returnsTheLiveCodeAndLink(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newInviteHarness(t)
	_, token := h.user(t, "creator")
	groupID := h.cabal(t, token, "Sunday Investors", "open")

	// Act
	first := sendInvite(h.invites.GetGroupInviteHandler, http.MethodGet, "/v1/groups/"+groupID+"/invites", token, "", "id", groupID)
	second := sendInvite(h.invites.GetGroupInviteHandler, http.MethodGet, "/v1/groups/"+groupID+"/invites", token, "", "id", groupID)

	// Assert
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("status = %d, %d; want 200, 200", first.Code, second.Code)
	}
	invite := decodeInvite(t, first)
	if !inviteCodePattern.MatchString(invite.Code) || invite.URL != "https://trymonaco.xyz/join/"+invite.Code {
		t.Fatalf("invite = %+v", invite)
	}
	if again := decodeInvite(t, second); again.Code != invite.Code {
		t.Fatalf("second GET = %q, want the same code %q", again.Code, invite.Code)
	}
	if got := first.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestGroupInvites_refuseAnyoneButAMember(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newInviteHarness(t)
	_, creator := h.user(t, "creator")
	_, stranger := h.user(t, "stranger")
	groupID := h.cabal(t, creator, "Members Only", "open")
	routes := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{"get", h.invites.GetGroupInviteHandler, http.MethodGet, "/v1/groups/" + groupID + "/invites"},
		{"create", h.invites.CreateGroupInviteHandler, http.MethodPost, "/v1/groups/" + groupID + "/invites"},
		{"revoke", h.invites.RevokeGroupInviteHandler, http.MethodPost, "/v1/groups/" + groupID + "/invites/revoke"},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			// Act
			anonymous := sendInvite(route.handler, route.method, route.path, "", "", "id", groupID)
			outsider := sendInvite(route.handler, route.method, route.path, stranger, "", "id", groupID)
			malformed := sendInvite(route.handler, route.method, "/v1/groups/nope/invites", creator, "", "id", "nope")

			// Assert
			if anonymous.Code != http.StatusUnauthorized {
				t.Errorf("no session = %d, want 401", anonymous.Code)
			}
			if outsider.Code != http.StatusNotFound {
				t.Errorf("non-member = %d, want 404", outsider.Code)
			}
			if malformed.Code != http.StatusNotFound {
				t.Errorf("malformed id = %d, want 404", malformed.Code)
			}
		})
	}
}

func TestPOST_groupInvites_replacesTheCodeAndRetiresTheOldOne(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newInviteHarness(t)
	_, token := h.user(t, "creator")
	groupID := h.cabal(t, token, "Rotating", "open")
	old := decodeInvite(t, sendInvite(h.invites.GetGroupInviteHandler, http.MethodGet, "/v1/groups/"+groupID+"/invites", token, "", "id", groupID))

	// Act
	rec := sendInvite(h.invites.CreateGroupInviteHandler, http.MethodPost, "/v1/groups/"+groupID+"/invites", token, "", "id", groupID)

	// Assert
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	fresh := decodeInvite(t, rec)
	if fresh.Code == old.Code || !inviteCodePattern.MatchString(fresh.Code) {
		t.Fatalf("new code = %q, old = %q", fresh.Code, old.Code)
	}
	if preview := sendInvite(h.invites.GetInvitePreviewHandler, http.MethodGet, "/v1/invites/"+old.Code, "", "", "code", old.Code); preview.Code != http.StatusNotFound {
		t.Fatalf("old code preview = %d, want 404", preview.Code)
	}
}

func TestPOST_groupInvitesRevoke_leavesTheLinkDead(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newInviteHarness(t)
	_, token := h.user(t, "creator")
	groupID := h.cabal(t, token, "Revoked", "open")
	old := decodeInvite(t, sendInvite(h.invites.GetGroupInviteHandler, http.MethodGet, "/v1/groups/"+groupID+"/invites", token, "", "id", groupID))

	// Act
	rec := sendInvite(h.invites.RevokeGroupInviteHandler, http.MethodPost, "/v1/groups/"+groupID+"/invites/revoke", token, "", "id", groupID)

	// Assert
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if preview := sendInvite(h.invites.GetInvitePreviewHandler, http.MethodGet, "/v1/invites/"+old.Code, "", "", "code", old.Code); preview.Code != http.StatusNotFound {
		t.Fatalf("revoked code preview = %d, want 404", preview.Code)
	}
}

func TestGET_invitePreview_isPublicAndCarriesTheCabalsFace(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newInviteHarness(t)
	_, token := h.user(t, "creator")
	groupID := h.cabal(t, token, "Sunday Investors", "request")
	invite := decodeInvite(t, sendInvite(h.invites.GetGroupInviteHandler, http.MethodGet, "/v1/groups/"+groupID+"/invites", token, "", "id", groupID))

	// Act: no Authorization header at all.
	rec := sendInvite(h.invites.GetInvitePreviewHandler, http.MethodGet, "/v1/invites/"+invite.Code, "", "", "code", strings.ToLower(invite.Code))

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	want := map[string]any{
		"code":        invite.Code,
		"groupId":     groupID,
		"name":        "Sunday Investors",
		"memberCount": float64(1),
		"tint":        app.CabalTintName(groupID),
		"pictureUrl":  nil,
		"joinPolicy":  "request",
		"potValueUsd": "0.00",
	}
	if len(body) != len(want) {
		t.Fatalf("preview keys = %v, want exactly %v", body, want)
	}
	for key, value := range want {
		if body[key] != value {
			t.Errorf("%s = %#v, want %#v", key, body[key], value)
		}
	}
}

func TestGET_invitePreview_unknownCodeIsNotFound(t *testing.T) {
	t.Parallel()
	h := newInviteHarness(t)
	for _, code := range []string{"ZZZZ2222", "AB12CD34", "short"} {
		// Act
		rec := sendInvite(h.invites.GetInvitePreviewHandler, http.MethodGet, "/v1/invites/"+code, "", "", "code", code)

		// Assert
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", code, rec.Code)
		}
		var body struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error != "invite not found" {
			t.Errorf("%s: body = %s", code, rec.Body.String())
		}
	}
}

func TestPOST_joinByCode_openCabalJoinsWithTheLegacyAnswer(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newInviteHarness(t)
	_, creator := h.user(t, "creator")
	joiner, joinerToken := h.user(t, "joiner")
	groupID := h.cabal(t, creator, "Open Door", "open")
	invite := decodeInvite(t, sendInvite(h.invites.GetGroupInviteHandler, http.MethodGet, "/v1/groups/"+groupID+"/invites", creator, "", "id", groupID))

	// Act: the code as someone would paste it from a message.
	rec := sendInvite(h.invites.JoinByCodeHandler, http.MethodPost, "/v1/groups/join-by-code", joinerToken, `{"code":"`+strings.ToLower(invite.Code[:4])+"-"+invite.Code[4:]+`"}`)

	// Assert
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/v1/groups/"+groupID {
		t.Fatalf("Location = %q", got)
	}
	member, err := h.store.IsGroupMember(t.Context(), groupID, joiner.UserID)
	if err != nil || !member {
		t.Fatalf("joiner is member = (%v, %v), want true", member, err)
	}
}

func TestPOST_joinByCode_requestCabalFilesARequest(t *testing.T) {
	t.Parallel()
	// Arrange
	h := newInviteHarness(t)
	_, creator := h.user(t, "creator")
	joiner, joinerToken := h.user(t, "joiner")
	groupID := h.cabal(t, creator, "By Request", "request")
	invite := decodeInvite(t, sendInvite(h.invites.GetGroupInviteHandler, http.MethodGet, "/v1/groups/"+groupID+"/invites", creator, "", "id", groupID))

	// Act
	rec := sendInvite(h.invites.JoinByCodeHandler, http.MethodPost, "/v1/groups/join-by-code", joinerToken, `{"code":"`+invite.Code+`"}`)

	// Assert
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body = %s", rec.Code, rec.Body.String())
	}
	var body joinByCodePendingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "pending" || body.GroupID != groupID {
		t.Fatalf("body = %+v", body)
	}
	if _, pending, err := h.store.GetPendingJoinRequest(t.Context(), groupID, joiner.UserID); err != nil || !pending {
		t.Fatalf("pending request = (%v, %v), want true", pending, err)
	}
}

func TestPOST_joinByCode_refusals(t *testing.T) {
	t.Parallel()
	h := newInviteHarness(t)
	_, joiner := h.user(t, "joiner")
	cases := []struct {
		name   string
		token  privy.AccessToken
		body   string
		status int
	}{
		{"unknown code", joiner, `{"code":"ZZZZ2222"}`, http.StatusNotFound},
		{"malformed code", joiner, `{"code":"AB12CD34"}`, http.StatusNotFound},
		{"empty code", joiner, `{"code":""}`, http.StatusNotFound},
		{"not json", joiner, `{code`, http.StatusBadRequest},
		{"no session", "", `{"code":"ZZZZ2222"}`, http.StatusUnauthorized},
		{"bad session", "not-a-token", `{"code":"ZZZZ2222"}`, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			rec := sendInvite(h.invites.JoinByCodeHandler, http.MethodPost, "/v1/groups/join-by-code", tc.token, tc.body)

			// Assert
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestRateLimiter_invitePreviewIsLimitedPerIPWithoutASession(t *testing.T) {
	// Arrange
	limiter := NewRateLimiter(nil, false)
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), limiter.Middleware())
	preview := func(ip string) int {
		req := testHTTPRequest(http.MethodGet, "/v1/invites/K7QM4XPD")
		req.RemoteAddr = ip + ":5100"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	burst := rateLimitRules[rateClassPublic].ipBurst

	// Act / Assert
	for i := 0; i < burst; i++ {
		if code := preview("203.0.113.5"); code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200 inside the burst", i, code)
		}
	}
	if code := preview("203.0.113.5"); code != http.StatusTooManyRequests {
		t.Fatalf("over the IP budget = %d, want 429", code)
	}
	if code := preview("198.51.100.7"); code != http.StatusOK {
		t.Fatalf("another address = %d, want 200", code)
	}
}

func TestClassifyRateLimit_publicRoutesAreLimitedWhateverTheMethod(t *testing.T) {
	cases := []struct {
		method, path string
		class        string
		limited      bool
	}{
		{http.MethodGet, "/v1/invites/K7QM4XPD", rateClassPublic, true},
		{http.MethodHead, "/v1/invites/K7QM4XPD", rateClassPublic, true},
		{http.MethodGet, "/v1/groups/g1/invites", "", false},
		{http.MethodPost, "/v1/groups/g1/invites", rateClassWrite, true},
		{http.MethodPost, "/v1/groups/join-by-code", rateClassWrite, true},
	}
	for _, tc := range cases {
		class, limited := classifyRateLimit(tc.method, tc.path)
		if class != tc.class || limited != tc.limited {
			t.Errorf("%s %s = (%q, %v), want (%q, %v)", tc.method, tc.path, class, limited, tc.class, tc.limited)
		}
	}
}
