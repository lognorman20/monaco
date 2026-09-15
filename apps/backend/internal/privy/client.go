package privy

import (
	"context"
	"net/http"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/config"
)

const defaultBaseURL = "https://api.privy.io"

// Client verifies Privy sessions and provisions Solana wallets.
type Client interface {
	VerifySession(ctx context.Context, token AccessToken) (Identity, error)
	EnsureMemberWallet(ctx context.Context, privyUserID string, userID UserID) (WalletRef, error)
	EnsureTreasury(ctx context.Context, groupID GroupID) (TreasuryRef, error)
}

// HTTPClient calls Privy REST APIs with app credentials.
type HTTPClient struct {
	appID      string
	appSecret  string
	baseURL    string
	httpClient *http.Client
}

// NewHTTPClient builds a Privy client from API config.
func NewHTTPClient(cfg *config.Config) *HTTPClient {
	return &HTTPClient{
		appID:     cfg.PrivyAppID,
		appSecret: cfg.PrivyAppSecret,
		baseURL:   defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewHTTPClientWithTransport is used in tests to inject an httptest server transport.
func NewHTTPClientWithTransport(cfg *config.Config, baseURL string, transport http.RoundTripper) *HTTPClient {
	client := NewHTTPClient(cfg)
	client.baseURL = baseURL
	client.httpClient = &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
	return client
}
