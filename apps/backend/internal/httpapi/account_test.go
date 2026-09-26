package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// stubSliceValuer prices every slice at one figure, or fails when failing is set.
type stubSliceValuer struct {
	micros  int64
	failing bool
}

func (v stubSliceValuer) MemberSliceMicros(context.Context, string, string) (int64, error) {
	if v.failing {
		return 0, errors.New("pot unpriced")
	}
	return v.micros, nil
}

type accountTestApp struct {
	auth    *AuthHandlers
	account *AccountHandlers
	privy   privy.Client
	store   *postgres.Store
	iso     *postgres.TestIsolation
}

func newAccountTestApp(t *testing.T, valuer app.SliceValuer) accountTestApp {
	t.Helper()
	auth, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	return accountTestApp{
		auth:    auth,
		account: &AccountHandlers{Accounts: app.NewAccountService(store, privyClient, valuer)},
		privy:   privyClient,
		store:   store,
		iso:     iso,
	}
}

func (a accountTestApp) serve(handler http.HandlerFunc, method, path string, token privy.AccessToken, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// sliceIn gives userID share units in a new cabal it creates, and returns the cabal's id.
func (a accountTestApp) sliceIn(t *testing.T, token privy.AccessToken, userID, name string, units int64) string {
	t.Helper()
	ctx := context.Background()
	governance := app.NewGovernanceService(a.store, a.privy)
	group, err := governance.CreateGroupWithRules(ctx, string(token), a.iso.Suffix()+" "+name, app.DefaultGroupRules())
	if err != nil {
		t.Fatalf("create cabal: %v", err)
	}
	a.iso.TrackGroup(group.GroupID)
	tx, err := a.store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := a.store.IncrementPositionTx(ctx, tx, userID, group.GroupID, units, units); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return group.GroupID
}

func decodeJSONMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v; body = %s", err, rec.Body.String())
	}
	return payload
}

