package httpapi

import (
	"database/sql"
	"encoding/json"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

func newTestHealthHandler() http.HandlerFunc {
	return HealthHandler
}

func testHTTPRequest(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

func integrationApp(t *testing.T) (*AuthHandlers, wallets.Client, *sql.DB, *postgres.TestIsolation) {
	t.Helper()
	db := postgres.OpenTestDB(t)
	iso := postgres.PrepareTestDB(t, db)
	store := postgres.NewStore(db)
	walletClient := wallets.NewFakeClient()
	verifier := auth.NewFakeVerifier()
	sessions := app.NewSessionService(store, verifier, walletClient)
	return &AuthHandlers{Sessions: sessions, Verifier: verifier}, walletClient, db, iso
}

func seedAuthenticatedUser(t *testing.T, iso *postgres.TestIsolation, handlers *AuthHandlers, walletClient wallets.Client, label string, displayName string) (authSessionResponse, auth.AccessToken) {
	t.Helper()
	_ = walletClient

	token := auth.AccessToken(iso.UniqueToken(label))
	identity := auth.Identity{
		DynamicUserID: iso.UniqueDynamicID(label),
		DisplayName:   displayName,
	}
	auth.RegisterToken(handlers.Verifier, token, identity)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/session", strings.NewReader(`{"accessToken":"`+string(token)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlers.SessionHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("seed session status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload authSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode session json: %v", err)
	}
	iso.TrackUser(payload.UserID)
	return payload, token
}

func trackCreatedGroup(iso *postgres.TestIsolation, groupID string) {
	iso.TrackGroup(groupID)
}
