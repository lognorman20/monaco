package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// TreasurySigner signs Solana swap transactions with the app-owned treasury wallet.
type TreasurySigner interface {
	SignTreasuryTransaction(ctx context.Context, walletID, unsignedTxBase64 string) (string, error)
}

// PrivyTreasurySigner signs via Privy wallet RPC.
type PrivyTreasurySigner struct {
	client *privy.HTTPClient
}

// NewPrivyTreasurySigner returns a treasury signer backed by Privy HTTP RPC.
func NewPrivyTreasurySigner(client *privy.HTTPClient) *PrivyTreasurySigner {
	return &PrivyTreasurySigner{client: client}
}

// SignTreasuryTransaction signs an unsigned base64 transaction with the treasury wallet.
func (s *PrivyTreasurySigner) SignTreasuryTransaction(ctx context.Context, walletID, unsignedTxBase64 string) (string, error) {
	if walletID == "" || unsignedTxBase64 == "" {
		return "", fmt.Errorf("wallet id and unsigned transaction are required")
	}
	return s.client.SignSolanaTransaction(ctx, walletID, unsignedTxBase64)
}

// fakePrivyTreasurySigner is the locked test double for treasury signing.
type fakePrivyTreasurySigner struct{}

// NewFakePrivyTreasurySigner returns a deterministic treasury signer for tests.
func NewFakePrivyTreasurySigner() TreasurySigner {
	return &fakePrivyTreasurySigner{}
}

func (f *fakePrivyTreasurySigner) SignTreasuryTransaction(ctx context.Context, walletID, unsignedTxBase64 string) (string, error) {
	_ = ctx
	if walletID == "" || unsignedTxBase64 == "" {
		return "", fmt.Errorf("wallet id and unsigned transaction are required")
	}
	sum := sha256.Sum256([]byte("signed:" + walletID + ":" + unsignedTxBase64))
	return "SIGNED" + hex.EncodeToString(sum[:16]), nil
}
