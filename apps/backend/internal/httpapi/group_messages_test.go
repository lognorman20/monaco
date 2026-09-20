package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

type chatTestApp struct {
	chat    *GroupMessageHandlers
	groups  *GroupHandlers
	auth    *AuthHandlers
	privy   wallets.Client
	db      *sql.DB
	iso     *postgres.TestIsolation
	groupID string
	owner   auth.AccessToken
}

// integrationChatApp seeds one cabal owned by a fresh user. limiter nil = effectively unlimited.
func integrationChatApp(t *testing.T, limiter *app.KeyedRateLimiter) chatTestApp {
	t.Helper()

	_, groupHandlers, authHandlers, privyClient, _, _, iso := integrationQuotesApp(t)
	db := postgres.OpenTestDB(t)
	store := postgres.NewStore(db)
	if limiter == nil {
		limiter = app.NewKeyedRateLimiter(1000, time.Millisecond, nil)
	}
	chat := &GroupMessageHandlers{Chat: app.NewGroupChatServiceWithLimiter(store, privyClient, limiter)}
	owner, groupID, _ := createGroupForQuotes(t, iso, groupHandlers, authHandlers, privyClient)
	return chatTestApp{
		chat:    chat,
		groups:  groupHandlers,
		auth:    authHandlers,
		privy:   privyClient,
		db:      db,
		iso:     iso,
		groupID: groupID,
		owner:   owner,
	}
}

func (a chatTestApp) joinAs(t *testing.T, label, displayName string) auth.AccessToken {
	t.Helper()
	_, token := seedAuthenticatedUser(t, a.iso, a.auth, a.privy, label, displayName)
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+a.groupID+"/join", strings.NewReader(`{}`))
	req.SetPathValue("id", a.groupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	a.groups.JoinGroupHandler(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("join status = %d, want 204; body = %s", rec.Code, rec.Body.String())
	}
	return token
}

func (a chatTestApp) post(token auth.AccessToken, groupID, rawBody string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/messages", strings.NewReader(rawBody))
	req.SetPathValue("id", groupID)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	rec := httptest.NewRecorder()
	a.chat.PostGroupMessageHandler(rec, req)
	return rec
}

func (a chatTestApp) list(token auth.AccessToken, groupID string, query url.Values) *httptest.ResponseRecorder {
	target := "/v1/groups/" + groupID + "/messages"
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.SetPathValue("id", groupID)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	rec := httptest.NewRecorder()
	a.chat.ListGroupMessagesHandler(rec, req)
	return rec
}

func messageBody(text string) string {
	b, _ := json.Marshal(postGroupMessageRequest{Body: text})
	return string(b)
}

func decodeMessagesPage(t *testing.T, rec *httptest.ResponseRecorder) listGroupMessagesResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var page listGroupMessagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list json: %v", err)
	}
	return page
}

