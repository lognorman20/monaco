package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecover_panicBeforeWrite_returnsJSON500WithRequestID(t *testing.T) {
	// Arrange
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}), RequestID(), Recover())
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, testHTTPRequest("POST", "/v1/groups"))

	// Assert
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if body.Error != "internal server error" {
		t.Fatalf("error = %q, want a generic message (no panic value)", body.Error)
	}
	if body.RequestID == "" || body.RequestID != rec.Header().Get(RequestIDHeader) {
		t.Fatalf("requestId = %q, header = %q, want equal and set", body.RequestID, rec.Header().Get(RequestIDHeader))
	}
}

func TestRecover_panicAfterWrite_doesNotAppendSecondResponse(t *testing.T) {
	// Arrange
	handler := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":`))
		panic("boom")
	}), Recover())
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, testHTTPRequest("POST", "/v1/groups"))

	// Assert
	if rec.Code != http.StatusCreated || rec.Body.String() != `{"ok":` {
		t.Fatalf("code = %d body = %q, want the partial 201 untouched", rec.Code, rec.Body.String())
	}
}

func TestRecover_abortHandler_isRepanicked(t *testing.T) {
	// Arrange
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}), Recover())
	defer func() {
		// Assert
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", rec)
		}
	}()

	// Act
	handler.ServeHTTP(httptest.NewRecorder(), testHTTPRequest("GET", "/v1/me"))
}

func TestRequestID_rejectsUnsafeInboundID(t *testing.T) {
	for name, inbound := range map[string]string{
		"log injection": "abc\nlevel=ERROR forged",
		"too long":      strings.Repeat("a", maxInboundRequestIDLen+1),
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			var seen string
			handler := Chain(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = RequestIDFromContext(r.Context())
			}), RequestID())
			req := testHTTPRequest("GET", "/v1/me")
			req.Header[RequestIDHeader] = []string{inbound}

			// Act
			handler.ServeHTTP(httptest.NewRecorder(), req)

			// Assert
			if seen == "" || seen == inbound {
				t.Fatalf("request id = %q, want a freshly minted one", seen)
			}
		})
	}
}

func TestRequestID_keepsSafeInboundID(t *testing.T) {
	// Arrange
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), RequestID())
	req := testHTTPRequest("GET", "/v1/me")
	req.Header.Set(RequestIDHeader, "ios-7f3a_01")
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if got := rec.Header().Get(RequestIDHeader); got != "ios-7f3a_01" {
		t.Fatalf("echoed id = %q, want ios-7f3a_01", got)
	}
}

func TestLimitRequestBody_oversizedJSONBody_failsRead(t *testing.T) {
	// Arrange
	var readErr error
	handler := Chain(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}), LimitRequestBody(16))
	req := httptest.NewRequest("POST", "/v1/groups", strings.NewReader(strings.Repeat("x", 17)))

	// Act
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Assert
	var tooLarge *http.MaxBytesError
	if !errors.As(readErr, &tooLarge) {
		t.Fatalf("read err = %v, want *http.MaxBytesError", readErr)
	}
}

func TestLimitRequestBody_profilePhotoUpload_getsUploadBudget(t *testing.T) {
	// Arrange
	var read int
	handler := Chain(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		read = len(body)
	}), LimitRequestBody(16))
	size := 2<<20 + (1 << 10)
	req := httptest.NewRequest("POST", "/v1/me/profile-photo", strings.NewReader(strings.Repeat("x", size)))

	// Act
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Assert
	if read != size {
		t.Fatalf("read %d bytes, want the full %d byte upload", read, size)
	}
}

