package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func newTestHealthHandler() http.HandlerFunc {
	return HealthHandler
}

func testHTTPRequest(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

func seedAuthenticatedUser(t *testing.T, handlers *AuthHandlers, privyClient privy.Client, token privy.AccessToken, identity privy.Identity) authSessionResponse {
	t.Helper()

	privy.RegisterToken(privyClient, token, identity)
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
	return payload
}
