package privy

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
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

// SweepClient is the sweep poller's view of Privy. Sweeps are split in two so the poller can
// persist the transaction signature between PrepareSweep and BroadcastSweep.
type SweepClient interface {
	MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error)
	PrepareSweep(ctx context.Context, req SweepRequest) (PreparedSweep, error)
	BroadcastSweep(ctx context.Context, prepared PreparedSweep) (SweepResult, error)
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
	solanaRPCURL                 string // test override; empty uses cluster default
	httpClient                   *http.Client
}

// NewHTTPClient builds a Privy client from API config.
func NewHTTPClient(cfg *config.Config) *HTTPClient {
	client := &HTTPClient{
		appID:                        cfg.PrivyAppID,
		appSecret:                    cfg.PrivyAppSecret,
		privyAuthorizationPrivateKey: cfg.PrivyAuthorizationPrivateKey,
		privyAuthorizationKeyID:      cfg.PrivyAuthorizationKeyID,
		relayerPrivateKey:            cfg.RelayerPrivateKey,
		baseURL:                      defaultBaseURL,
		solanaCluster:                cfg.SolanaCluster,
		solanaRPCURL:                 cfg.SolanaRPCURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	client.httpClient.Transport = client.instrumentedTransport(nil)
	return client
}

// instrumentedTransport labels each outbound call for metrics. This client talks to two
// upstreams through one http.Client: Privy's API, and Solana RPC for balances and sends.
func (c *HTTPClient) instrumentedTransport(inner http.RoundTripper) http.RoundTripper {
	return telemetry.TransportFunc(func(req *http.Request) string {
		if rpc, err := url.Parse(c.solanaRPCEndpoint()); err == nil && req.URL.Host == rpc.Host {
			return telemetry.UpstreamSolanaRPC
		}
		return telemetry.UpstreamPrivy
	}, inner)
}

// NewHTTPClientWithTransport is used in tests to inject an httptest server transport.
func NewHTTPClientWithTransport(cfg *config.Config, baseURL string, transport http.RoundTripper) *HTTPClient {
	client := NewHTTPClient(cfg)
	client.baseURL = baseURL
	client.httpClient = &http.Client{
		Timeout:   30 * time.Second,
		Transport: client.instrumentedTransport(transport),
	}
	return client
}
