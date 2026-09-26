package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func generateP8(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), key
}

func testClient(t *testing.T, pemText string, env Env) *Client {
	t.Helper()
	client, err := NewClient(Config{KeyID: "KEY1234567", TeamID: "TEAM123456", PrivateKeyPEM: pemText, BundleID: "com.monaco.app", Env: env})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestProviderToken_isES256SignedWithKidAndTeam(t *testing.T) {
	// Arrange
	pemText, key := generateP8(t)
	now := time.Unix(1_800_000_000, 0)
	client := testClient(t, pemText, EnvSandbox).WithClock(func() time.Time { return now })

	// Act
	signed, err := client.ProviderToken()

	// Assert
	if err != nil {
		t.Fatalf("ProviderToken: %v", err)
	}
	parsed, err := jwt.Parse(signed, func(tok *jwt.Token) (any, error) {
		if tok.Method != jwt.SigningMethodES256 {
			t.Fatalf("alg = %v, want ES256", tok.Method.Alg())
		}
		return &key.PublicKey, nil
	}, jwt.WithoutClaimsValidation())
	if err != nil || !parsed.Valid {
		t.Fatalf("token does not verify against the key: %v", err)
	}
	if kid := parsed.Header["kid"]; kid != "KEY1234567" {
		t.Fatalf("kid = %v", kid)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["iss"] != "TEAM123456" {
		t.Fatalf("iss = %v", claims["iss"])
	}
	if iat, _ := claims["iat"].(float64); int64(iat) != now.Unix() {
		t.Fatalf("iat = %v, want %d", claims["iat"], now.Unix())
	}
	// ES256 in JWS is r||s, 64 bytes, not DER.
	parts := strings.Split(signed, ".")
	sig, err := jwt.NewParser().DecodeSegment(parts[2])
	if err != nil || len(sig) != 64 {
		t.Fatalf("signature is %d bytes (err %v), want 64", len(sig), err)
	}
}

func TestProviderToken_isReusedFor50MinutesThenReminted(t *testing.T) {
	cases := []struct {
		name    string
		elapsed time.Duration
		reused  bool
	}{
		{name: "one minute later", elapsed: time.Minute, reused: true},
		{name: "49 minutes later", elapsed: 49 * time.Minute, reused: true},
		{name: "50 minutes later", elapsed: 50 * time.Minute, reused: false},
		{name: "two hours later", elapsed: 2 * time.Hour, reused: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			pemText, _ := generateP8(t)
			now := time.Unix(1_800_000_000, 0)
			client := testClient(t, pemText, EnvSandbox).WithClock(func() time.Time { return now })
			first, err := client.ProviderToken()
			if err != nil {
				t.Fatalf("first token: %v", err)
			}

			// Act
			now = now.Add(tc.elapsed)
			second, err := client.ProviderToken()

			// Assert
			if err != nil {
				t.Fatalf("second token: %v", err)
			}
			if (first == second) != tc.reused {
				t.Fatalf("reused = %v, want %v", first == second, tc.reused)
			}
		})
	}
}

func TestParsePrivateKey(t *testing.T) {
	pemText, _ := generateP8(t)
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "pem with newlines", raw: pemText},
		{name: "pem pasted with literal backslash n", raw: strings.ReplaceAll(pemText, "\n", `\n`)},
		{name: "empty", raw: "  ", wantErr: true},
		{name: "not pem", raw: "hello", wantErr: true},
		{name: "garbage block", raw: "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePrivateKey(tc.raw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestParseEnv(t *testing.T) {
	cases := map[string]struct {
		want    Env
		wantErr bool
	}{
		"":            {want: EnvSandbox},
		"sandbox":     {want: EnvSandbox},
		"PRODUCTION":  {want: EnvProduction},
		"development": {want: EnvSandbox},
		"prod":        {wantErr: true},
	}
	for raw, tc := range cases {
		got, err := ParseEnv(raw)
		if (err != nil) != tc.wantErr || (err == nil && got != tc.want) {
			t.Fatalf("ParseEnv(%q) = %q, %v", raw, got, err)
		}
	}
}

func TestPayload_marshalsApsAndRoutingKeys(t *testing.T) {
	// Arrange
	payload := Payload{
		Alert:          Alert{Title: "Jordan proposed $250 of Google in Sunday Investors", Body: "Voting closes in 1 day."},
		Badge:          3,
		Sound:          "default",
		ThreadID:       "group-1",
		GroupID:        "group-1",
		ProposalID:     "proposal-1",
		NotificationID: "n-1",
		Kind:           "proposal_created",
	}

	// Act
	raw, err := json.Marshal(payload)

	// Assert
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	aps := decoded["aps"].(map[string]any)
	alert := aps["alert"].(map[string]any)
	if alert["title"] != payload.Alert.Title || alert["body"] != payload.Alert.Body {
		t.Fatalf("alert = %v", alert)
	}
	if aps["badge"].(float64) != 3 || aps["sound"] != "default" || aps["thread-id"] != "group-1" {
		t.Fatalf("aps = %v", aps)
	}
	if decoded["groupId"] != "group-1" || decoded["proposalId"] != "proposal-1" || decoded["notificationId"] != "n-1" || decoded["kind"] != "proposal_created" {
		t.Fatalf("routing keys = %v", decoded)
	}
}

func TestPayload_omitsEmptyRoutingKeysButAlwaysSendsBadge(t *testing.T) {
	raw, _ := json.Marshal(Payload{Alert: Alert{Title: "$500 arrived in your balance"}})
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	if _, ok := decoded["groupId"]; ok {
		t.Fatalf("groupId should be omitted: %s", raw)
	}
	if _, ok := decoded["proposalId"]; ok {
		t.Fatalf("proposalId should be omitted: %s", raw)
	}
	aps := decoded["aps"].(map[string]any)
	if aps["badge"].(float64) != 0 {
		t.Fatalf("badge = %v, want 0 so a read inbox clears the icon", aps["badge"])
	}
}

type recordedRequest struct {
	path    string
	headers http.Header
	body    []byte
}

func fakeAPNs(t *testing.T, respond func(n int, w http.ResponseWriter)) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, recordedRequest{path: r.URL.Path, headers: r.Header.Clone(), body: body})
		n := len(seen)
		mu.Unlock()
		respond(n, w)
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func TestSend_postsToTheDeviceWithProviderHeaders(t *testing.T) {
	// Arrange
	pemText, _ := generateP8(t)
	server, seen := fakeAPNs(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusOK) })
	client := testClient(t, pemText, EnvSandbox).WithEndpoints(server.Client(), server.URL+"/prod", server.URL+"/sandbox")

	// Act
	err := client.Send(context.Background(), Device{Token: "abc123", Env: EnvProduction}, Payload{Alert: Alert{Title: "Hi"}, CollapseID: "c1"})

	// Assert
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := (*seen)[0]
	if got.path != "/prod/3/device/abc123" {
		t.Fatalf("path = %s, want the production host for a production device", got.path)
	}
	if !strings.HasPrefix(got.headers.Get("authorization"), "bearer ") {
		t.Fatalf("authorization = %q", got.headers.Get("authorization"))
	}
	for header, want := range map[string]string{
		"apns-topic": "com.monaco.app", "apns-push-type": "alert", "apns-priority": "10", "apns-collapse-id": "c1",
	} {
		if got.headers.Get(header) != want {
			t.Fatalf("%s = %q, want %q", header, got.headers.Get(header), want)
		}
	}
}

