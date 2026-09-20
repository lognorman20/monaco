package app

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
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

// SignTreasuryMessage returns the treasury wallet's raw Ed25519 signature over message.
func (s *PrivyTreasurySigner) SignTreasuryMessage(ctx context.Context, walletID string, message []byte) ([]byte, error) {
	if walletID == "" || len(message) == 0 {
		return nil, fmt.Errorf("wallet id and message are required")
	}
	return s.client.SignSolanaMessage(ctx, walletID, message)
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
	return base64.StdEncoding.EncodeToString(fakeSignedTransaction(walletID, unsignedTxBase64)), nil
}

// fakeSignedTransaction returns a well-formed, fully signed legacy Solana transaction derived
// from its inputs: one signature, one account key, a recent blockhash, no instructions. Swap
// code reads the transaction id and blockhash off signed bytes, so the fake has to parse.
func fakeSignedTransaction(walletID, unsignedTxBase64 string) []byte {
	signature := sha512.Sum512([]byte("signed:" + walletID + ":" + unsignedTxBase64))
	account := sha256.Sum256([]byte("account:" + walletID))
	blockhash := sha256.Sum256([]byte("blockhash:" + unsignedTxBase64))

	tx := []byte{1}
	tx = append(tx, signature[:]...)
	tx = append(tx, 1, 0, 0) // message header: one required signer
	tx = append(tx, 1)       // account key count
	tx = append(tx, account[:]...)
	tx = append(tx, blockhash[:]...)
	tx = append(tx, 0) // instruction count
	return tx
}

// SignTreasuryMessage returns a deterministic 64-byte stand-in for an Ed25519 signature.
func (f *fakePrivyTreasurySigner) SignTreasuryMessage(ctx context.Context, walletID string, message []byte) ([]byte, error) {
	_ = ctx
	if walletID == "" || len(message) == 0 {
		return nil, fmt.Errorf("wallet id and message are required")
	}
	first := sha256.Sum256(append([]byte("signed-message:"+walletID+":"), message...))
	second := sha256.Sum256(first[:])
	return append(first[:], second[:]...), nil
}
