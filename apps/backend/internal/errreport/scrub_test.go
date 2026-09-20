package errreport

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
)

const testJWT = "eyJhbGciOiJFUzI1NiJ9.eyJzdWIiOiJkaWQ6cHJpdnk6YWxmcmVkIn0.c2lnbmF0dXJlLWJ5dGVz"

func TestScrubString_redactsCredentialsInFreeText(t *testing.T) {
	relayerKey := solanakey.TestPrivateKeyBase58()
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
	signature := solanakey.TestPrivateKeyBase58()

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

func TestScrub_event_redactsEveryField(t *testing.T) {
	// Arrange
	event := Event{
		Level:   LevelFatal,
		Message: "privy call failed with Bearer msg-token",
		Err:     fmt.Errorf("wrap: %w", errors.New("token "+testJWT)),
		Panic:   "bad header Authorization: Bearer panic-token",
		Stack:   "goroutine 1 [running]:\nmain.pay(wallet-auth:STACKSECRET)",
		Tags:    map[string]string{"route": "POST /v1/groups/{id}/fund", "agent_key": "tag-secret"},
		Extra: map[string]any{
			"headers": map[string]any{"Authorization": "Bearer nested-token", "X-Monaco-Agent-Key": "nested-agent-key", "Accept": "*/*"},
			"nested":  []any{map[string]any{"private_key": "deep-secret"}},
			"err":     errors.New("dial postgres://monaco:dbpass@host/db"),
			"amount":  int64(2_500_000),
		},
	}

	// Act
	got := Scrub(event)
	flat := fmt.Sprintf("%v|%v|%v|%v|%v|%v", got.Message, got.Err, got.Panic, got.Stack, got.Tags, got.Extra)

	// Assert
	for _, secret := range []string{"msg-token", testJWT, "panic-token", "STACKSECRET", "tag-secret", "nested-token", "nested-agent-key", "deep-secret", "dbpass"} {
		if strings.Contains(flat, secret) {
			t.Errorf("secret %q survived: %s", secret, flat)
		}
	}
	if got.Tags["route"] != "POST /v1/groups/{id}/fund" || got.Extra["amount"] != int64(2_500_000) {
		t.Fatalf("non-secret fields were damaged: %+v", got)
	}
	headers := got.Extra["headers"].(map[string]any)
	if headers["Accept"] != "*/*" {
		t.Fatalf("headers = %v", headers)
	}
	if event.Extra["headers"].(map[string]any)["Authorization"] != "Bearer nested-token" {
		t.Fatal("Scrub mutated the caller's event")
	}
}
