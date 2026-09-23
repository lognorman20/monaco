package evm

import (
	"context"
	"math/big"
	"time"
)

// Log is a simplified transaction log for receipt parsing.
type Log struct {
	Address string
	Topics  []string
	Data    []byte
}

// Receipt is an EVM transaction receipt.
type Receipt struct {
	Status uint64
	Logs   []Log
	Found  bool
}

// RoundData is a Chainlink latestRoundData answer.
type RoundData struct {
	RoundID   *big.Int
	Answer    *big.Int
	UpdatedAt time.Time
}

// Client reads Base chain state.
type Client interface {
	Call(ctx context.Context, to string, data []byte) ([]byte, error)
	ERC20Balance(ctx context.Context, token, holder string) (*big.Int, error)
	ETHBalance(ctx context.Context, addr string) (*big.Int, error)
	Allowance(ctx context.Context, token, owner, spender string) (*big.Int, error)
	Receipt(ctx context.Context, txHash string) (Receipt, error)
	IsConfirmed(ctx context.Context, txHash string) (bool, error)
	ChainlinkLatestRoundData(ctx context.Context, feed string) (RoundData, error)
	ChainlinkLatestRoundDataMany(ctx context.Context, feeds []string) (map[string]RoundData, error)
	// ChainlinkRoundsSince reads a feed's round history back to an instant, so a
	// caller asks for a window rather than for a round count it cannot translate.
	ChainlinkRoundsSince(ctx context.Context, feed string, since time.Time, maxRounds int) (RoundHistory, error)
}
