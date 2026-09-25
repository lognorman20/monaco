// Package treasury is the Solana side of a group treasury: a Privy server wallet that holds
// SPL USDC, pays it out with a relayer-paid transfer, and reports confirmed inbound USDC.
//
// Base USDC and the Dynamic signer stay in package wallets. Nothing here emits EVM calldata.
package treasury

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrAPI wraps a Privy or Solana RPC failure.
	ErrAPI = errors.New("solana treasury: api error")
	// ErrNotConfigured means the Privy/Solana credentials are missing, so no Solana treasury
	// can be provisioned or signed for.
	ErrNotConfigured = errors.New("solana treasury: not configured")
)

// TreasuryRef is a group treasury Solana wallet provisioned in Privy.
type TreasuryRef struct {
	GroupID       string
	PrivyWalletID string
	SolanaAddress string
}

// PayUSDCRequest is an SPL USDC transfer out of a treasury.
type PayUSDCRequest struct {
	TreasuryRef TreasuryRef
	ToAddress   string
	Amount      int64
}

// PreparedPayout is a fully signed treasury payout that has not been broadcast yet. Its
// signature is known before anything reaches the chain, so the caller persists it first.
type PreparedPayout struct {
	TxSignature          string
	SignedTransaction    string
	LastValidBlockHeight uint64
}

// PayoutState is where a payout transaction stands on chain.
type PayoutState string

const (
	// PayoutStatePending: not seen at confirmed commitment yet, and its blockhash is still valid.
	PayoutStatePending PayoutState = "pending"
	// PayoutStateConfirmed: landed without error at confirmed or finalized commitment.
	PayoutStateConfirmed PayoutState = "confirmed"
	// PayoutStateFailed: landed and the chain rejected it. No USDC moved.
	PayoutStateFailed PayoutState = "failed"
	// PayoutStateDropped: never landed and its blockhash has expired, so it never can.
	PayoutStateDropped PayoutState = "dropped"
)

// PayoutStatus is the on-chain fate of a payout signature.
type PayoutStatus struct {
	State  PayoutState
	Reason string
}

// InboundQuery selects confirmed SPL USDC transfers into a treasury from one sender.
type InboundQuery struct {
	TreasuryAddress string
	FromAddress     string
	// Since drops transfers whose block time is earlier.
	Since time.Time
}

// InboundTransfer is one confirmed SPL USDC transfer into a treasury.
type InboundTransfer struct {
	TxSignature string
	FromAddress string
	Amount      int64
	BlockTime   time.Time
}

// Client is everything the agent deployment job needs from a Solana treasury.
type Client interface {
	EnsureTreasury(ctx context.Context, groupID string) (TreasuryRef, error)
	TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error)
	// A payout is three steps so the caller can persist the signature between signing and
	// broadcasting, and settle only on what the chain reports.
	PrepareUSDCPayout(ctx context.Context, req PayUSDCRequest) (PreparedPayout, error)
	BroadcastUSDCPayout(ctx context.Context, payout PreparedPayout) error
	USDCPayoutStatus(ctx context.Context, payout PreparedPayout) (PayoutStatus, error)
	ListInboundUSDCTransfers(ctx context.Context, q InboundQuery) ([]InboundTransfer, error)
}

// Config holds Privy app credentials, the SOL fee payer, and the Solana RPC endpoint.
type Config struct {
	PrivyAppID                   string
	PrivyAppSecret               string
	PrivyAuthorizationPrivateKey string
	PrivyAuthorizationKeyID      string
	RelayerPrivateKey            string
	RPCURL                       string
}

// Enabled reports whether every credential a treasury payout needs is present.
func (c Config) Enabled() bool {
	return c.PrivyAppID != "" && c.PrivyAppSecret != "" && c.PrivyAuthorizationPrivateKey != "" &&
		c.RelayerPrivateKey != "" && c.RPCURL != ""
}

const defaultPrivyBaseURL = "https://api.privy.io"

// DefaultRPCURL is the public Solana mainnet endpoint. It rate-limits; set SOLANA_RPC_URL.
const DefaultRPCURL = "https://api.mainnet-beta.solana.com"

// HTTPClient calls Privy for wallet custody and Solana RPC for chain state.
type HTTPClient struct {
	cfg        Config
	privyURL   string
	httpClient *http.Client
}

// NewHTTPClient builds a live client. Callers check cfg.Enabled first.
func NewHTTPClient(cfg Config) *HTTPClient {
	cfg.RPCURL = strings.TrimSpace(cfg.RPCURL)
	if cfg.RPCURL == "" {
		cfg.RPCURL = DefaultRPCURL
	}
	return &HTTPClient{
		cfg:        cfg,
		privyURL:   defaultPrivyBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewHTTPClientForTest points Privy and Solana RPC at test servers.
func NewHTTPClientForTest(cfg Config, privyURL string, httpClient *http.Client) *HTTPClient {
	c := NewHTTPClient(cfg)
	c.privyURL = privyURL
	if httpClient != nil {
		c.httpClient = httpClient
	}
	return c
}

var _ Client = (*HTTPClient)(nil)
