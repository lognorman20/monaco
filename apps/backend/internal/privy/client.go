package privy

import (
	"context"
	"crypto/ecdsa"
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
	MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error)
	TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error)
	SubmitSweep(ctx context.Context, req SweepRequest) (SweepResult, error)
	SubmitMemberUSDCTransfer(ctx context.Context, req TransferRequest) (TransferResult, error)
	VerifyPayoutProof(ctx context.Context, userID string, proof PayoutProof) error
	PayUSDC(ctx context.Context, req PayUSDCRequest) (PayUSDCResult, error)
}

// HTTPClient calls Privy REST APIs with app credentials.
type HTTPClient struct {
	appID                        string
	appSecret                    string
	privyAuthorizationPrivateKey string
	privyAuthorizationKeyID      string
	relayerPrivateKey            string
	baseURL                      string
	solanaCluster                string
	solanaRPCURL                 string // SOLANA_RPC_URL; empty uses the public cluster endpoint
	verificationKey              *ecdsa.PublicKey
	httpClient                   *http.Client
}

// NewHTTPClient builds a Privy client from API config.
func NewHTTPClient(cfg *config.Config) *HTTPClient {
	return &HTTPClient{
		appID:                        cfg.PrivyAppID,
		appSecret:                    cfg.PrivyAppSecret,
		privyAuthorizationPrivateKey: cfg.PrivyAuthorizationPrivateKey,
		privyAuthorizationKeyID:      cfg.PrivyAuthorizationKeyID,
		relayerPrivateKey:            cfg.RelayerPrivateKey,
		baseURL:                      defaultBaseURL,
		solanaCluster:                cfg.SolanaCluster,
		solanaRPCURL:                 cfg.SolanaRPCURL,
		verificationKey:              cfg.PrivyVerificationKey,
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
