package httpapi

import (
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/apps/backend/internal/ratelimit"
	"github.com/monaco/monaco/apps/backend/internal/storage"
)

func patchMe(t *testing.T, handlers *MeHandlers, token auth.AccessToken, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/v1/me", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlers.PatchMeHandler(rec, req)
	return rec
}

func getMe(t *testing.T, handlers *MeHandlers, token auth.AccessToken) meResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	handlers.MeHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/me status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode GET /v1/me: %v", err)
	}
	return payload
}

func decodeMe(t *testing.T, rec *httptest.ResponseRecorder) meResponse {
	t.Helper()
	var payload meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode me json: %v; body = %s", err, rec.Body.String())
	}
	return payload
}

func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error json: %v; body = %s", err, rec.Body.String())
	}
	return payload["error"]
}

func jsonDisplayName(name string) string {
	encoded, _ := json.Marshal(map[string]string{"displayName": name})
	return string(encoded)
}

func TestPATCH_me_authenticated_setsDisplayName(t *testing.T) {
	t.Parallel()
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, nil)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-me", "")

	rec := patchMe(t, meHandlers, token, `{"displayName":"Logan"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	payload := decodeMe(t, rec)
	if payload.DisplayName != "Logan" {
		t.Fatalf("displayName = %q, want Logan", payload.DisplayName)
	}
	if payload.UserID != session.UserID {
		t.Fatalf("userId = %q, want %q", payload.UserID, session.UserID)
	}
	if payload.MemberWalletAddress == "" || payload.MemberWalletAddress != session.MemberWalletAddress {
		t.Fatalf("memberWalletAddress = %q, want %q", payload.MemberWalletAddress, session.MemberWalletAddress)
	}
	if payload.ProfilePhotoURL != nil {
		t.Fatalf("profilePhotoUrl = %q, want null", *payload.ProfilePhotoURL)
	}
	if _, err := time.Parse(time.RFC3339, payload.CreatedAt); err != nil {
		t.Fatalf("createdAt = %q, want RFC3339: %v", payload.CreatedAt, err)
	}
	if got := getMe(t, meHandlers, token); got.DisplayName != "Logan" {
		t.Fatalf("GET /v1/me displayName = %q after PATCH, want Logan", got.DisplayName)
	}
}

func TestPATCH_me_trimsAndCollapsesWhitespace(t *testing.T) {
	t.Parallel()
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, nil)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-trim", "Before")

	rec := patchMe(t, meHandlers, token, jsonDisplayName("   Logan     Norman \n"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if got := decodeMe(t, rec).DisplayName; got != "Logan Norman" {
		t.Fatalf("displayName = %q, want %q", got, "Logan Norman")
	}
	if got := getMe(t, meHandlers, token).DisplayName; got != "Logan Norman" {
		t.Fatalf("persisted displayName = %q, want %q", got, "Logan Norman")
	}
}

func TestPATCH_me_acceptsBoundaryAndUnicodeNames(t *testing.T) {
	t.Parallel()
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, nil)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-unicode", "Before")

	for _, name := range []string{
		"L",
		strings.Repeat("a", app.DisplayNameMaxRunes),
		strings.Repeat("é", app.DisplayNameMaxRunes),
		"José Álvarez",
		"山田太郎",
		"Ana 🚀",
	} {
		rec := patchMe(t, meHandlers, token, jsonDisplayName(name))
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH %q status = %d, want 200; body = %s", name, rec.Code, rec.Body.String())
		}
		if got := decodeMe(t, rec).DisplayName; got != name {
			t.Fatalf("displayName = %q, want %q", got, name)
		}
	}
}

func TestPATCH_me_invalidDisplayName_returns400AndKeepsName(t *testing.T) {
	t.Parallel()
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, nil)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-invalid", "Alfred")

	zeroWidthSpace := string(rune(0x200b))
	rightToLeftOverride := string(rune(0x202e))
	hangulFiller := string(rune(0x3164))
	cases := []struct {
		name        string
		displayName string
		wantMessage string
	}{
		{"empty", "", "Display name is required."},
		{"whitespace only", "   ", "Display name is required."},
		{"too long ascii", strings.Repeat("a", app.DisplayNameMaxRunes+1), "Display name must be 32 characters or fewer."},
		{"too long multibyte", strings.Repeat("é", app.DisplayNameMaxRunes+1), "Display name must be 32 characters or fewer."},
		{"interior newline", "Lo\ngan", "Display name can only use letters, numbers, spaces, punctuation, and emoji."},
		{"control character", "Lo\x07gan", "Display name can only use letters, numbers, spaces, punctuation, and emoji."},
		{"zero width space", "Lo" + zeroWidthSpace + "gan", "Display name can only use letters, numbers, spaces, punctuation, and emoji."},
		{"bidi override", rightToLeftOverride + "Logan", "Display name can only use letters, numbers, spaces, punctuation, and emoji."},
		{"invisible filler", hangulFiller, "Display name can only use letters, numbers, spaces, punctuation, and emoji."},
		{"punctuation only", "...", "Display name must include a letter or number."},
	}
	for _, tc := range cases {
		rec := patchMe(t, meHandlers, token, jsonDisplayName(tc.displayName))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body = %s", tc.name, rec.Code, rec.Body.String())
		}
		if got := errorMessage(t, rec); got != tc.wantMessage {
			t.Fatalf("%s: error = %q, want %q", tc.name, got, tc.wantMessage)
		}
	}

	if got := getMe(t, meHandlers, token).DisplayName; got != "Alfred" {
		t.Fatalf("displayName = %q after rejected updates, want unchanged Alfred", got)
	}
}

func TestPATCH_me_malformedBodies_return400(t *testing.T) {
	t.Parallel()
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, nil)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-malformed", "Alfred")

	cases := []struct {
		name string
		body string
	}{
		{"not json", `displayName=Logan`},
		{"truncated json", `{"displayName":"Lo`},
		{"empty body", ``},
		{"wrong type", `{"displayName":42}`},
		{"missing field", `{}`},
		{"null field", `{"displayName":null}`},
		{"json null", `null`},
		{"trailing value", `{"displayName":"Logan"}{"displayName":"Other"}`},
	}
	for _, tc := range cases {
		rec := patchMe(t, meHandlers, token, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body = %s", tc.name, rec.Code, rec.Body.String())
		}
	}

	if got := getMe(t, meHandlers, token).DisplayName; got != "Alfred" {
		t.Fatalf("displayName = %q after malformed requests, want unchanged Alfred", got)
	}
}