func countGroupMessages(t *testing.T, db *sql.DB, groupID string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM group_messages WHERE group_id = $1`, groupID).Scan(&n); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	return n
}

func TestPOST_groupMessages_member_returns201WithAuthorAndTrimmedBody(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationChatApp(t, nil)

	// Act
	rec := a.post(a.owner, a.groupID, messageBody("  buy apple before earnings?  \n"))

	// Assert
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	var msg groupMessageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if msg.Body != "buy apple before earnings?" {
		t.Fatalf("body = %q, want trimmed text", msg.Body)
	}
	if msg.AuthorName != "Quotes User" || !msg.Mine || msg.GroupID != a.groupID || msg.ID == "" {
		t.Fatalf("unexpected message: %+v", msg)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, msg.CreatedAt)
	if err != nil || createdAt.Location() != time.UTC || len(msg.CreatedAt) != len("2026-09-18T15:04:05.000000Z") {
		t.Fatalf("createdAt = %q, want fixed-width microsecond RFC3339 UTC (err %v)", msg.CreatedAt, err)
	}
}

func TestGET_groupMessages_otherMemberSeesMessageAsNotMine(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationChatApp(t, nil)
	friend := a.joinAs(t, "chat-friend", "Friend")
	if rec := a.post(a.owner, a.groupID, messageBody("hello cabal")); rec.Code != http.StatusCreated {
		t.Fatalf("seed post status = %d", rec.Code)
	}
	if rec := a.post(friend, a.groupID, messageBody("hey")); rec.Code != http.StatusCreated {
		t.Fatalf("friend post status = %d", rec.Code)
	}

	// Act
	page := decodeMessagesPage(t, a.list(friend, a.groupID, nil))

	// Assert: newest first, mine flag is viewer-relative.
	if len(page.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(page.Messages))
	}
	if page.Messages[0].Body != "hey" || !page.Messages[0].Mine || page.Messages[0].AuthorName != "Friend" {
		t.Fatalf("newest = %+v, want friend's own message", page.Messages[0])
	}
	if page.Messages[1].Body != "hello cabal" || page.Messages[1].Mine {
		t.Fatalf("older = %+v, want owner's message not mine", page.Messages[1])
	}
	if page.NextCursor != "" {
		t.Fatalf("nextCursor = %q, want empty on last page", page.NextCursor)
	}
}

func TestGET_groupMessages_emptyCabal_returnsEmptyArray(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationChatApp(t, nil)

	// Act
	rec := a.list(a.owner, a.groupID, nil)

	// Assert: clients decode a non-null array.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"messages":[]`) {
		t.Fatalf("body = %s, want empty messages array", rec.Body.String())
	}
}

func TestGET_groupMessages_cursorPagesThroughAllMessagesNewestFirst(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationChatApp(t, nil)
	sent := []string{"m1", "m2", "m3", "m4", "m5"}
	for _, text := range sent {
		if rec := a.post(a.owner, a.groupID, messageBody(text)); rec.Code != http.StatusCreated {
			t.Fatalf("seed %s status = %d", text, rec.Code)
		}
	}

	// Act
	var got []string
	cursor := ""
	pages := 0
	for {
		q := url.Values{"limit": {"2"}}
		if cursor != "" {
			q.Set("before", cursor)
		}
		page := decodeMessagesPage(t, a.list(a.owner, a.groupID, q))
		pages++
		for _, m := range page.Messages {
			got = append(got, m.Body)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > 5 {
			t.Fatal("pagination did not terminate")
		}
	}

	// Assert
	want := []string{"m5", "m4", "m3", "m2", "m1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("paged bodies = %v, want %v", got, want)
	}
	if pages != 3 {
		t.Fatalf("pages = %d, want 3 (2+2+1)", pages)
	}
}

func TestGroupMessages_nonMember_returns403AndStoresNothing(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationChatApp(t, nil)
	if rec := a.post(a.owner, a.groupID, messageBody("members only")); rec.Code != http.StatusCreated {
		t.Fatalf("seed status = %d", rec.Code)
	}
	_, outsider := seedAuthenticatedUser(t, a.iso, a.auth, a.privy, "chat-outsider", "Outsider")

	// Act
	listRec := a.list(outsider, a.groupID, nil)
	postRec := a.post(outsider, a.groupID, messageBody("let me in"))

	// Assert
	if listRec.Code != http.StatusForbidden {
		t.Fatalf("list status = %d, want 403; body = %s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), "members only") {
		t.Fatal("non-member response leaked message body")
	}
	if postRec.Code != http.StatusForbidden {
		t.Fatalf("post status = %d, want 403", postRec.Code)
	}
	if n := countGroupMessages(t, a.db, a.groupID); n != 1 {
		t.Fatalf("stored messages = %d, want 1", n)
	}
}

func TestGroupMessages_unknownOrMalformedGroup_returns404(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationChatApp(t, nil)
	ids := []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid"}

	for _, id := range ids {
		// Act
		listRec := a.list(a.owner, id, nil)
		postRec := a.post(a.owner, id, messageBody("hi"))

		// Assert
		if listRec.Code != http.StatusNotFound || postRec.Code != http.StatusNotFound {
			t.Fatalf("id %q: list=%d post=%d, want 404/404", id, listRec.Code, postRec.Code)
		}
	}
}

func TestPOST_groupMessages_invalidBodies_return400(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationChatApp(t, nil)
	cases := map[string]string{
		"blank":           messageBody(" \n\t "),
		"empty":           messageBody(""),
		"missing field":   `{}`,
		"too long":        messageBody(strings.Repeat("a", app.GroupMessageMaxChars+1)),
		"malformed json":  `{"body":`,
		"oversize stream": `{"body":"` + strings.Repeat("a", maxGroupMessageRequestBytes+1) + `"}`,
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			rec := a.post(a.owner, a.groupID, raw)

			// Assert
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
			}
		})
	}
	if n := countGroupMessages(t, a.db, a.groupID); n != 0 {
		t.Fatalf("stored messages = %d, want 0", n)
	}
}

