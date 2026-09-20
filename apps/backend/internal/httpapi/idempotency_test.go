package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

const testIdempotencyKey = "3f0e7c1a-5b7d-4a53-9a65-2f4f4f0f9a11"

// idempotencyHarness runs the middleware over a counting money handler against the real
// test database and the fake Privy client.
type idempotencyHarness struct {
	t       *testing.T
	db      *sql.DB
	auth    *AuthHandlers
	privy   privy.Client
	iso     *postgres.TestIsolation
	runs    atomic.Int64
	respond func(w http.ResponseWriter, r *http.Request, run int64)
	handler http.Handler
}

func newIdempotencyHarness(t *testing.T) *idempotencyHarness {
	t.Helper()
	authHandlers, privyClient, db, iso := integrationApp(t)
	h := &idempotencyHarness{t: t, db: db, auth: authHandlers, privy: privyClient, iso: iso}
	h.respond = func(w http.ResponseWriter, _ *http.Request, run int64) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"depositId":"dep-` + strconv.FormatInt(run, 10) + `"}`))
	}
	money := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.respond(w, r, h.runs.Add(1))
	})
	h.handler = NewIdempotency(postgres.NewStore(db), privyClient).Middleware()(money)
	return h
}

func (h *idempotencyHarness) signIn(label string) privy.AccessToken {
	h.t.Helper()
	_, token := seedAuthenticatedUser(h.t, h.iso, h.auth, h.privy, label, label)
	return token
}

func (h *idempotencyHarness) post(path string, token privy.AccessToken, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	if key != "" {
		req.Header.Set(IdempotencyKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func TestIdempotency_sameKeySameBodyReplaysStoredResponse(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-replay")

	first := h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":5000000}`)
	requireStatus(t, first, http.StatusOK, "first fund")
	if first.Header().Get(IdempotencyStatusHeader) != "" {
		t.Fatalf("first response must not be marked replayed")
	}

	// The client lost the response and retries; it may re-encode the same payload.
	retry := h.post("/v1/groups/g1/fund", token, testIdempotencyKey, ` { "amount" : 5000000 } `)
	requireStatus(t, retry, http.StatusOK, "retried fund")
	if got := h.runs.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
	if retry.Body.String() != first.Body.String() {
		t.Fatalf("replayed body = %s, want %s", retry.Body.String(), first.Body.String())
	}
	if retry.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("replayed content type = %q", retry.Header().Get("Content-Type"))
	}
	if retry.Header().Get(IdempotencyStatusHeader) != idempotencyStatusReplayed {
		t.Fatalf("replay must set %s", IdempotencyStatusHeader)
	}
}

func TestIdempotency_replaysStoredClientError(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-4xx")
	h.respond = func(w http.ResponseWriter, r *http.Request, _ int64) {
		writeJSONError(r.Context(), w, http.StatusBadRequest, "amount exceeds available platform balance")
	}

	first := h.post("/v1/me/withdrawals", token, testIdempotencyKey, `{"amount":1,"toAddress":"x"}`)
	retry := h.post("/v1/me/withdrawals", token, testIdempotencyKey, `{"toAddress":"x","amount":1}`)
	requireStatus(t, first, http.StatusBadRequest, "first withdrawal")
	requireStatus(t, retry, http.StatusBadRequest, "retried withdrawal")
	if got := h.runs.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
	if retry.Body.String() != first.Body.String() {
		t.Fatalf("replayed body = %s, want %s", retry.Body.String(), first.Body.String())
	}
}