func TestPATCH_me_oversizedBody_returns413(t *testing.T) {
	t.Parallel()
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, nil)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-oversized", "Alfred")

	body := `{"displayName":"` + strings.Repeat("a", maxPatchMeBodyBytes) + `"}`
	rec := patchMe(t, meHandlers, token, body)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body = %s", rec.Code, rec.Body.String())
	}
}

func TestPATCH_me_missingAuth_returns401(t *testing.T) {
	t.Parallel()
	meHandlers, _, _, _ := integrationMeHandlers(t, nil)

	rec := patchMe(t, meHandlers, "", `{"displayName":"Logan"}`)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
}

func TestPATCH_me_invalidToken_returns401EvenWithInvalidName(t *testing.T) {
	t.Parallel()
	meHandlers, _, _, _ := integrationMeHandlers(t, nil)

	for _, body := range []string{`{"displayName":"Logan"}`, `{"displayName":""}`} {
		rec := patchMe(t, meHandlers, "not-a-registered-token", body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("body %s: status = %d, want 401; body = %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestPATCH_me_duplicateNamesAreAllowed(t *testing.T) {
	t.Parallel()
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, nil)
	_, firstToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-dupe-a", "")
	_, secondToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-dupe-b", "")

	if rec := patchMe(t, meHandlers, firstToken, `{"displayName":"Sam"}`); rec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	rec := patchMe(t, meHandlers, secondToken, `{"displayName":"SAM"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200 (display names are labels, not handles); body = %s", rec.Code, rec.Body.String())
	}
}

func TestPATCH_me_rateLimited_returns429PerUser(t *testing.T) {
	t.Parallel()
	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	limiter := ratelimit.New(2, time.Minute)
	sessions := app.NewSessionService(store, auth.NewFakeVerifier(), privyClient).WithDisplayNameLimiter(limiter)
	meHandlers := &MeHandlers{Sessions: sessions}
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-limit", "Alfred")
	_, otherToken := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-limit-other", "Bartholomez")

	for _, name := range []string{"One", "Two"} {
		if rec := patchMe(t, meHandlers, token, jsonDisplayName(name)); rec.Code != http.StatusOK {
			t.Fatalf("PATCH %s status = %d, want 200; body = %s", name, rec.Code, rec.Body.String())
		}
	}
	rec := patchMe(t, meHandlers, token, `{"displayName":"Three"}`)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body = %s", rec.Code, rec.Body.String())
	}
	retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || retryAfter < 1 || retryAfter > 60 {
		t.Fatalf("Retry-After = %q, want whole seconds in [1,60]", rec.Header().Get("Retry-After"))
	}
	if got := getMe(t, meHandlers, token).DisplayName; got != "Two" {
		t.Fatalf("displayName = %q after throttled write, want Two", got)
	}
	if rec := patchMe(t, meHandlers, otherToken, `{"displayName":"Other"}`); rec.Code != http.StatusOK {
		t.Fatalf("other user status = %d, want 200 (limits are per user); body = %s", rec.Code, rec.Body.String())
	}
}

func TestPOST_session_returnsProfilePhotoAndCreatedAt(t *testing.T) {
	t.Parallel()
	fakeStorage := storage.NewFakeClient("https://example.supabase.co")
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, fakeStorage)
	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "session-shape", "Alfred")
	if _, err := time.Parse(time.RFC3339, session.CreatedAt); err != nil {
		t.Fatalf("session createdAt = %q, want RFC3339", session.CreatedAt)
	}
	if session.ProfilePhotoURL != nil {
		t.Fatalf("session profilePhotoUrl = %q before upload, want null", *session.ProfilePhotoURL)
	}

	uploadPhoto(t, meHandlers, token)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/session", strings.NewReader(`{"accessToken":"`+string(token)+`"}`))
	rec := httptest.NewRecorder()
	authHandlers.SessionHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("session status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	reopened := decodeMe(t, rec)
	if reopened.ProfilePhotoURL == nil || !strings.Contains(*reopened.ProfilePhotoURL, session.UserID) {
		t.Fatalf("session profilePhotoUrl = %v after upload, want URL containing user id", reopened.ProfilePhotoURL)
	}
	if reopened.CreatedAt != session.CreatedAt {
		t.Fatalf("createdAt changed across sessions: %q -> %q", session.CreatedAt, reopened.CreatedAt)
	}
}

func TestUploadProfilePhotoHandler_rateLimited_returns429(t *testing.T) {
	t.Parallel()
	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	fakeStorage := storage.NewFakeClient("https://example.supabase.co")
	photos := app.NewProfilePhotoService(store, privyClient, fakeStorage).WithUploadLimiter(ratelimit.New(1, time.Minute))
	meHandlers := &MeHandlers{Sessions: app.NewSessionService(store, auth.NewFakeVerifier(), privyClient), ProfilePhoto: photos}
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "photo-limit", "Uploader")

	uploadPhoto(t, meHandlers, token)
	body, contentType := multipartPhotoBody(t, minimalPNG())
	req := httptest.NewRequest(http.MethodPost, "/v1/me/profile-photo", body)
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	meHandlers.UploadProfilePhotoHandler(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
	if len(fakeStorage.Uploads) != 1 {
		t.Fatalf("storage uploads = %d, want 1 (throttled upload must not reach storage)", len(fakeStorage.Uploads))
	}
}

func TestUploadProfilePhotoHandler_rejectsOversizedPhoto(t *testing.T) {
	t.Parallel()
	fakeStorage := storage.NewFakeClient("https://example.supabase.co")
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t, fakeStorage)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "photo-large", "Uploader")

	oversized := append(minimalPNG(), make([]byte, 2<<20)...)
	body, contentType := multipartPhotoBody(t, oversized)
	req := httptest.NewRequest(http.MethodPost, "/v1/me/profile-photo", body)
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	meHandlers.UploadProfilePhotoHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if len(fakeStorage.Uploads) != 0 {
		t.Fatalf("storage uploads = %d, want 0", len(fakeStorage.Uploads))
	}
}

func uploadPhoto(t *testing.T, handlers *MeHandlers, token auth.AccessToken) meResponse {
	t.Helper()
	body, contentType := multipartPhotoBody(t, minimalPNG())
	req := httptest.NewRequest(http.MethodPost, "/v1/me/profile-photo", body)
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handlers.UploadProfilePhotoHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	return decodeMe(t, rec)
}

// TestPATCH_me_nameAndPhotoShowOnBoards covers the profile edit path end to end:
// a renamed member with an avatar appears with the new identity on the group
// member board, the /v1/home people board, and the home dashboard leaderboard.
func TestPATCH_me_nameAndPhotoShowOnBoards(t *testing.T) {
	t.Parallel()
	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	fakeStorage := storage.NewFakeClient("https://example.supabase.co")
	sessions := app.NewSessionService(store, auth.NewFakeVerifier(), privyClient)
	meHandlers := &MeHandlers{Sessions: sessions, ProfilePhoto: app.NewProfilePhotoService(store, privyClient, fakeStorage)}
	groups := app.NewGroupService(store, privyClient)
	governance := app.NewGovernanceService(store, privyClient)
	symbols := app.NewSymbolResolver(nil)
	deposits := app.NewDepositService(store, privyClient, nil, symbols)
	home := app.NewHomeService(store, privyClient, nil, deposits, symbols)
	groupHandlers := &GroupHandlers{Groups: groups, Governance: governance, Home: home}
	homeHandlers := &HomeHandlers{Home: home}

	session, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "boards-renamed", "Alfred")
	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups", strings.NewReader(`{"name":"Board Rename Club"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(token))
	createRec := httptest.NewRecorder()
	groupHandlers.CreateGroupHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create group status = %d, want 200; body = %s", createRec.Code, createRec.Body.String())
	}
	var created createGroupResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create group: %v", err)
	}
	trackCreatedGroup(iso, created.GroupID)

	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.IncrementPositionTx(ctx, tx, session.UserID, created.GroupID, 50_000_000, 50_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit position: %v", err)
	}

	if rec := patchMe(t, meHandlers, token, `{"displayName":"Renamed Alfred"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	uploaded := uploadPhoto(t, meHandlers, token)
	if uploaded.ProfilePhotoURL == nil {
		t.Fatal("upload returned null profilePhotoUrl")
	}
	wantPhoto := *uploaded.ProfilePhotoURL

	viewReq := httptest.NewRequest(http.MethodGet, "/v1/groups/"+created.GroupID+"/view", nil)
	viewReq.SetPathValue("id", created.GroupID)
	viewReq.Header.Set("Authorization", "Bearer "+string(token))
	viewRec := httptest.NewRecorder()
	groupHandlers.GetGroupViewHandler(viewRec, viewReq)
	if viewRec.Code != http.StatusOK {
		t.Fatalf("group view status = %d, want 200; body = %s", viewRec.Code, viewRec.Body.String())
	}
	var view groupViewResponse
	if err := json.Unmarshal(viewRec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode group view: %v", err)
	}
	if len(view.Members) != 1 {
		t.Fatalf("members len = %d, want 1", len(view.Members))
	}
	member := view.Members[0]
	if member.DisplayName != "Renamed Alfred" {
		t.Fatalf("member board displayName = %q, want Renamed Alfred", member.DisplayName)
	}
	if member.ProfilePhotoURL == nil || *member.ProfilePhotoURL != wantPhoto {
		t.Fatalf("member board profilePhotoUrl = %v, want %q", member.ProfilePhotoURL, wantPhoto)
	}

	homeReq := httptest.NewRequest(http.MethodGet, "/v1/home", nil)
	homeReq.Header.Set("Authorization", "Bearer "+string(token))
	homeRec := httptest.NewRecorder()
	homeHandlers.HomeHandler(homeRec, homeReq)
	if homeRec.Code != http.StatusOK {
		t.Fatalf("home status = %d, want 200; body = %s", homeRec.Code, homeRec.Body.String())
	}
	var homePayload homeResponse
	if err := json.Unmarshal(homeRec.Body.Bytes(), &homePayload); err != nil {
		t.Fatalf("decode home: %v", err)
	}
	assertPersonOnBoard(t, "home people board", homePayload.People, session.UserID, "Renamed Alfred", wantPhoto)

	dashReq := httptest.NewRequest(http.MethodGet, "/v1/home/dashboard", nil)
	dashReq.Header.Set("Authorization", "Bearer "+string(token))
	dashRec := httptest.NewRecorder()
	homeHandlers.HomeDashboardHandler(dashRec, dashReq)
	if dashRec.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200; body = %s", dashRec.Code, dashRec.Body.String())
	}
	var dashboard homeDashboardResponse
	if err := json.Unmarshal(dashRec.Body.Bytes(), &dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	assertPersonOnBoard(t, "dashboard leaderboard", dashboard.Leaderboard.People, session.UserID, "Renamed Alfred", wantPhoto)
}

func assertPersonOnBoard(t *testing.T, board string, people []homePeopleBoardRowResponse, userID, wantName, wantPhoto string) {
	t.Helper()
	for _, person := range people {
		if person.UserID != userID {
			continue
		}
		if person.DisplayName != wantName {
			t.Fatalf("%s displayName = %q, want %q", board, person.DisplayName, wantName)
		}
		if person.ProfilePhotoURL == nil || *person.ProfilePhotoURL != wantPhoto {
			t.Fatalf("%s profilePhotoUrl = %v, want %q", board, person.ProfilePhotoURL, wantPhoto)
		}
		return
	}
	t.Fatalf("%s: user %s not found among %d people", board, userID, len(people))
}
