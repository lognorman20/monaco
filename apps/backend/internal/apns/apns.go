// Package apns sends alert notifications through Apple Push Notification service with
// token-based (.p8) authentication over HTTP/2.
//
// The client knows the protocol and nothing about Monaco: which members get a push, and what
// happens to a token Apple no longer accepts, is the caller's business (see
// app.APNsPushSender).
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Hosts Apple serves the provider API on.
const (
	ProductionURL = "https://api.push.apple.com"
	SandboxURL    = "https://api.sandbox.push.apple.com"
)

// Env names which APNs host a device token belongs to.
type Env string

const (
	EnvSandbox    Env = "sandbox"
	EnvProduction Env = "production"
)

// ParseEnv reads APNS_ENV. Blank is sandbox: a development build is the safe default.
func ParseEnv(raw string) (Env, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "sandbox", "development":
		return EnvSandbox, nil
	case "production":
		return EnvProduction, nil
	default:
		return "", fmt.Errorf("APNS_ENV must be sandbox or production, got %q", raw)
	}
}

// tokenLifetime is how long one provider token is reused. Apple accepts a token for an hour
// and refuses one refreshed more often than every 20 minutes, so 50 minutes sits between.
const tokenLifetime = 50 * time.Minute

// ErrUnregistered means Apple answered 410: the app was removed from that device, or the
// token is otherwise dead. The caller should forget the token.
var ErrUnregistered = errors.New("apns: device token is no longer registered")

// Config is the provider identity from the Apple developer account.
type Config struct {
	KeyID  string
	TeamID string
	// PrivateKeyPEM is the contents of the AuthKey_<KeyID>.p8 file.
	PrivateKeyPEM string
	// BundleID is the app's bundle identifier, sent as apns-topic.
	BundleID string
	Env      Env
}

// Alert is what the lock screen shows.
type Alert struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

// Payload is one notification. Badge is the app icon number (the member's unread count).
// GroupID and ProposalID tell the app where a tap goes.
type Payload struct {
	Alert          Alert
	Badge          int
	Sound          string
	ThreadID       string
	GroupID        string
	ProposalID     string
	NotificationID string
	Kind           string
	// CollapseID replaces an earlier notification with the same id on the device.
	CollapseID string
}

// MarshalJSON writes the APNs body: `aps` plus Monaco's routing keys at the top level.
func (p Payload) MarshalJSON() ([]byte, error) {
	aps := map[string]any{
		"alert": p.Alert,
		"badge": p.Badge,
	}
	if p.Sound != "" {
		aps["sound"] = p.Sound
	}
	if p.ThreadID != "" {
		aps["thread-id"] = p.ThreadID
	}
	body := map[string]any{"aps": aps}
	if p.GroupID != "" {
		body["groupId"] = p.GroupID
	}
	if p.ProposalID != "" {
		body["proposalId"] = p.ProposalID
	}
	if p.NotificationID != "" {
		body["notificationId"] = p.NotificationID
	}
	if p.Kind != "" {
		body["kind"] = p.Kind
	}
	return json.Marshal(body)
}

// Device is one install to deliver to.
type Device struct {
	Token string
	Env   Env
}

// Client sends to APNs. Safe for concurrent use.
type Client struct {
	cfg           Config
	key           *ecdsa.PrivateKey
	http          *http.Client
	productionURL string
	sandboxURL    string
	now           func() time.Time

	mu       sync.Mutex
	token    string
	issuedAt time.Time
}

// NewClient parses the .p8 key and builds an HTTP/2 client. A key pasted into an env var with
// literal `\n` sequences is accepted.
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.KeyID) == "" || strings.TrimSpace(cfg.TeamID) == "" || strings.TrimSpace(cfg.BundleID) == "" {
		return nil, fmt.Errorf("apns: key id, team id and bundle id are required")
	}
	key, err := ParsePrivateKey(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	if cfg.Env == "" {
		cfg.Env = EnvSandbox
	}
	return &Client{
		cfg: cfg,
		key: key,
		http: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{ForceAttemptHTTP2: true, MaxIdleConnsPerHost: 4, IdleConnTimeout: 5 * time.Minute},
		},
		productionURL: ProductionURL,
		sandboxURL:    SandboxURL,
		now:           time.Now,
	}, nil
}