func TestPOST_groupMessages_maxLengthMultibyte_isAccepted(t *testing.T) {
	t.Parallel()
	// Arrange: limit counts characters, not bytes.
	a := integrationChatApp(t, nil)
	text := strings.Repeat("é", app.GroupMessageMaxChars)

	// Act
	rec := a.post(a.owner, a.groupID, messageBody(text))

	// Assert
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
}

func TestGET_groupMessages_badQuery_returns400(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationChatApp(t, nil)
	cases := map[string]url.Values{
		"garbage cursor":  {"before": {"not-a-cursor!"}},
		"zero limit":      {"limit": {"0"}},
		"negative limit":  {"limit": {"-3"}},
		"limit too large": {"limit": {"101"}},
		"non-int limit":   {"limit": {"ten"}},
	}

	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			rec := a.list(a.owner, a.groupID, q)

			// Assert
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGroupMessages_missingOrInvalidAuth_returns401(t *testing.T) {
	t.Parallel()
	// Arrange
	a := integrationChatApp(t, nil)

	// Act
	missingList := a.list("", a.groupID, nil)
	missingPost := a.post("", a.groupID, messageBody("hi"))
	badList := a.list("not-a-registered-token", a.groupID, nil)
	badPost := a.post("not-a-registered-token", a.groupID, messageBody("hi"))

	// Assert
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"missing list": missingList, "missing post": missingPost,
		"invalid list": badList, "invalid post": badPost,
	} {
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", name, rec.Code)
		}
	}
}

func TestPOST_groupMessages_overRateLimit_returns429WithRetryAfter(t *testing.T) {
	t.Parallel()
	// Arrange: burst of 2, one token per minute on a frozen clock.
	frozen := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	limiter := app.NewKeyedRateLimiter(2, time.Minute, func() time.Time { return frozen })
	a := integrationChatApp(t, limiter)
	friend := a.joinAs(t, "chat-ratelimit-friend", "Friend")

	// Act
	first := a.post(a.owner, a.groupID, messageBody("one"))
	second := a.post(a.owner, a.groupID, messageBody("two"))
	third := a.post(a.owner, a.groupID, messageBody("three"))
	friendPost := a.post(friend, a.groupID, messageBody("my own bucket"))

	// Assert
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("burst statuses = %d,%d, want 201,201", first.Code, second.Code)
	}
	if third.Code != http.StatusTooManyRequests {
		t.Fatalf("third status = %d, want 429", third.Code)
	}
	if got := third.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
	if friendPost.Code != http.StatusCreated {
		t.Fatalf("friend status = %d, want 201 (limit is per user)", friendPost.Code)
	}
	if n := countGroupMessages(t, a.db, a.groupID); n != 3 {
		t.Fatalf("stored messages = %d, want 3", n)
	}
}
