package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

func notificationHandlersForTest(t *testing.T) (*NotificationHandlers, *postgres.Store, string, string) {
	t.Helper()
	auth, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	session, token := seedAuthenticatedUser(t, iso, auth, privyClient, "inbox", "Priya")
	return &NotificationHandlers{
		Inbox:      app.NewNotificationService(store, privyClient),
		Governance: app.NewGovernanceService(store, privyClient),
	}, store, session.UserID, string(token)
}

func serveNotification(handler http.HandlerFunc, method, path, token, body string, pathValues map[string]string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestNotificationRoutes_listShapeAndMarkRead(t *testing.T) {
	// Arrange
	h, store, userID, token := notificationHandlersForTest(t)
	notifier := app.NewNotifier(store, app.LogPushSender{}).SendPushInline()
	notifier.Notify(context.Background(), []string{userID}, app.Notification{Kind: app.NotifyFundsArrived, Title: "$500 arrived in your balance", Body: "Put it into a cabal when you're ready."})

	// Act
	rec := serveNotification(h.ListNotificationsHandler, http.MethodGet, "/v1/me/notifications?limit=10", token, "", nil)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Notifications []map[string]any `json:"notifications"`
		UnreadCount   int              `json:"unreadCount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.UnreadCount != 1 || len(page.Notifications) != 1 {
		t.Fatalf("page = %+v", page)
	}
	row := page.Notifications[0]
	for key, want := range map[string]any{"kind": "funds_arrived", "category": "money", "title": "$500 arrived in your balance", "groupId": nil, "readAt": nil} {
		if row[key] != want {
			t.Fatalf("%s = %v, want %v (row %v)", key, row[key], want, row)
		}
	}
	if _, err := time.Parse(time.RFC3339, row["createdAt"].(string)); err != nil {
		t.Fatalf("createdAt %v: %v", row["createdAt"], err)
	}

	read := serveNotification(h.MarkNotificationsReadHandler, http.MethodPost, "/v1/me/notifications/read", token, `{"ids":["`+row["id"].(string)+`"]}`, nil)
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"unreadCount":0`) {
		t.Fatalf("read = %d %s", read.Code, read.Body.String())
	}
}

func TestNotificationRoutes_refusals(t *testing.T) {
	h, _, _, token := notificationHandlersForTest(t)
	validToken := strings.Repeat("ab", 32)
	cases := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
		token   string
		body    string
		values  map[string]string
		want    int
	}{
		{name: "list without auth", handler: h.ListNotificationsHandler, method: http.MethodGet, path: "/v1/me/notifications", want: http.StatusUnauthorized},
		{name: "list bad limit", handler: h.ListNotificationsHandler, method: http.MethodGet, path: "/v1/me/notifications?limit=abc", token: token, want: http.StatusBadRequest},
		{name: "list limit too big", handler: h.ListNotificationsHandler, method: http.MethodGet, path: "/v1/me/notifications?limit=500", token: token, want: http.StatusBadRequest},
		{name: "list bad cursor", handler: h.ListNotificationsHandler, method: http.MethodGet, path: "/v1/me/notifications?cursor=%21%21", token: token, want: http.StatusBadRequest},
		{name: "read with neither", handler: h.MarkNotificationsReadHandler, method: http.MethodPost, path: "/v1/me/notifications/read", token: token, body: `{}`, want: http.StatusBadRequest},
		{name: "read malformed", handler: h.MarkNotificationsReadHandler, method: http.MethodPost, path: "/v1/me/notifications/read", token: token, body: `{`, want: http.StatusBadRequest},
		{name: "read all", handler: h.MarkNotificationsReadHandler, method: http.MethodPost, path: "/v1/me/notifications/read", token: token, body: `{"all":true}`, want: http.StatusOK},
		{name: "device bad token", handler: h.RegisterDeviceHandler, method: http.MethodPut, path: "/v1/me/devices", token: token, body: `{"token":"xyz","platform":"ios","appEnv":"debug"}`, want: http.StatusBadRequest},
		{name: "device bad env", handler: h.RegisterDeviceHandler, method: http.MethodPut, path: "/v1/me/devices", token: token, body: `{"token":"` + validToken + `","platform":"ios","appEnv":"beta"}`, want: http.StatusBadRequest},
		{name: "device registered", handler: h.RegisterDeviceHandler, method: http.MethodPut, path: "/v1/me/devices", token: token, body: `{"token":"` + validToken + `","platform":"ios","appEnv":"debug"}`, want: http.StatusNoContent},
		{name: "device removed", handler: h.UnregisterDeviceHandler, method: http.MethodDelete, path: "/v1/me/devices/" + validToken, token: token, values: map[string]string{"token": validToken}, want: http.StatusNoContent},
		{name: "device remove bad token", handler: h.UnregisterDeviceHandler, method: http.MethodDelete, path: "/v1/me/devices/x", token: token, values: map[string]string{"token": "x"}, want: http.StatusBadRequest},
		{name: "nudge unknown proposal", handler: h.NudgeProposalHandler, method: http.MethodPost, path: "/v1/proposals/00000000-0000-0000-0000-000000000000/nudge", token: token, values: map[string]string{"id": "00000000-0000-0000-0000-000000000000"}, want: http.StatusNotFound},
		{name: "nudge without auth", handler: h.NudgeProposalHandler, method: http.MethodPost, path: "/v1/proposals/x/nudge", values: map[string]string{"id": "x"}, want: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveNotification(tc.handler, tc.method, tc.path, tc.token, tc.body, tc.values)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestWriteNudgeError_rateLimitedCarriesRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/proposals/p/nudge", nil)
	log := newRequestLog(req, "POST /v1/proposals/{id}/nudge")

	writeNudgeError(req.Context(), log, rec, &app.RateLimitError{RetryAfter: 41*time.Minute + 200*time.Millisecond})

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "2461" {
		t.Fatalf("Retry-After = %q, want 2461", got)
	}
	if !strings.Contains(rec.Body.String(), "reminded less than an hour ago") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
