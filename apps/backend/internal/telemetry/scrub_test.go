package telemetry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

// testSecretKeyBase58 has the shape of a 64-byte Solana secret key (and of a signature).
const testSecretKeyBase58 = "5Kd3NBUAdUnhyzenEwVLy9pBKxSwXvE9FMPyR4UKZvpe6E3ZcrYyFZ8sPq2mW7hTgJ4nXb1LcD9uRfAa3Vt6QeHk"

const testJWT = "eyJhbGciOiJFUzI1NiJ9.eyJzdWIiOiJkaWQ6cHJpdnk6YWxmcmVkIn0.c2lnbmF0dXJlLWJ5dGVz"

func TestScrubString_redactsCredentialsInFreeText(t *testing.T) {
	relayerKey := testSecretKeyBase58
	cases := map[string]struct {
		input  string
		secret string
	}{
		"bearer header":         {"upstream rejected Authorization: Bearer abc.def-123", "abc.def-123"},
		"privy access token":    {"verify failed for token " + testJWT, testJWT},
		"agent key header":      {"request headers: X-Monaco-Agent-Key: mk7Hq2pLx9 Accept: */*", "mk7Hq2pLx9"},
		"privy authorization":   {"sign failed with wallet-auth:MIGHAgEAMBMGByqGSM49", "MIGHAgEAMBMGByqGSM49"},
		"relayer secret key":    {"invalid fee payer " + relayerKey, relayerKey},
		"rpc url api key":       {`Post "https://mainnet.helius-rpc.com/?api-key=9f2c1d7e": timeout`, "9f2c1d7e"},
		"database url password": {"ping postgres://monaco:s3cretpw@db.internal:5432/monaco failed", "s3cretpw"},
		"json secret field":     {`privy said {"app_secret":"privy-app-secret-value","code":401}`, "privy-app-secret-value"},
		"keygen byte array":     {"key " + byteArrayKey(), "11,12,13"},
		"pem private key":       {"-----BEGIN PRIVATE KEY-----\nMIGHAgEAMBMG\n-----END PRIVATE KEY-----", "MIGHAgEAMBMG"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			got := ScrubString(tc.input)

			// Assert
			if strings.Contains(got, tc.secret) {
				t.Fatalf("secret survived: %q", got)
			}
			if !strings.Contains(got, Redacted) {
				t.Fatalf("no redaction marker in %q", got)
			}
		})
	}
}

func byteArrayKey() string {
	parts := make([]string, 64)
	for i := range parts {
		parts[i] = fmt.Sprint(i + 10)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestScrubString_keepsOperationalDetail(t *testing.T) {
	// Arrange
	input := "sweep deposit 7b1e4a52-9d0c-4c55-8a53-1f2f6f1f0a11 to treasury 9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin amount 2500000 failed: blockhash not found"

	// Act
	got := ScrubString(input)

	// Assert
	if got != input {
		t.Fatalf("scrubber damaged a line with no credentials:\n got %q\nwant %q", got, input)
	}
}

func TestScrubValue_sensitiveKeys_areRedactedWhateverTheValue(t *testing.T) {
	for _, key := range []string{
		"Authorization", "X-Monaco-Agent-Key", "agent_key", "Cookie", "PRIVY_APP_SECRET",
		"RELAYER_PRIVATE_KEY", "relayerPrivateKey", "access_token", "token", "api_key", "SENTRY_DSN", "password",
	} {
		if got := ScrubValue(key, "short"); got != Redacted {
			t.Errorf("ScrubValue(%q) = %v, want redacted", key, got)
		}
	}
}

func TestScrubValue_tradeFieldsNamedToken_areKept(t *testing.T) {
	for _, key := range []string{"token_amount", "token_mint", "output_mint", "usdc_amount"} {
		if got := ScrubValue(key, "1500000"); got != "1500000" {
			t.Errorf("ScrubValue(%q) = %v, want the value kept", key, got)
		}
	}
}

func TestScrubValue_transactionSignature_isKeptOnlyUnderSignatureKeys(t *testing.T) {
	// Arrange: a signature is 64 bytes of base58, indistinguishable from a secret key.
	signature := testSecretKeyBase58

	// Act
	underSignatureKey := ScrubValue("tx_signature", signature)
	underOtherKey := ScrubValue("detail", signature)

	// Assert
	if underSignatureKey != signature {
		t.Fatalf("tx_signature = %v, want it kept for tracing the payment", underSignatureKey)
	}
	if underOtherKey != Redacted {
		t.Fatalf("detail = %v, want redacted", underOtherKey)
	}
}

func TestScrubSentryEvent_redactsWhatTheSDKWouldSend(t *testing.T) {
	// Arrange
	event := sentry.NewEvent()
	event.Message = "Poller deposit_sweep panicked: Bearer msg-token"
	event.Exception = []sentry.Exception{{Type: "panic", Value: `Post "https://rpc.example.com/?api-key=rpc-key-value": EOF`}}
	event.Tags = map[string]string{"route": "POST /v1/groups/{id}/fund", "agent_key": "tag-secret"}
	event.Contexts = map[string]sentry.Context{
		"panic": {"stack": "main.pay(wallet-auth:STACKSECRET)"},
		"alert": {"detail": "dial postgres://monaco:dbpass@host/db", "job_id": "job-1"},
	}
	event.Request = &sentry.Request{Headers: map[string]string{"Authorization": "Bearer header-token"}}
	event.User = sentry.User{IPAddress: "203.0.113.9"}

	// Act
	got := scrubSentryEvent(event, nil)
	flat := fmt.Sprintf("%v|%v|%v|%v|%v|%v", got.Message, got.Exception, got.Tags, got.Contexts, got.Request, got.User)

	// Assert
	for _, secret := range []string{"msg-token", "rpc-key-value", "tag-secret", "STACKSECRET", "dbpass", "header-token", "203.0.113.9"} {
		if strings.Contains(flat, secret) {
			t.Errorf("secret %q survived: %s", secret, flat)
		}
	}
	if got.Tags["route"] != "POST /v1/groups/{id}/fund" || got.Contexts["alert"]["job_id"] != "job-1" {
		t.Fatalf("non-secret fields were damaged: tags=%v contexts=%v", got.Tags, got.Contexts)
	}
}

func TestAlert_upstreamErrorInDetail_isScrubbedBeforeTheWebhook(t *testing.T) {
	// Arrange
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
	}))
	defer server.Close()
	a := newAlerter(server.URL, time.Minute, server.Client())
	a.start()
	defer a.stop()

	// Act
	a.raise(context.Background(), AlertEvent{
		Kind:   "test_poller_panic",
		Title:  "Poller panicked",
		Detail: `Post "https://rpc.example.com/?api-key=rpc-key-value": Authorization: Bearer live-token`,
		Fields: map[string]string{"job_id": "job-1", "access_token": "field-token"},
	})

	// Assert
	select {
	case body := <-received:
		for _, secret := range []string{"rpc-key-value", "live-token", "field-token"} {
			if strings.Contains(body, secret) {
				t.Errorf("secret %q reached the webhook: %s", secret, body)
			}
		}
		if !strings.Contains(body, "job_id: job-1") || !strings.Contains(body, "rpc.example.com") {
			t.Errorf("webhook body lost its operational detail: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook was never called")
	}
}