func TestSend_deviceWithoutEnvUsesTheConfiguredHost(t *testing.T) {
	pemText, _ := generateP8(t)
	server, seen := fakeAPNs(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusOK) })
	client := testClient(t, pemText, EnvSandbox).WithEndpoints(server.Client(), server.URL+"/prod", server.URL+"/sandbox")

	if err := client.Send(context.Background(), Device{Token: "abc"}, Payload{Alert: Alert{Title: "Hi"}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if (*seen)[0].path != "/sandbox/3/device/abc" {
		t.Fatalf("path = %s, want the APNS_ENV host", (*seen)[0].path)
	}
}

func TestSend_mapsApplesAnswers(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		reason     string
		wantUnreg  bool
		wantStatus int
	}{
		{name: "410 unregistered", status: http.StatusGone, reason: "Unregistered", wantUnreg: true},
		{name: "400 bad device token is not unregistered", status: http.StatusBadRequest, reason: "BadDeviceToken", wantStatus: 400},
		{name: "429 too many requests", status: http.StatusTooManyRequests, reason: "TooManyRequests", wantStatus: 429},
		{name: "500 internal", status: http.StatusInternalServerError, reason: "InternalServerError", wantStatus: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			pemText, _ := generateP8(t)
			server, _ := fakeAPNs(t, func(_ int, w http.ResponseWriter) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"reason":"` + tc.reason + `"}`))
			})
			client := testClient(t, pemText, EnvSandbox).WithEndpoints(server.Client(), server.URL, server.URL)

			// Act
			err := client.Send(context.Background(), Device{Token: "abc"}, Payload{Alert: Alert{Title: "Hi"}})

			// Assert
			if errors.Is(err, ErrUnregistered) != tc.wantUnreg {
				t.Fatalf("unregistered = %v, want %v (err %v)", errors.Is(err, ErrUnregistered), tc.wantUnreg, err)
			}
			if !tc.wantUnreg {
				var apnsErr *Error
				if !errors.As(err, &apnsErr) || apnsErr.Status != tc.wantStatus || apnsErr.Reason != tc.reason {
					t.Fatalf("err = %v, want *Error %d %s", err, tc.wantStatus, tc.reason)
				}
			}
		})
	}
}

func TestSend_expiredProviderTokenIsRemintedAndRetriedOnce(t *testing.T) {
	// Arrange
	pemText, _ := generateP8(t)
	server, seen := fakeAPNs(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"reason":"ExpiredProviderToken"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	clock := time.Unix(1_800_000_000, 0)
	client := testClient(t, pemText, EnvSandbox).
		WithEndpoints(server.Client(), server.URL, server.URL).
		WithClock(func() time.Time { clock = clock.Add(time.Second); return clock })

	// Act
	err := client.Send(context.Background(), Device{Token: "abc"}, Payload{Alert: Alert{Title: "Hi"}})

	// Assert
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(*seen) != 2 {
		t.Fatalf("requests = %d, want the first plus one retry", len(*seen))
	}
	if (*seen)[0].headers.Get("authorization") == (*seen)[1].headers.Get("authorization") {
		t.Fatal("the retry reused the rejected provider token")
	}
}

func TestNewClient_requiresIdentity(t *testing.T) {
	pemText, _ := generateP8(t)
	if _, err := NewClient(Config{TeamID: "T", BundleID: "b", PrivateKeyPEM: pemText}); err == nil {
		t.Fatal("missing key id accepted")
	}
	if _, err := NewClient(Config{KeyID: "K", TeamID: "T", BundleID: "b", PrivateKeyPEM: "nope"}); err == nil {
		t.Fatal("bad key accepted")
	}
}
