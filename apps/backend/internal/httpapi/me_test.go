package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/privy"
)

func integrationMeApp(t *testing.T) (*MeHandlers, privy.Client) {
	t.Helper()

	authHandlers, privyClient, _ := integrationApp(t)
	return &MeHandlers{Sessions: authHandlers.Sessions}, privyClient
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

func TestGET_me_authenticated_returnsUserIdDisplayNameAndMemberAddress(t *testing.T) {
	// Arrange
	authHandlers, privyClient, _ := integrationApp(t)
	meHandlers := &MeHandlers{Sessions: authHandlers.Sessions}
	token := fixtureSessionToken()
	session := seedAuthenticatedUser(t, authHandlers, privyClient, token, privy.Identity{
		PrivyUserID: "did:privy:alfred",
		DisplayName: "Alfred",
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	meHandlers.MeHandler(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var payload meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload.UserID != session.UserID {
		t.Fatalf("user_id = %q, want %q", payload.UserID, session.UserID)
	}
	if payload.DisplayName != "Alfred" {
		t.Fatalf("display_name = %q, want Alfred", payload.DisplayName)
	}
	if payload.MemberWalletAddress != session.MemberWalletAddress {
		t.Fatalf("member_wallet_address = %q, want %q", payload.MemberWalletAddress, session.MemberWalletAddress)
	}
}

func TestGET_me_missingAuth_returns401(t *testing.T) {
	// Arrange
	meHandlers, _ := integrationMeApp(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	rec := httptest.NewRecorder()

	// Act
	meHandlers.MeHandler(rec, req)

	// Assert
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGET_me_unknownUser_returns404(t *testing.T) {
	// Arrange
	meHandlers, privyClient := integrationMeApp(t)
	token := privy.AccessToken("orphan-session-token")
	privy.RegisterToken(privyClient, token, privy.Identity{
		PrivyUserID: "did:privy:unknown-user",
		DisplayName: "Ghost",
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()

	// Act
	meHandlers.MeHandler(rec, req)

	// Assert
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}