func TestIdempotency_sameKeyDifferentBodyIsUnprocessable(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-mismatch")

	requireStatus(t, h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":5000000}`), http.StatusOK, "first fund")

	otherAmount := h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":9000000}`)
	requireStatus(t, otherAmount, http.StatusUnprocessableEntity, "same key, other amount")
	otherRoute := h.post("/v1/groups/g2/fund", token, testIdempotencyKey, `{"amount":5000000}`)
	requireStatus(t, otherRoute, http.StatusUnprocessableEntity, "same key, other group")
	if got := h.runs.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}

func TestIdempotency_concurrentDuplicatesRunOnce(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-concurrent")

	const callers = 8
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	h.respond = func(w http.ResponseWriter, _ *http.Request, _ int64) {
		startOnce.Do(func() { close(started) })
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"withdrawalId":"w-1"}`))
	}

	codes := make(chan int, callers)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		codes <- h.post("/v1/me/withdrawals", token, testIdempotencyKey, `{"amount":1}`).Code
	}()
	<-started
	duplicate := h.post("/v1/me/withdrawals", token, testIdempotencyKey, `{"amount":1}`)
	requireStatus(t, duplicate, http.StatusConflict, "duplicate while in progress")
	if got := duplicate.Header().Get(IdempotencyStatusHeader); got != idempotencyStatusInProgress {
		t.Fatalf("%s = %q, want %q", IdempotencyStatusHeader, got, idempotencyStatusInProgress)
	}
	// The first request holds the claim: every duplicate must be refused, not queued or run.
	for i := 1; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- h.post("/v1/me/withdrawals", token, testIdempotencyKey, `{"amount":1}`).Code
		}()
	}
	for i := 1; i < callers; i++ {
		if code := <-codes; code != http.StatusConflict {
			t.Errorf("duplicate while in progress: status = %d, want 409", code)
		}
	}
	close(release)
	wg.Wait()
	if code := <-codes; code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200", code)
	}
	if got := h.runs.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}

	after := h.post("/v1/me/withdrawals", token, testIdempotencyKey, `{"amount":1}`)
	requireStatus(t, after, http.StatusOK, "retry after completion")
	if after.Body.String() != `{"withdrawalId":"w-1"}` {
		t.Fatalf("replayed body = %s", after.Body.String())
	}
}

func TestIdempotency_racingDuplicatesRunOnce(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-race")

	const callers = 12
	start := make(chan struct{})
	codes := make(chan int, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes <- h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":5000000}`).Code
		}()
	}
	close(start)
	wg.Wait()
	close(codes)

	for code := range codes {
		if code != http.StatusOK && code != http.StatusConflict {
			t.Errorf("status = %d, want 200 (owner or replay) or 409 (in progress)", code)
		}
	}
	if got := h.runs.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}

func TestIdempotency_keyIsScopedToUser(t *testing.T) {
	h := newIdempotencyHarness(t)
	alice := h.signIn("idem-alice")
	bob := h.signIn("idem-bob")

	a := h.post("/v1/groups/g1/fund", alice, testIdempotencyKey, `{"amount":5000000}`)
	b := h.post("/v1/groups/g1/fund", bob, testIdempotencyKey, `{"amount":7000000}`)
	requireStatus(t, a, http.StatusOK, "alice fund")
	requireStatus(t, b, http.StatusOK, "bob fund with alice's key")
	if got := h.runs.Load(); got != 2 {
		t.Fatalf("handler ran %d times, want 2", got)
	}
	if a.Body.String() == b.Body.String() {
		t.Fatalf("bob received alice's stored response: %s", b.Body.String())
	}
	if b.Header().Get(IdempotencyStatusHeader) != "" {
		t.Fatalf("bob's first request must not be a replay")
	}
}

