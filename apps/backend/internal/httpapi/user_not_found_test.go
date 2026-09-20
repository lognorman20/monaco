package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// A Privy token can be valid before the app has called POST /v1/auth/session, so
// there is no Monaco user row yet. That is the caller's state, not our failure:
// every route answers 404 "user not found", never 500.
func TestRoutes_validTokenWithoutMonacoUser_return404(t *testing.T) {
	// Arrange
	a := newMultiUserApp(t)
	withdrawals := &PlatformWithdrawHandlers{
		Withdrawals: app.NewPlatformWithdrawService(a.Store, a.Privy, a.DepositService, nil, "relayer-test-key"),
	}
	token := privy.AccessToken(a.ISO.UniqueToken("no-monaco-user"))
	privy.RegisterToken(a.Privy, token, privy.Identity{
		PrivyUserID: a.ISO.UniquePrivyID("no-monaco-user"),
		DisplayName: "Not Signed In Yet",
	})
	member := a.signIn(t, "no-user-club-owner", "Club Owner")
	c := a.createClub(t, member, "No User Club")

	cases := []struct {
		name       string
		handler    http.HandlerFunc
		target     string
		pathValues []string
	}{
		{"get deposit", a.Deposits.GetDepositHandler, "/v1/deposits/dep-unknown", []string{"id", "dep-unknown"}},
		{"share units", a.Deposits.GetMemberShareUnitsHandler, "/v1/groups/" + c.ID + "/share-units", []string{"id", c.ID}},
		{"treasury usdc", a.Deposits.GetTreasuryUsdcBalanceHandler, "/v1/groups/" + c.ID + "/treasury/usdc", []string{"id", c.ID}},
		{"get platform withdrawal", withdrawals.GetPlatformWithdrawalHandler, "/v1/me/withdrawals/wd-unknown", []string{"id", "wd-unknown"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			rec := a.call(t, tc.handler, http.MethodGet, tc.target, token, "", tc.pathValues...)

			// Assert
			requireStatus(t, rec, http.StatusNotFound, tc.name)
			var payload errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error body: %v; body = %s", err, rec.Body.String())
			}
			if payload.Error != "user not found" {
				t.Fatalf("error = %q, want %q", payload.Error, "user not found")
			}
		})
	}

	// An unknown token on the same routes is still 401, not 404.
	for _, tc := range cases {
		t.Run(tc.name+" invalid token", func(t *testing.T) {
			rec := a.call(t, tc.handler, http.MethodGet, tc.target, privy.AccessToken("never-registered-"+a.ISO.Suffix()), "", tc.pathValues...)
			requireStatus(t, rec, http.StatusUnauthorized, tc.name)
		})
	}
}