// WithEndpoints points the client at other hosts and another HTTP client. For tests.
func (c *Client) WithEndpoints(httpClient *http.Client, productionURL, sandboxURL string) *Client {
	c.http = httpClient
	c.productionURL = strings.TrimRight(productionURL, "/")
	c.sandboxURL = strings.TrimRight(sandboxURL, "/")
	return c
}

// WithClock replaces time.Now. For tests.
func (c *Client) WithClock(now func() time.Time) *Client {
	c.now = now
	return c
}

// DefaultEnv is the host a device goes to when it did not say which build registered it.
func (c *Client) DefaultEnv() Env { return c.cfg.Env }

// ParsePrivateKey reads an Apple .p8 (PKCS#8, P-256) key.
func ParsePrivateKey(raw string) (*ecdsa.PrivateKey, error) {
	text := strings.TrimSpace(strings.ReplaceAll(raw, `\n`, "\n"))
	if text == "" {
		return nil, fmt.Errorf("apns: private key is empty")
	}
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, fmt.Errorf("apns: private key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if ec, ecErr := x509.ParseECPrivateKey(block.Bytes); ecErr == nil {
			parsed = ec
		} else {
			return nil, fmt.Errorf("apns: parse private key: %w", err)
		}
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("apns: private key is not an EC key")
	}
	if key.Curve.Params().Name != "P-256" {
		return nil, fmt.Errorf("apns: private key must be P-256")
	}
	return key, nil
}

// ProviderToken returns the cached ES256 JWT, minting a new one after tokenLifetime.
func (c *Client) ProviderToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.token != "" && now.Sub(c.issuedAt) < tokenLifetime {
		return c.token, nil
	}
	claims := jwt.MapClaims{"iss": c.cfg.TeamID, "iat": now.Unix()}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = c.cfg.KeyID
	signed, err := tok.SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("apns: sign provider token: %w", err)
	}
	c.token = signed
	c.issuedAt = now
	return signed, nil
}

// resetProviderToken drops the cached token after Apple rejected it.
func (c *Client) resetProviderToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

// Error is a non-200 answer from APNs.
type Error struct {
	Status int
	Reason string
}

func (e *Error) Error() string {
	return fmt.Sprintf("apns: %d %s", e.Status, e.Reason)
}

// Send delivers one payload to one device. ErrUnregistered on 410; *Error for other refusals.
func (c *Client) Send(ctx context.Context, device Device, payload Payload) error {
	err := c.send(ctx, device, payload)
	var apnsErr *Error
	if errors.As(err, &apnsErr) && apnsErr.Status == http.StatusForbidden &&
		(apnsErr.Reason == "ExpiredProviderToken" || apnsErr.Reason == "InvalidProviderToken") {
		// A token the cache thought was fresh: mint another and try once more.
		c.resetProviderToken()
		err = c.send(ctx, device, payload)
	}
	return err
}

func (c *Client) send(ctx context.Context, device Device, payload Payload) error {
	token := strings.TrimSpace(device.Token)
	if token == "" {
		return fmt.Errorf("apns: device token is empty")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("apns: encode payload: %w", err)
	}
	provider, err := c.ProviderToken()
	if err != nil {
		return err
	}
	base := c.sandboxURL
	env := device.Env
	if env == "" {
		env = c.cfg.Env
	}
	if env == EnvProduction {
		base = c.productionURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/3/device/"+token, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("apns: build request: %w", err)
	}
	req.Header.Set("authorization", "bearer "+provider)
	req.Header.Set("apns-topic", c.cfg.BundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("content-type", "application/json")
	if payload.CollapseID != "" {
		req.Header.Set("apns-collapse-id", payload.CollapseID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("apns: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	var answer struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&answer)
	if resp.StatusCode == http.StatusGone {
		return fmt.Errorf("%w (%s)", ErrUnregistered, answer.Reason)
	}
	return &Error{Status: resp.StatusCode, Reason: answer.Reason}
}
