package telemetry

import (
	"fmt"
	"regexp"
)

// Redacted replaces every credential the scrubber finds.
const Redacted = "[redacted]"

// sensitiveKey matches attribute, header and tag names whose value is a credential no
// matter what it looks like: Authorization, Cookie, X-Monaco-Agent-Key, *_SECRET,
// RELAYER_PRIVATE_KEY, access_token, SENTRY_DSN, ... A bare "token" suffix is matched but
// "token_amount" or "token_mint" are not: those are trade fields worth keeping.
var sensitiveKey = regexp.MustCompile(`(?i)(authorization|cookie|secret|passw(or)?d|private[_-]?key|api[_-]?key|agent[_-]?key|mnemonic|seed[_-]?phrase|bearer|dsn|(^|[_-])token$|(access|refresh|id|identity)[_-]?token)`)

// signatureKey names attributes that hold a Solana transaction signature. A signature is
// public and is what an operator needs to trace a payment, but it is the same 64 bytes of
// base58 as a secret key, so only values under these names keep their long base58 runs.
var signatureKey = regexp.MustCompile(`(?i)(^|[_-])(signature|sig)$`)

type scrubRule struct {
	pattern     *regexp.Regexp
	replacement string
}

var (
	// secretKeyBase58 is a 64-byte Solana secret key (relayer, wallet export) in base58.
	secretKeyBase58 = scrubRule{regexp.MustCompile(`\b[1-9A-HJ-NP-Za-km-z]{80,90}\b`), Redacted}

	valueRules = []scrubRule{
		{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), Redacted},
		{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`), "Bearer " + Redacted},
		// Privy access and identity tokens are JWTs.
		{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`), Redacted},
		// PRIVY_AUTHORIZATION_PRIVATE_KEY.
		{regexp.MustCompile(`wallet-auth:[A-Za-z0-9+/=_-]+`), Redacted},
		// DATABASE_URL style userinfo: scheme://user:password@host.
		{regexp.MustCompile(`(://[^\s:/@]+:)[^\s@/]+@`), "${1}" + Redacted + "@"},
		// RPC and API URLs carry their key in the query string (?api-key=...).
		{regexp.MustCompile(`(?i)([?&][^=&\s]*(?:key|token|secret|password|auth)[^=&\s]*=)[^&\s"']+`), "${1}" + Redacted},
		// "X-Monaco-Agent-Key: abc", "client_secret=abc", `"password":"abc"` inside free text.
		{regexp.MustCompile(`(?i)((?:authorization|cookie|x-monaco-agent-key|[a-z0-9_-]*(?:secret|password|private[_-]?key|api[_-]?key|agent[_-]?key|access[_-]?token|refresh[_-]?token))["']?\s*[:=]\s*["']?)[^\s"',;}]+`), "${1}" + Redacted},
		// A secret key as the JSON byte array solana-keygen writes.
		{regexp.MustCompile(`\[\s*(?:\d{1,3}\s*,\s*){31,}\d{1,3}\s*\]`), Redacted},
	}
)

// SensitiveKey reports whether a value stored under key must never leave the process.
func SensitiveKey(key string) bool {
	return sensitiveKey.MatchString(key)
}

// ScrubString redacts credentials embedded in free text: error messages, panic values,
// stack traces, URLs.
func ScrubString(s string) string {
	return scrubString(s, false)
}

func scrubString(s string, keepSignatures bool) string {
	if s == "" {
		return s
	}
	for _, rule := range valueRules {
		s = rule.pattern.ReplaceAllString(s, rule.replacement)
	}
	if !keepSignatures {
		s = secretKeyBase58.pattern.ReplaceAllString(s, secretKeyBase58.replacement)
	}
	return s
}

// ScrubValue returns value safe to send when stored under key.
func ScrubValue(key string, value any) any {
	if SensitiveKey(key) {
		return Redacted
	}
	keepSignatures := signatureKey.MatchString(key)
	switch v := value.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return v
	case string:
		return scrubString(v, keepSignatures)
	case map[string]any:
		return ScrubMap(v)
	case map[string]string:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = ScrubValue(k, item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = ScrubValue(key, item)
		}
		return out
	case []string:
		out := make([]string, len(v))
		for i, item := range v {
			out[i] = scrubString(item, keepSignatures)
		}
		return out
	default:
		// Errors, Stringers and structs are flattened to text: a struct field holding a
		// credential cannot survive a pattern scrub as a typed value.
		return scrubString(fmt.Sprint(v), keepSignatures)
	}
}

// ScrubMap returns a scrubbed copy of m.
func ScrubMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = ScrubValue(k, v)
	}
	return out
}

// ScrubTags returns a scrubbed copy of tags.
func ScrubTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	out := make(map[string]string, len(tags))
	for k, v := range tags {
		if SensitiveKey(k) {
			out[k] = Redacted
			continue
		}
		out[k] = scrubString(v, signatureKey.MatchString(k))
	}
	return out
}
