package privy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// PayoutProof binds a payout Solana address to a user via signed message.
type PayoutProof struct {
	PayoutAddress string
	Message       string
	Signature     string
}

// PayUSDCRequest is a treasury USDC payout to a proven external address.
type PayUSDCRequest struct {
	TreasuryPrivyWalletID string
	TreasuryAddress       string
	ToAddress             string
	Amount                int64
	GroupID               string
	UserID                string
}

// PayUSDCResult is the submitted payout transaction signature.
type PayUSDCResult struct {
	TxSignature string
}

// PayoutMessage returns the canonical ownership proof message for a user and payout address.
func PayoutMessage(userID, payoutAddress string) string {
	return fmt.Sprintf("monaco-payout:%s:%s", userID, payoutAddress)
}

// VerifyPayoutProof checks message format and signature registration (fake) or on-chain proof (HTTP).
func (c *HTTPClient) VerifyPayoutProof(ctx context.Context, userID string, proof PayoutProof) error {
	_ = ctx
	if strings.TrimSpace(proof.PayoutAddress) == "" {
		return ErrInvalidPayoutProof
	}
	if strings.TrimSpace(proof.Message) == "" || strings.TrimSpace(proof.Signature) == "" {
		return ErrInvalidPayoutProof
	}
	expected := PayoutMessage(userID, proof.PayoutAddress)
	if proof.Message != expected {
		return ErrInvalidPayoutProof
	}
	return fmt.Errorf("%w: live payout proof verification not implemented", ErrAPI)
}

// PayUSDC sends USDC from a group treasury to a proven payout address.
func (c *HTTPClient) PayUSDC(ctx context.Context, req PayUSDCRequest) (PayUSDCResult, error) {
	_ = ctx
	if req.Amount <= 0 || req.ToAddress == "" || req.TreasuryAddress == "" {
		return PayUSDCResult{}, fmt.Errorf("%w: invalid payout request", ErrAPI)
	}
	return PayUSDCResult{}, fmt.Errorf("%w: live USDC payout not implemented", ErrAPI)
}

func deterministicPayoutSignature(userID, payoutAddress string) string {
	sum := sha256.Sum256([]byte(PayoutMessage(userID, payoutAddress)))
	return "PROOF" + hex.EncodeToString(sum[:16])
}
