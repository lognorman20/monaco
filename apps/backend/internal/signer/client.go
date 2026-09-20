package signer

import (
	"context"
	"encoding/json"
	"math/big"
)

// HealthInfo is returned by the signer sidecar health check.
type HealthInfo struct {
	RelayerAddress string
	ChainID        int64
}

// CreatedWallet is a newly created Dynamic server wallet.
type CreatedWallet struct {
	WalletID  string
	Address   string
	Metadata  json.RawMessage
	KeyShares json.RawMessage
}

// SignRequest signs EIP-712 typed data with a server wallet.
type SignRequest struct {
	Metadata  json.RawMessage
	KeyShares json.RawMessage
	TypedData any
}

// SendRequest sends a transaction from a server wallet.
type SendRequest struct {
	Metadata  json.RawMessage
	KeyShares json.RawMessage
	To        string
	Data      []byte
	ValueWei  *big.Int
}

// RelayerSendRequest sends a transaction from the relayer EOA.
type RelayerSendRequest struct {
	To       string
	Data     []byte
	ValueWei *big.Int
}

// Client talks to the signer sidecar.
type Client interface {
	Health(ctx context.Context) (HealthInfo, error)
	CreateWallet(ctx context.Context) (CreatedWallet, error)
	SignTypedData(ctx context.Context, req SignRequest) (string, error)
	SendTransaction(ctx context.Context, req SendRequest) (string, error)
	RelayerSend(ctx context.Context, req RelayerSendRequest) (string, error)
}