func TestCORS_unlistedOrigin_getsNoAllowHeadersAndPreflightIsRefused(t *testing.T) {
	// Arrange
	reached := false
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }),
		CORS([]string{"https://app.monaco.example", "*"}))
	req := testHTTPRequest("OPTIONS", "/v1/groups")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusForbidden || reached {
		t.Fatalf("code = %d reached = %v, want 403 without reaching the handler", rec.Code, reached)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestCORS_listedOrigin_isEchoedNeverWildcard(t *testing.T) {
	// Arrange
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		CORS([]string{" https://App.Monaco.example/ "}))
	req := testHTTPRequest("OPTIONS", "/v1/groups")
	req.Header.Set("Origin", "https://app.monaco.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.monaco.example" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestCORS_noOriginHeader_passesThroughUntouched(t *testing.T) {
	// Arrange: the iOS app sends no Origin header.
	reached := false
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }), CORS(nil))
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, testHTTPRequest("POST", "/v1/groups"))

	// Assert
	if !reached || rec.Header().Get("Vary") != "" {
		t.Fatalf("reached = %v vary = %q, want untouched pass-through", reached, rec.Header().Get("Vary"))
	}
}

func TestClassifyRateLimit(t *testing.T) {
	cases := []struct {
		method, path string
		class        string
		limited      bool
	}{
		{"GET", "/v1/home", "", false},
		{"POST", "/v1/auth/session", rateClassAuth, true},
		{"POST", "/v1/groups/g1/withdraw-to-balance", rateClassMoney, true},
		{"POST", "/v1/groups/g1/deposits", rateClassMoney, true},
		{"POST", "/v1/me/withdrawals", rateClassMoney, true},
		{"POST", "/v1/transactions/t1/retry", rateClassMoney, true},
		{"POST", "/v1/groups/g1/messages", rateClassWrite, true},
		{"PATCH", "/v1/me", rateClassWrite, true},
	}
	for _, tc := range cases {
		class, limited := classifyRateLimit(tc.method, tc.path)
		if class != tc.class || limited != tc.limited {
			t.Errorf("%s %s = (%q, %v), want (%q, %v)", tc.method, tc.path, class, limited, tc.class, tc.limited)
		}
	}
}

func TestRateLimiter_moneyRoute_overBudgetGets429ThenRecovers(t *testing.T) {
	// Arrange
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter(false).WithClock(func() time.Time { return now })
	served := 0
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served++ }), limiter.Middleware())
	send := func(token string) *httptest.ResponseRecorder {
		req := testHTTPRequest("POST", "/v1/groups/g1/withdraw-to-balance")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	burst := rateLimitRules[rateClassMoney].userBurst

	// Act
	for i := 0; i < burst; i++ {
		if rec := send("alice"); rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200 inside the burst", i, rec.Code)
		}
	}
	blocked := send("alice")
	other := send("bob")
	now = now.Add(rateLimitRules[rateClassMoney].userInterval)
	recovered := send("alice")

	// Assert
	if blocked.Code != http.StatusTooManyRequests || blocked.Header().Get("Retry-After") == "" {
		t.Fatalf("over budget = %d retry-after = %q, want 429 with Retry-After", blocked.Code, blocked.Header().Get("Retry-After"))
	}
	if other.Code != http.StatusOK {
		t.Fatalf("another user = %d, want 200 (buckets are per credential)", other.Code)
	}
	if recovered.Code != http.StatusOK {
		t.Fatalf("after refill = %d, want 200", recovered.Code)
	}
	if served != burst+2 {
		t.Fatalf("handler ran %d times, want %d", served, burst+2)
	}
}

func TestRateLimiter_readsAreNeverLimited(t *testing.T) {
	// Arrange
	limiter := NewRateLimiter(false)
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), limiter.Middleware())

	// Act / Assert
	for i := 0; i < 500; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, testHTTPRequest("GET", "/v1/home/dashboard"))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %d = %d, want 200", i, rec.Code)
		}
	}
}

func TestClientIP_ignoresForwardedForUnlessProxyTrusted(t *testing.T) {
	// Arrange
	req := testHTTPRequest("POST", "/v1/auth/session")
	req.RemoteAddr = "10.0.0.7:4312"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")

	// Act / Assert
	if got := clientIP(req, false); got != "10.0.0.7" {
		t.Fatalf("untrusted clientIP = %q, want the peer 10.0.0.7", got)
	}
	if got := clientIP(req, true); got != "203.0.113.9" {
		t.Fatalf("trusted clientIP = %q, want 203.0.113.9", got)
	}
}
