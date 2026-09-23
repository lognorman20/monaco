package b20

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Logos are the issuer's own. Every B20 token publishes ERC-7572 contract-level
// metadata: contractURI() returns a JSON document whose "image" is the company's
// icon, which Coinbase (the issuer) hosts on metadata.coinbase.com. ERC-7572 exists
// so that apps can show a contract's name and image without keeping their own
// copy, which is exactly this use. Nothing here scrapes a website or bundles a
// third party's artwork.
//
// The image URL is only ever handed to the app when it is https on the issuer's
// metadata host: contractURI is on-chain data, and the app loads whatever URL we
// send it, so a changed or hostile URI must not become an arbitrary fetch on a
// member's phone.

// contractURISelector is keccak256("contractURI()")[:4].
var contractURISelector = []byte{0xe8, 0xa3, 0xd4, 0x85}

// issuerLogoHosts are the hosts a logo may be served from.
var issuerLogoHosts = map[string]bool{"metadata.coinbase.com": true}

const (
	// logoTTL is how long a resolved logo is reused. The metadata changes when the
	// issuer rebrands a listing, which is rare; a day is fresh enough.
	logoTTL = 24 * time.Hour
	// logoFailureTTL is how long a failed read is remembered, so an RPC outage
	// costs one call per token per few minutes rather than one per page.
	logoFailureTTL = 5 * time.Minute
	// maxContractURIBytes bounds the metadata document we will decode.
	maxContractURIBytes = 16 * 1024
)

// ContractCaller is the one chain read the logo source needs: an eth_call.
// evm.Client satisfies it.
type ContractCaller interface {
	Call(ctx context.Context, to string, data []byte) ([]byte, error)
}

// LogoSource resolves a token's logo from its on-chain metadata.
type LogoSource interface {
	// LogoURL is the issuer's image for the token, or "" when it cannot be
	// resolved (no metadata, an unreadable document, a host we do not trust, or
	// the chain read failed). It never blocks past ctx.
	LogoURL(ctx context.Context, tokenAddress string) string
}

type logoEntry struct {
	url       string
	expiresAt time.Time
}

type contractLogos struct {
	chain ContractCaller
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]logoEntry
}

// NewContractLogos reads logos through chain and caches them per token. A nil
// chain returns nil, which callers treat as "no logos".
func NewContractLogos(chain ContractCaller, now func() time.Time) LogoSource {
	if chain == nil {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	return &contractLogos{chain: chain, now: now, entries: make(map[string]logoEntry)}
}

func (c *contractLogos) LogoURL(ctx context.Context, tokenAddress string) string {
	key := strings.ToLower(strings.TrimSpace(tokenAddress))
	if key == "" {
		return ""
	}
	now := c.now()
	c.mu.Lock()
	if entry, ok := c.entries[key]; ok && now.Before(entry.expiresAt) {
		c.mu.Unlock()
		return entry.url
	}
	c.mu.Unlock()

	logo, err := c.read(ctx, key)
	if err != nil {
		if ctx.Err() != nil {
			// The caller ran out of time; that says nothing about the token.
			return ""
		}
		slog.Warn("b20 logo unavailable", "token", key, "err", err.Error())
	}
	ttl := logoTTL
	if err != nil {
		ttl = logoFailureTTL
	}
	c.mu.Lock()
	c.entries[key] = logoEntry{url: logo, expiresAt: now.Add(ttl)}
	c.mu.Unlock()
	return logo
}

func (c *contractLogos) read(ctx context.Context, token string) (string, error) {
	raw, err := c.chain.Call(ctx, token, contractURISelector)
	if err != nil {
		return "", fmt.Errorf("contractURI call: %w", err)
	}
	uri, err := decodeABIString(raw)
	if err != nil {
		return "", fmt.Errorf("contractURI decode: %w", err)
	}
	return LogoFromContractURI(uri)
}

// LogoFromContractURI extracts a trusted https image URL from an ERC-7572
// contractURI. Only an inline data:application/json document is read; a URI that
// points elsewhere would be a second, unbounded fetch and is reported as
// unavailable instead.
func LogoFromContractURI(uri string) (string, error) {
	const jsonBase64 = "data:application/json;base64,"
	const jsonPlain = "data:application/json,"
	var doc []byte
	switch {
	case strings.HasPrefix(uri, jsonBase64):
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, jsonBase64))
		if err != nil {
			return "", fmt.Errorf("metadata is not base64: %w", err)
		}
		doc = decoded
	case strings.HasPrefix(uri, jsonPlain):
		unescaped, err := url.PathUnescape(strings.TrimPrefix(uri, jsonPlain))
		if err != nil {
			return "", fmt.Errorf("metadata is not a JSON data URI: %w", err)
		}
		doc = []byte(unescaped)
	case uri == "":
		return "", errors.New("no contract metadata")
	default:
		return "", errors.New("contract metadata is not inline JSON")
	}
	if len(doc) > maxContractURIBytes {
		return "", errors.New("contract metadata too large")
	}
	var meta struct {
		Image string `json:"image"`
	}
	if err := json.Unmarshal(doc, &meta); err != nil {
		return "", fmt.Errorf("metadata JSON: %w", err)
	}
	return trustedLogoURL(meta.Image)
}

func trustedLogoURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("metadata has no image")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("image URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.User != nil || !issuerLogoHosts[strings.ToLower(parsed.Hostname())] || parsed.Port() != "" {
		return "", fmt.Errorf("image URL %q is not on the issuer's metadata host", raw)
	}
	return parsed.String(), nil
}

// decodeABIString decodes a single ABI-encoded `string` return value.
func decodeABIString(raw []byte) (string, error) {
	if len(raw) < 64 {
		return "", errors.New("short return data")
	}
	offset := new(big.Int).SetBytes(raw[:32])
	if !offset.IsInt64() || offset.Int64() < 0 || offset.Int64()+32 > int64(len(raw)) {
		return "", errors.New("bad string offset")
	}
	start := offset.Int64()
	length := new(big.Int).SetBytes(raw[start : start+32])
	if !length.IsInt64() || length.Int64() > maxContractURIBytes*2 || start+32+length.Int64() > int64(len(raw)) {
		return "", errors.New("bad string length")
	}
	return string(raw[start+32 : start+32+length.Int64()]), nil
}
