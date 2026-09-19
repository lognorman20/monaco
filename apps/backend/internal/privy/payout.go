package privy

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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
	if req.Amount <= 0 || req.ToAddress == "" || req.TreasuryAddress == "" {
		return PayUSDCResult{}, fmt.Errorf("%w: invalid payout request", ErrAPI)
	}
	if req.TreasuryPrivyWalletID == "" {
		return PayUSDCResult{}, fmt.Errorf("%w: treasury wallet id required", ErrAPI)
	}
	if c.relayerPrivateKey == "" {
		return PayUSDCResult{}, fmt.Errorf("%w: relayer key required", ErrAPI)
	}

	blockhash, err := c.getLatestBlockhash(ctx)
	if err != nil {
		return PayUSDCResult{}, err
	}

	txBase64, err := buildUSDCPayoutTransaction(usdcPayoutRequest{
		RelayerKey:      c.relayerPrivateKey,
		TreasuryAddress: req.TreasuryAddress,
		ToAddress:       req.ToAddress,
		Amount:          req.Amount,
	}, blockhash)
	if err != nil {
		return PayUSDCResult{}, err
	}

	hash, err := c.signAndSendSolanaTransaction(ctx, req.TreasuryPrivyWalletID, txBase64)
	if err != nil {
		return PayUSDCResult{}, err
	}

	return PayUSDCResult{TxSignature: hash}, nil
}

type usdcPayoutRequest struct {
	RelayerKey      string
	TreasuryAddress string
	ToAddress       string
	Amount          int64
}

func buildUSDCPayoutTransaction(req usdcPayoutRequest, recentBlockhash []byte) (string, error) {
	relayerPriv, relayerPub, err := decodeSolanaKeypair(req.RelayerKey)
	if err != nil {
		return "", fmt.Errorf("%w: invalid relayer key: %v", ErrAPI, err)
	}

	treasuryPub, err := decodeBase58Pubkey(req.TreasuryAddress)
	if err != nil {
		return "", fmt.Errorf("%w: invalid treasury address: %v", ErrAPI, err)
	}
	recipientPub, err := decodeBase58Pubkey(req.ToAddress)
	if err != nil {
		return "", fmt.Errorf("%w: invalid payout address: %v", ErrAPI, err)
	}
	mintPub, err := decodeBase58Pubkey(usdcMintAddress)
	if err != nil {
		return "", err
	}
	tokenProgram, err := decodeBase58Pubkey(tokenProgramID)
	if err != nil {
		return "", err
	}
	ataProgram, err := decodeBase58Pubkey(associatedTokenProg)
	if err != nil {
		return "", err
	}
	systemProgram, err := decodeBase58Pubkey(systemProgramID)
	if err != nil {
		return "", err
	}
	if len(recentBlockhash) != 32 {
		return "", fmt.Errorf("%w: invalid recent blockhash length %d", ErrAPI, len(recentBlockhash))
	}

	sourceATA, err := findAssociatedTokenAddress(treasuryPub, mintPub, tokenProgram, ataProgram)
	if err != nil {
		return "", err
	}
	destATA, err := findAssociatedTokenAddress(recipientPub, mintPub, tokenProgram, ataProgram)
	if err != nil {
		return "", err
	}

	accounts := [][]byte{
		relayerPub,
		treasuryPub,
		sourceATA,
		destATA,
		recipientPub,
		mintPub,
		systemProgram,
		tokenProgram,
		ataProgram,
	}

	createATA := compiledInstruction{
		programIDIndex: 8,
		accounts:       []byte{0, 3, 4, 5, 6, 7},
		data:           []byte{1},
	}
	transferData := make([]byte, 9)
	transferData[0] = 3
	binary.LittleEndian.PutUint64(transferData[1:], uint64(req.Amount))
	transfer := compiledInstruction{
		programIDIndex: 7,
		accounts:       []byte{2, 3, 1},
		data:           transferData,
	}

	message := encodeLegacyMessage(
		[]byte{2, 0, 5},
		accounts,
		recentBlockhash,
		[]compiledInstruction{createATA, transfer},
	)

	tx := encodeTransaction(message, [][]byte{relayerPriv, nil})
	signed, err := signTransaction(tx, relayerPriv, 0)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(signed), nil
}

func deterministicPayoutSignature(userID, payoutAddress string) string {
	sum := sha256.Sum256([]byte(PayoutMessage(userID, payoutAddress)))
	return "PROOF" + hex.EncodeToString(sum[:16])
}
