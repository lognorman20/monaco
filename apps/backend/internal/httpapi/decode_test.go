package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
)

// bodyRoute is one body-reading route served the way production serves it:
// behind LimitRequestBody. Every handler decodes before it touches a service,
// so zero-value services are enough to reach the body.
type bodyRoute struct {
	name    string
	pattern string
	path    string
	handler http.HandlerFunc
	header  map[string]string
}

func bodyRoutes() []bodyRoute {
	auth := &AuthHandlers{}
	groups := &GroupHandlers{Redeem: &app.RedeemService{}}
	proposals := &ProposalHandlers{}
	bearer := map[string]string{"Authorization": "Bearer token"}
	return []bodyRoute{
		{name: "auth session", pattern: "POST /v1/auth/session", path: "/v1/auth/session", handler: auth.SessionHandler},
		{name: "patch me", pattern: "PATCH /v1/me", path: "/v1/me", handler: (&MeHandlers{}).PatchMeHandler, header: bearer},
		{name: "platform withdrawal", pattern: "POST /v1/me/withdrawals", path: "/v1/me/withdrawals", handler: (&PlatformWithdrawHandlers{}).CreatePlatformWithdrawalHandler, header: bearer},
		{name: "create group", pattern: "POST /v1/groups", path: "/v1/groups", handler: groups.CreateGroupHandler, header: bearer},
		{name: "leave group (optional body)", pattern: "POST /v1/groups/{id}/leave", path: "/v1/groups/g1/leave", handler: groups.LeaveGroupHandler, header: bearer},
		{name: "withdraw to balance (optional body)", pattern: "POST /v1/groups/{id}/withdraw-to-balance", path: "/v1/groups/g1/withdraw-to-balance", handler: groups.WithdrawToBalanceHandler, header: bearer},
		{name: "fund group", pattern: "POST /v1/groups/{id}/fund", path: "/v1/groups/g1/fund", handler: (&DepositHandlers{}).FundGroupHandler, header: bearer},
		{name: "quote", pattern: "POST /v1/groups/{id}/quotes", path: "/v1/groups/g1/quotes", handler: (&QuoteHandlers{}).QuoteHandler, header: bearer},
		{name: "create proposal", pattern: "POST /v1/groups/{id}/proposals", path: "/v1/groups/g1/proposals", handler: proposals.CreateProposalHandler, header: bearer},
		{name: "cast vote", pattern: "POST /v1/proposals/{id}/votes", path: "/v1/proposals/p1/votes", handler: proposals.CastVoteHandler, header: bearer},
		{name: "proposal comment", pattern: "POST /v1/proposals/{id}/comments", path: "/v1/proposals/p1/comments", handler: proposals.CreateProposalCommentHandler, header: bearer},
		{name: "group message", pattern: "POST /v1/groups/{id}/messages", path: "/v1/groups/g1/messages", handler: (&GroupMessageHandlers{}).PostGroupMessageHandler, header: bearer},
		{name: "agent intent", pattern: "POST /v1/groups/{id}/agents/intents", path: "/v1/groups/g1/agents/intents", handler: (&AgentHandlers{}).SubmitAgentIntentHandler, header: map[string]string{agentKeyHeader: "key"}},
	}
}

func serveBodyRoute(t *testing.T, route bodyRoute, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(route.pattern, route.handler)
	method, _, _ := strings.Cut(route.pattern, " ")
	req := httptest.NewRequest(method, route.path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	for k, v := range route.header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	Chain(mux, LimitRequestBody(DefaultMaxRequestBytes)).ServeHTTP(rec, req)
	return rec
}

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantMessage string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, rec.Body.String())
	}
	var got errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v; body = %s", err, rec.Body.String())
	}
	if got.Error != wantMessage {
		t.Fatalf("error = %q, want %q", got.Error, wantMessage)
	}
}

func TestJSONRoutes_bodyOverCap_returns413(t *testing.T) {
	t.Parallel()
	// Arrange: well-formed JSON one field too big for the 64 KiB default cap.
	oversized := `{"padding":"` + strings.Repeat("a", int(DefaultMaxRequestBytes)) + `"}`

	for _, route := range bodyRoutes() {
		t.Run(route.name, func(t *testing.T) {
			// Act
			rec := serveBodyRoute(t, route, "application/json", oversized)

			// Assert
			assertErrorResponse(t, rec, http.StatusRequestEntityTooLarge, "request body too large")
		})
	}
}

func TestJSONRoutes_malformedBody_returns400(t *testing.T) {
	t.Parallel()
	for _, route := range bodyRoutes() {
		t.Run(route.name, func(t *testing.T) {
			// Act
			rec := serveBodyRoute(t, route, "application/json", `{"padding":`)

			// Assert
			assertErrorResponse(t, rec, http.StatusBadRequest, "invalid request body")
		})
	}
}

func profilePhotoRoute() bodyRoute {
	me := &MeHandlers{ProfilePhoto: &app.ProfilePhotoService{}}
	return bodyRoute{
		name:    "profile photo",
		pattern: "POST /v1/me/profile-photo",
		path:    "/v1/me/profile-photo",
		handler: me.UploadProfilePhotoHandler,
		header:  map[string]string{"Authorization": "Bearer token"},
	}
}

func TestPOST_profilePhoto_uploadOverCap_returns413(t *testing.T) {
	t.Parallel()
	// Arrange: a multipart upload whose photo part alone exceeds the route cap.
	const boundary = "monaco-boundary"
	body := "--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"photo\"; filename=\"p.jpg\"\r\n" +
		"Content-Type: image/jpeg\r\n\r\n" +
		strings.Repeat("a", 2<<20+(2<<10)) + "\r\n--" + boundary + "--\r\n"

	// Act
	rec := serveBodyRoute(t, profilePhotoRoute(), "multipart/form-data; boundary="+boundary, body)

	// Assert
	assertErrorResponse(t, rec, http.StatusRequestEntityTooLarge, "photo must be at most 2MB")
}

func TestPOST_profilePhoto_malformedMultipart_returns400(t *testing.T) {
	t.Parallel()
	// Act
	rec := serveBodyRoute(t, profilePhotoRoute(), "multipart/form-data; boundary=monaco-boundary", "not multipart")

	// Assert
	assertErrorResponse(t, rec, http.StatusBadRequest, "invalid multipart form")
}