func TestIdempotency_keyExpiresAfterTTL(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-expiry")

	requireStatus(t, h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":5000000}`), http.StatusOK, "first fund")

	h.backdate(testIdempotencyKey, idempotencyKeyTTL-time.Minute)
	stillValid := h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":5000000}`)
	requireStatus(t, stillValid, http.StatusOK, "retry inside ttl")
	if got := h.runs.Load(); got != 1 {
		t.Fatalf("handler ran %d times inside ttl, want 1", got)
	}

	h.backdate(testIdempotencyKey, idempotencyKeyTTL+time.Minute)
	expired := h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":9000000}`)
	requireStatus(t, expired, http.StatusOK, "reuse after ttl")
	if expired.Header().Get(IdempotencyStatusHeader) != "" {
		t.Fatalf("expired key must not replay")
	}
	if got := h.runs.Load(); got != 2 {
		t.Fatalf("handler ran %d times after expiry, want 2", got)
	}
}

func TestIdempotency_abandonedInProgressClaimIsHandedOver(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-abandoned")

	// A process that died mid-request leaves its claim in_progress.
	h.forceInProgress(token, testIdempotencyKey, `{"amount":5000000}`)

	h.respond = func(w http.ResponseWriter, _ *http.Request, _ int64) { w.WriteHeader(http.StatusNoContent) }
	blocked := h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":5000000}`)
	requireStatus(t, blocked, http.StatusConflict, "retry while the claim is fresh")

	h.backdate(testIdempotencyKey, idempotencyAbandonedAfter+time.Minute)
	handedOver := h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":5000000}`)
	requireStatus(t, handedOver, http.StatusNoContent, "retry after the claim is abandoned")
}

func TestIdempotency_serverErrorIsNotStored(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-5xx")

	h.respond = func(w http.ResponseWriter, r *http.Request, run int64) {
		if run == 1 {
			writeJSONError(r.Context(), w, http.StatusInternalServerError, "internal server error")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
	requireStatus(t, h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":1}`), http.StatusInternalServerError, "first fund")
	retry := h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":1}`)
	requireStatus(t, retry, http.StatusOK, "retry after 5xx")
	if got := h.runs.Load(); got != 2 {
		t.Fatalf("handler ran %d times, want 2", got)
	}
}

func TestIdempotency_panicReleasesKey(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-panic")
	inner := h.handler
	h.handler = Recover()(inner)

	h.respond = func(w http.ResponseWriter, _ *http.Request, run int64) {
		if run == 1 {
			panic("boom")
		}
		w.WriteHeader(http.StatusNoContent)
	}
	requireStatus(t, h.post("/v1/groups/g1/leave", token, testIdempotencyKey, `{}`), http.StatusInternalServerError, "panicking leave")
	requireStatus(t, h.post("/v1/groups/g1/leave", token, testIdempotencyKey, `{}`), http.StatusNoContent, "retry after panic")
}

func TestIdempotency_absentKeyLeavesBehaviourUnchanged(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-absent")

	first := h.post("/v1/groups/g1/fund", token, "", `{"amount":5000000}`)
	second := h.post("/v1/groups/g1/fund", token, "", `{"amount":5000000}`)
	requireStatus(t, first, http.StatusOK, "first fund")
	requireStatus(t, second, http.StatusOK, "second fund")
	if got := h.runs.Load(); got != 2 {
		t.Fatalf("handler ran %d times, want 2", got)
	}
	if got := h.countKeys(); got != 0 {
		t.Fatalf("stored %d keys without a header, want 0", got)
	}
}

func TestIdempotency_rejectsMalformedKeys(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-badkey")

	for name, key := range map[string]string{
		"too short":    "abc",
		"too long":     strings.Repeat("a", idempotencyKeyMaxLength+1),
		"bad charset":  "key with spaces!",
		"sql-ish":      "abcdefgh'; DROP TABLE",
		"non ascii":    "clé-idempotence-é",
		"only padding": "        ",
	} {
		rec := h.post("/v1/groups/g1/fund", token, key, `{"amount":1}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}

	// A header that is present but empty is a malformed key, not an absent one.
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/g1/fund", strings.NewReader(`{"amount":1}`))
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header[IdempotencyKeyHeader] = []string{""}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	requireStatus(t, rec, http.StatusBadRequest, "empty key")

	if got := h.runs.Load(); got != 0 {
		t.Fatalf("handler ran %d times for malformed keys, want 0", got)
	}
}

func TestIdempotency_unauthenticatedRequestReachesHandlerAndStoresNothing(t *testing.T) {
	h := newIdempotencyHarness(t)
	h.respond = func(w http.ResponseWriter, r *http.Request, _ int64) {
		writeJSONError(r.Context(), w, http.StatusUnauthorized, "invalid or expired access token")
	}

	// An expired token: the app refreshes it and retries under the same key.
	requireStatus(t, h.post("/v1/groups/g1/fund", "expired-token", testIdempotencyKey, `{"amount":1}`), http.StatusUnauthorized, "expired token")
	requireStatus(t, h.post("/v1/groups/g1/fund", "", testIdempotencyKey, `{"amount":1}`), http.StatusUnauthorized, "no token")
	if got := h.runs.Load(); got != 2 {
		t.Fatalf("handler ran %d times, want 2", got)
	}

	token := h.signIn("idem-refreshed")
	h.respond = func(w http.ResponseWriter, _ *http.Request, _ int64) { w.WriteHeader(http.StatusOK) }
	requireStatus(t, h.post("/v1/groups/g1/fund", token, testIdempotencyKey, `{"amount":1}`), http.StatusOK, "retry with fresh token")
}

func TestIdempotency_onlyCoversUserMoneyPosts(t *testing.T) {
	covered := []string{
		"/v1/groups/g1/fund",
		"/v1/groups/g1/withdraw-to-balance",
		"/v1/me/withdrawals",
		"/v1/transactions/t1/retry",
		"/v1/groups/g1/proposals",
		"/v1/groups/g1/leave/",
	}
	for _, path := range covered {
		if !isIdempotentPath(path) {
			t.Errorf("%s must be covered", path)
		}
	}
	for _, path := range []string{"/v1/groups/g1/agents/intents", "/v1/groups/g1/messages", "/v1/proposals/p1/votes", "/v1/auth/session"} {
		if isIdempotentPath(path) {
			t.Errorf("%s must not be covered", path)
		}
	}

	h := newIdempotencyHarness(t)
	token := h.signIn("idem-get")
	req := httptest.NewRequest(http.MethodGet, "/v1/me/withdrawals", nil)
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set(IdempotencyKeyHeader, testIdempotencyKey)
	h.handler.ServeHTTP(httptest.NewRecorder(), req)
	if got := h.countKeys(); got != 0 {
		t.Fatalf("GET stored %d keys, want 0", got)
	}
}

// A replay never reaches the mux, so it must still carry a request id and be counted under
// its route pattern when the layer sits inside Metrics, as cmd/api wires it.
func TestIdempotency_replayKeepsRequestIDAndIsMetered(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-metrics")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/idemmetrics/{id}/fund", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	idempotency := NewIdempotency(postgres.NewStore(h.db), h.privy).WithRoutePattern(func(r *http.Request) string {
		_, pattern := mux.Handler(r)
		return pattern
	})
	h.handler = Chain(mux, RequestID(), Recover(), Metrics(), idempotency.Middleware())

	first := h.post("/v1/idemmetrics/cabal-1/fund", token, testIdempotencyKey, `{"amount":1}`)
	replay := h.post("/v1/idemmetrics/cabal-1/fund", token, testIdempotencyKey, `{"amount":1}`)
	requireStatus(t, first, http.StatusCreated, "first fund")
	requireStatus(t, replay, http.StatusCreated, "replayed fund")
	if replay.Header().Get(IdempotencyStatusHeader) != idempotencyStatusReplayed {
		t.Fatalf("second response was not a replay")
	}
	if replay.Header().Get(RequestIDHeader) == "" || replay.Header().Get(RequestIDHeader) == first.Header().Get(RequestIDHeader) {
		t.Fatalf("replay request id = %q, want its own (first was %q)", replay.Header().Get(RequestIDHeader), first.Header().Get(RequestIDHeader))
	}

	mismatch := h.post("/v1/idemmetrics/cabal-1/fund", token, testIdempotencyKey, `{"amount":2}`)
	requireStatus(t, mismatch, http.StatusUnprocessableEntity, "mismatched body")

	body := scrapeMetrics(t)
	for _, want := range []string{
		`monaco_http_requests_total{method="POST",route="POST /v1/idemmetrics/{id}/fund",status="201"} 2`,
		`monaco_http_requests_total{method="POST",route="POST /v1/idemmetrics/{id}/fund",status="422"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in:\n%s", want, grepLines(body, "idemmetrics"))
		}
	}
}

func TestIdempotency_storeFailureFailsClosed(t *testing.T) {
	h := newIdempotencyHarness(t)
	token := h.signIn("idem-dbdown")
	failing := &failingIdempotencyStore{IdempotencyStore: postgres.NewStore(h.db)}
	var runs atomic.Int64
	handler := NewIdempotency(failing, h.privy).Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		runs.Add(1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/me/withdrawals", strings.NewReader(`{"amount":1}`))
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set(IdempotencyKeyHeader, testIdempotencyKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	requireStatus(t, rec, http.StatusInternalServerError, "claim failure")
	if runs.Load() != 0 {
		t.Fatalf("money handler ran without a claim")
	}
}

// TestIdempotency_fundRetryCreatesOneDeposit is the reported failure end to end: the fund
// response is lost, the member taps again, and the real handler must not open a second intent.
func TestIdempotency_fundRetryCreatesOneDeposit(t *testing.T) {
	a := newMultiUserApp(t)
	member := a.signIn(t, "idem-fund", "Idem Fund")
	c := a.createClub(t, member, "Idem Club")
	a.topUpBalance(t, member, 20_000_000)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/groups/{id}/fund", a.Deposits.FundGroupHandler)
	handler := NewIdempotency(a.Store, a.Privy).Middleware()(mux)
	fund := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+c.ID+"/fund", strings.NewReader(`{"amount":5000000}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+string(member.Token))
		if key != "" {
			req.Header.Set(IdempotencyKeyHeader, key)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	first := fund(testIdempotencyKey)
	retry := fund(testIdempotencyKey)
	requireStatus(t, first, http.StatusOK, "fund")
	requireStatus(t, retry, http.StatusOK, "fund retry")
	firstIntent := decodeBody[createDepositResponse](t, first)
	retryIntent := decodeBody[createDepositResponse](t, retry)
	if retryIntent.DepositID != firstIntent.DepositID {
		t.Fatalf("retry opened deposit %s, want the original %s", retryIntent.DepositID, firstIntent.DepositID)
	}

	balance, err := a.DepositService.GetPlatformBalance(context.Background(), string(member.Token))
	if err != nil {
		t.Fatalf("GetPlatformBalance: %v", err)
	}
	if balance.PendingAllocationMicros != 5_000_000 {
		t.Fatalf("pending allocation = %d, want 5000000 (one intent)", balance.PendingAllocationMicros)
	}

	// A new submission carries a new key and is a new intent.
	second := fund("7b8c1f7e-0d0b-4f0e-8a55-0c7d0f3a2b19")
	requireStatus(t, second, http.StatusOK, "second submission")
	if decodeBody[createDepositResponse](t, second).DepositID == firstIntent.DepositID {
		t.Fatalf("a new key must open a new deposit")
	}
}

func (h *idempotencyHarness) backdate(key string, age time.Duration) {
	h.t.Helper()
	createdAt := time.Now().UTC().Add(-age)
	if _, err := h.db.Exec(`UPDATE idempotency_keys SET created_at = $1 WHERE key = $2 AND user_id = ANY($3::uuid[])`, createdAt, key, h.userIDs()); err != nil {
		h.t.Fatalf("backdate idempotency key: %v", err)
	}
}

// forceInProgress plants the claim a crashed process would leave behind.
func (h *idempotencyHarness) forceInProgress(token privy.AccessToken, key, body string) {
	h.t.Helper()
	identity, err := h.privy.VerifySession(context.Background(), token)
	if err != nil {
		h.t.Fatalf("VerifySession: %v", err)
	}
	store := postgres.NewStore(h.db)
	user, found, err := store.GetUserByPrivyUserID(context.Background(), identity.PrivyUserID)
	if err != nil || !found {
		h.t.Fatalf("GetUserByPrivyUserID: found=%v err=%v", found, err)
	}
	now := time.Now().UTC()
	_, claimed, err := store.ClaimIdempotencyKey(context.Background(), postgres.ClaimIdempotencyKeyParams{
		UserID:          user.ID,
		Key:             key,
		Route:           "POST /v1/groups/g1/fund",
		RequestHash:     hashIdempotentRequest([]byte(body)),
		Now:             now,
		ExpiredBefore:   now.Add(-idempotencyKeyTTL),
		AbandonedBefore: now.Add(-idempotencyAbandonedAfter),
	})
	if err != nil || !claimed {
		h.t.Fatalf("plant in_progress claim: claimed=%v err=%v", claimed, err)
	}
}

func (h *idempotencyHarness) userIDs() []string {
	h.t.Helper()
	rows, err := h.db.Query(`SELECT id::text FROM users WHERE privy_user_id LIKE '%' || $1 || '%'`, h.iso.Suffix())
	if err != nil {
		h.t.Fatalf("list users: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			h.t.Fatalf("scan user id: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func (h *idempotencyHarness) countKeys() int {
	h.t.Helper()
	var count int
	if err := h.db.QueryRow(`SELECT count(*) FROM idempotency_keys WHERE user_id = ANY($1::uuid[])`, h.userIDs()).Scan(&count); err != nil {
		h.t.Fatalf("count idempotency keys: %v", err)
	}
	return count
}

type failingIdempotencyStore struct {
	IdempotencyStore
}

func (f *failingIdempotencyStore) ClaimIdempotencyKey(context.Context, postgres.ClaimIdempotencyKeyParams) (postgres.IdempotencyKeyRow, bool, error) {
	return postgres.IdempotencyKeyRow{}, false, errors.New("connection refused")
}
