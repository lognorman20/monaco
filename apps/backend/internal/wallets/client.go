package wallets

import (
	"context"
	"math/big"
)

// Client provisions server wallets and submits chain transactions.
type Client interface {
	EnsureMemberWallet(ctx context.Context, dynamicUserID string, userID UserID) (WalletRef, error)
	EnsureTreasury(ctx context.Context, groupID GroupID) (TreasuryRef, error)
	MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error)
	TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error)
	SubmitSweep(ctx context.Context, req SweepRequest) (SweepResult, error)
	SubmitMemberUSDCTransfer(ctx context.Context, req TransferRequest) (TransferResult, error)
	PayUSDC(ctx context.Context, req PayUSDCRequest) (PayUSDCResult, error)
	SendTreasuryTransaction(ctx context.Context, treasury TreasuryRef, to string, data []byte, valueWei *big.Int) (txHash string, err error)
}
