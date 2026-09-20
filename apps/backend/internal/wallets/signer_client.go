package wallets

import (
	"context"
	"errors"
	"math/big"

	"github.com/monaco/monaco/apps/backend/internal/signer"
)

// ErrNotConfigured means the signer-backed wallet client is not wired yet.
var ErrNotConfigured = errors.New("signer wallet client not configured")

type signerClient struct {
	signer signer.Client
}

// NewSignerClient builds a wallets client over the signer sidecar (stub until M6-T4).
func NewSignerClient(s signer.Client, chain interface{}, store interface{}, sharesKey []byte, relayerAddress string) Client {
	_ = chain
	_ = store
	_ = sharesKey
	_ = relayerAddress
	return &signerClient{signer: s}
}

func (c *signerClient) notReady() error {
	return ErrNotConfigured
}

func (c *signerClient) EnsureMemberWallet(ctx context.Context, dynamicUserID string, userID UserID) (WalletRef, error) {
	_ = ctx
	_ = dynamicUserID
	_ = userID
	return WalletRef{}, c.notReady()
}

func (c *signerClient) EnsureTreasury(ctx context.Context, groupID GroupID) (TreasuryRef, error) {
	_ = ctx
	_ = groupID
	return TreasuryRef{}, c.notReady()
}

func (c *signerClient) MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error) {
	_ = ctx
	_ = memberAddress
	return 0, c.notReady()
}

func (c *signerClient) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	_ = ctx
	_ = treasuryAddress
	return 0, c.notReady()
}

func (c *signerClient) SubmitSweep(ctx context.Context, req SweepRequest) (SweepResult, error) {
	_ = ctx
	_ = req
	return SweepResult{}, c.notReady()
}

func (c *signerClient) SubmitMemberUSDCTransfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	_ = ctx
	_ = req
	return TransferResult{}, c.notReady()
}

func (c *signerClient) PayUSDC(ctx context.Context, req PayUSDCRequest) (PayUSDCResult, error) {
	_ = ctx
	_ = req
	return PayUSDCResult{}, c.notReady()
}

func (c *signerClient) SendTreasuryTransaction(ctx context.Context, treasury TreasuryRef, to string, data []byte, valueWei *big.Int) (string, error) {
	_ = ctx
	_ = treasury
	_ = to
	_ = data
	_ = valueWei
	return "", c.notReady()
}
