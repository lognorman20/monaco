package privy

import (
	"context"
	"fmt"
	"strings"
)

// TransferRequest is a server-signed USDC transfer from a member wallet to any Solana address.
type TransferRequest struct {
	MemberAddress string
	ToAddress     string
	Amount        int64
	RelayerKey    string
}

// TransferResult is the submitted member-wallet USDC transfer signature.
type TransferResult struct {
	TxSignature string
}

// ValidateSolanaAddress checks base58 length for a Solana public key.
func ValidateSolanaAddress(address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("address is required")
	}
	if _, err := decodeBase58Pubkey(address); err != nil {
		return fmt.Errorf("invalid solana address: %w", err)
	}
	return nil
}

// BuildTransferRequest validates member-wallet USDC transfer inputs.
func BuildTransferRequest(memberAddress, toAddress string, amount int64, relayerKey string) (TransferRequest, error) {
	if memberAddress == "" {
		return TransferRequest{}, fmt.Errorf("member address is required")
	}
	if err := ValidateSolanaAddress(toAddress); err != nil {
		return TransferRequest{}, err
	}
	if amount <= 0 {
		return TransferRequest{}, fmt.Errorf("amount must be positive")
	}
	if relayerKey == "" {
		return TransferRequest{}, fmt.Errorf("relayer key is required")
	}
	return TransferRequest{
		MemberAddress: memberAddress,
		ToAddress:     toAddress,
		Amount:        amount,
		RelayerKey:    relayerKey,
	}, nil
}

// SubmitMemberUSDCTransfer Privy-signs a USDC SPL transfer from member wallet to toAddress; relayer pays SOL.
func (c *HTTPClient) SubmitMemberUSDCTransfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	sweepReq, err := BuildSweepRequest(req.MemberAddress, req.ToAddress, req.Amount, req.RelayerKey)
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: %v", ErrAPI, err)
	}
	result, err := c.SubmitSweep(ctx, sweepReq)
	if err != nil {
		return TransferResult{}, err
	}
	return TransferResult{TxSignature: result.TxSignature}, nil
}
