package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func integrationMeHandlers(t *testing.T) (*MeHandlers, *AuthHandlers, privy.Client, *postgres.TestIsolation) {
	t.Helper()

	authHandlers, privyClient, _, iso := integrationApp(t)
	me := &MeHandlers{Sessions: authHandlers.Sessions}
	return me, authHandlers, privyClient, iso
}

func TestPATCH_me_authenticated_setsDisplayName(t *testing.T) {
	t.Parallel()
	// Arrange
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-me", "")
	req := httptest.NewRequest(http.MethodPatch, "/v1/me", strings.NewReader(`{"displayName":"Logan"}`))
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Act
	meHandlers.PatchMeHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.DisplayName != "Logan" {
		t.Fatalf("displayName = %q, want Logan", payload.DisplayName)
	}
	if payload.UserID == "" {
		t.Fatal("expected userId in response")
	}
	if payload.MemberWalletAddress == "" {
		t.Fatal("expected memberWalletAddress in response")
	}
}

func TestPATCH_me_emptyDisplayName_returns400(t *testing.T) {
	t.Parallel()
	// Arrange
	meHandlers, authHandlers, privyClient, iso := integrationMeHandlers(t)
	_, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "patch-empty", "Alfred")
	req := httptest.NewRequest(http.MethodPatch, "/v1/me", strings.NewReader(`{"displayName":"   "}`))
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Act
	meHandlers.PatchMeHandler(rec, req)

	// Assert
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestPATCH_me_missingAuth_returns401(t *testing.T) {
	t.Parallel()
	// Arrange
	meHandlers, _, _, _ := integrationMeHandlers(t)
	req := httptest.NewRequest(http.MethodPatch, "/v1/me", strings.NewReader(`{"displayName":"Logan"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Act
	meHandlers.PatchMeHandler(rec, req)

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
}