func TestGET_mePreferences_defaultsToEverythingOn(t *testing.T) {
	t.Parallel()

	// Arrange
	a := newAccountTestApp(t, nil)
	_, token := seedAuthenticatedUser(t, a.iso, a.auth, a.privy, "prefs-get", "Ines Moreau")

	// Act
	rec := a.serve(a.account.GetPreferencesHandler, http.MethodGet, "/v1/me/preferences", token, "")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	want := `{"notifications":{"proposals":true,"results":true,"chat":true,"money":true}}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestPATCH_mePreferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantBody   string
		wantReason string
	}{
		{
			name:       "merges one switch and returns the whole document",
			body:       `{"notifications":{"chat":false}}`,
			wantStatus: http.StatusOK,
			wantBody:   `{"notifications":{"proposals":true,"results":true,"chat":false,"money":true}}`,
		},
		{
			name:       "unknown key is refused",
			body:       `{"notifications":{"marketing":false}}`,
			wantStatus: http.StatusBadRequest,
			wantReason: "invalid_preferences",
		},
		{
			name:       "non-boolean is refused",
			body:       `{"notifications":{"chat":"off"}}`,
			wantStatus: http.StatusBadRequest,
			wantReason: "invalid_preferences",
		},
		{
			name:       "malformed JSON is a bad body",
			body:       `{"notifications":`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "trailing data is a bad body",
			body:       `{"notifications":{"chat":false}} {}`,
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			a := newAccountTestApp(t, nil)
			_, token := seedAuthenticatedUser(t, a.iso, a.auth, a.privy, "prefs-patch", "Jonas Berg")

			// Act
			rec := a.serve(a.account.PatchPreferencesHandler, http.MethodPatch, "/v1/me/preferences", token, tt.body)

			// Assert
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantBody != "" && strings.TrimSpace(rec.Body.String()) != tt.wantBody {
				t.Fatalf("body = %s, want %s", rec.Body.String(), tt.wantBody)
			}
			if tt.wantReason != "" {
				if reason := decodeJSONMap(t, rec)["reason"]; reason != tt.wantReason {
					t.Fatalf("reason = %v, want %s", reason, tt.wantReason)
				}
			}
		})
	}
}

func TestAccountRoutes_requireAuth(t *testing.T) {
	t.Parallel()

	a := newAccountTestApp(t, nil)
	routes := []struct {
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{http.MethodGet, "/v1/me/preferences", a.account.GetPreferencesHandler},
		{http.MethodPatch, "/v1/me/preferences", a.account.PatchPreferencesHandler},
		{http.MethodGet, "/v1/me/deletion-check", a.account.DeletionCheckHandler},
		{http.MethodDelete, "/v1/me", a.account.DeleteMeHandler},
	}
	for _, route := range routes {
		// Act
		rec := a.serve(route.handler, route.method, route.path, "", "")

		// Assert
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", route.method, route.path, rec.Code)
		}
	}
}

func TestGET_meDeletionCheck_emptyAccount(t *testing.T) {
	t.Parallel()

	// Arrange
	a := newAccountTestApp(t, stubSliceValuer{})
	_, token := seedAuthenticatedUser(t, a.iso, a.auth, a.privy, "check-empty", "Kofi Mensah")

	// Act
	rec := a.serve(a.account.DeletionCheckHandler, http.MethodGet, "/v1/me/deletion-check", token, "")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"canDelete":true,"blockers":[]}` {
		t.Fatalf("body = %s, want canDelete with an empty list", got)
	}
}

func TestGET_meDeletionCheck_listsBlockers(t *testing.T) {
	t.Parallel()

	// Arrange
	a := newAccountTestApp(t, stubSliceValuer{micros: 245_120_000})
	session, token := seedAuthenticatedUser(t, a.iso, a.auth, a.privy, "check-blocked", "Lena Fischer")
	groupID := a.sliceIn(t, token, session.UserID, "Sunday Investors", 200_000_000)
	privy.SetMemberUSDCBalance(a.privy, session.MemberWalletAddress, 30_000_000)

	// Act
	rec := a.serve(a.account.DeletionCheckHandler, http.MethodGet, "/v1/me/deletion-check", token, "")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload deletionCheckResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.CanDelete || len(payload.Blockers) != 2 {
		t.Fatalf("payload = %+v, want two blockers", payload)
	}
	slice, balance := payload.Blockers[0], payload.Blockers[1]
	if slice.Kind != "cabal_slice" || slice.GroupID != groupID || slice.GroupName != a.iso.Suffix()+" Sunday Investors" || slice.ValueUsd == nil || *slice.ValueUsd != "245.12" {
		t.Fatalf("slice blocker = %+v", slice)
	}
	if balance.Kind != "account_balance" || balance.GroupID != "" || balance.ValueUsd == nil || *balance.ValueUsd != "30.00" {
		t.Fatalf("balance blocker = %+v", balance)
	}
}

func TestGET_meDeletionCheck_unpricedSliceHasNullValue(t *testing.T) {
	t.Parallel()

	// Arrange
	a := newAccountTestApp(t, stubSliceValuer{failing: true})
	session, token := seedAuthenticatedUser(t, a.iso, a.auth, a.privy, "check-unpriced", "Mateo Silva")
	a.sliceIn(t, token, session.UserID, "Night Owls", 1_000_000)

	// Act
	rec := a.serve(a.account.DeletionCheckHandler, http.MethodGet, "/v1/me/deletion-check", token, "")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"valueUsd":null`) {
		t.Fatalf("body = %s, want valueUsd null for an unpriced slice", rec.Body.String())
	}
}

func TestDELETE_me_withBlockersIs409WithTheBlockers(t *testing.T) {
	t.Parallel()

	// Arrange
	a := newAccountTestApp(t, stubSliceValuer{micros: 12_000_000})
	session, token := seedAuthenticatedUser(t, a.iso, a.auth, a.privy, "delete-blocked", "Nia Brooks")
	a.sliceIn(t, token, session.UserID, "Semis or Bust", 12_000_000)

	// Act
	rec := a.serve(a.account.DeleteMeHandler, http.MethodDelete, "/v1/me", token, "")

	// Assert
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
	var payload deletionBlockedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Reason != "account_not_empty" || payload.CanDelete || len(payload.Blockers) != 1 || payload.Blockers[0].Kind != "cabal_slice" {
		t.Fatalf("payload = %+v, want account_not_empty with the slice", payload)
	}
	if payload.Error == "" {
		t.Fatal("409 carries no error message")
	}
}

func TestDELETE_me_deletesOnceAndRepeatsTheAnswer(t *testing.T) {
	t.Parallel()

	// Arrange
	a := newAccountTestApp(t, stubSliceValuer{})
	_, token := seedAuthenticatedUser(t, a.iso, a.auth, a.privy, "delete-ok", "Owen Price")

	// Act
	first := a.serve(a.account.DeleteMeHandler, http.MethodDelete, "/v1/me", token, "")
	second := a.serve(a.account.DeleteMeHandler, http.MethodDelete, "/v1/me", token, "")

	// Assert
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d, want 200 twice; bodies = %s / %s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("repeat answered %s, first answered %s", second.Body.String(), first.Body.String())
	}
	if deletedAt, _ := decodeJSONMap(t, first)["deletedAt"].(string); deletedAt == "" {
		t.Fatalf("body = %s, want deletedAt", first.Body.String())
	}
}

func TestPOST_authSession_deletedAccountIs410(t *testing.T) {
	t.Parallel()

	// Arrange
	a := newAccountTestApp(t, stubSliceValuer{})
	_, token := seedAuthenticatedUser(t, a.iso, a.auth, a.privy, "delete-reopen", "Pia Larsen")
	if rec := a.serve(a.account.DeleteMeHandler, http.MethodDelete, "/v1/me", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d; body = %s", rec.Code, rec.Body.String())
	}

	// Act
	rec := a.serve(a.auth.SessionHandler, http.MethodPost, "/v1/auth/session", "", `{"accessToken":"`+string(token)+`"}`)

	// Assert
	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410; body = %s", rec.Code, rec.Body.String())
	}
	if reason := decodeJSONMap(t, rec)["reason"]; reason != "account_deleted" {
		t.Fatalf("reason = %v, want account_deleted", reason)
	}
	pref := a.serve(a.account.GetPreferencesHandler, http.MethodGet, "/v1/me/preferences", token, "")
	if pref.Code != http.StatusNotFound {
		t.Fatalf("preferences after delete status = %d, want 404", pref.Code)
	}
}
