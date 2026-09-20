package privy

import (
	"bytes"
	"context"
	"crypto/ed25519"
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

// PreparedPayout is a fully signed treasury payout that has not been broadcast yet. Its
// signature is known before anything reaches the chain, so the caller can persist it first.
type PreparedPayout struct {
	TxSignature          string
	SignedTransaction    string
	LastValidBlockHeight int64
}

// PayoutState is where a payout transaction stands on chain.
type PayoutState string

const (
	// PayoutStatePending: not seen on chain yet, and its blockhash is still valid.
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
	State PayoutState
	// Reason is the chain's error for PayoutStateFailed.
	Reason string
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

// PrepareUSDCPayout builds and signs a treasury USDC payout without broadcasting it.
func (c *HTTPClient) PrepareUSDCPayout(ctx context.Context, req PayUSDCRequest) (PreparedPayout, error) {
	if req.Amount <= 0 || req.ToAddress == "" || req.TreasuryAddress == "" {
		return PreparedPayout{}, fmt.Errorf("%w: invalid payout request", ErrAPI)
	}
	if req.TreasuryPrivyWalletID == "" {
		return PreparedPayout{}, fmt.Errorf("%w: treasury wallet id required", ErrAPI)
	}
	if c.relayerPrivateKey == "" {
		return PreparedPayout{}, fmt.Errorf("%w: relayer key required", ErrAPI)
	}

	blockhash, lastValidBlockHeight, err := c.getLatestBlockhashWithExpiry(ctx)
	if err != nil {
		return PreparedPayout{}, err
	}

	txBase64, err := buildUSDCPayoutTransaction(usdcPayoutRequest{
		RelayerKey:      c.relayerPrivateKey,
		TreasuryAddress: req.TreasuryAddress,
		ToAddress:       req.ToAddress,
		Amount:          req.Amount,
	}, blockhash)
	if err != nil {
		return PreparedPayout{}, err
	}
	// The relayer is the fee payer, so its signature is the transaction id.
	relayerSignature, err := firstTransactionSignature(txBase64)
	if err != nil {
		return PreparedPayout{}, err
	}

	signed, err := c.signSolanaTransaction(ctx, req.TreasuryPrivyWalletID, txBase64)
	if err != nil {
		return PreparedPayout{}, err
	}
	signature, err := firstTransactionSignature(signed)
	if err != nil {
		return PreparedPayout{}, err
	}
	if signature != relayerSignature {
		return PreparedPayout{}, fmt.Errorf("%w: signed payout does not carry the relayer signature", ErrAPI)
	}

	return PreparedPayout{
		TxSignature:          signature,
		SignedTransaction:    signed,
		LastValidBlockHeight: lastValidBlockHeight,
	}, nil
}

// BroadcastUSDCPayout sends a prepared payout to the cluster. Sending the same signed
// transaction again is harmless: it carries one signature and can only land once. An error
// does not prove the transfer missed the chain; USDCPayoutStatus is the only authority.
func (c *HTTPClient) BroadcastUSDCPayout(ctx context.Context, payout PreparedPayout) error {
	if payout.SignedTransaction == "" {
		return fmt.Errorf("%w: signed payout transaction required", ErrAPI)
	}
	var txSignature string
	return c.callSolanaRPC(ctx, "sendTransaction", []any{
		payout.SignedTransaction,
		map[string]any{"encoding": "base64", "preflightCommitment": "confirmed"},
	}, &txSignature)
}

// USDCPayoutStatus reports whether a payout landed, failed, is still in flight, or can no
// longer land because its blockhash expired.
func (c *HTTPClient) USDCPayoutStatus(ctx context.Context, payout PreparedPayout) (PayoutStatus, error) {
	if payout.TxSignature == "" || payout.LastValidBlockHeight <= 0 {
		return PayoutStatus{}, fmt.Errorf("%w: payout signature and last valid block height required", ErrAPI)
	}

	status, seen, err := c.getSignatureStatus(ctx, payout.TxSignature)
	if err != nil {
		return PayoutStatus{}, err
	}
	if seen {
		return status, nil
	}

	blockHeight, err := c.getFinalizedBlockHeight(ctx)
	if err != nil {
		return PayoutStatus{}, err
	}
	if blockHeight <= payout.LastValidBlockHeight {
		return PayoutStatus{State: PayoutStatePending}, nil
	}

	// The blockhash is dead at finalized commitment. Anything that landed did so in a block
	// that is final by now, so one more look settles it either way.
	status, seen, err = c.getSignatureStatus(ctx, payout.TxSignature)
	if err != nil {
		return PayoutStatus{}, err
	}
	if seen {
		return status, nil
	}
	return PayoutStatus{State: PayoutStateDropped}, nil
}

// firstTransactionSignature returns the base58 transaction id of a base64 wire transaction.
func firstTransactionSignature(txBase64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		return "", fmt.Errorf("%w: decode transaction: %v", ErrAPI, err)
	}
	sigCount, offset, err := decodeCompactU16(raw)
	if err != nil {
		return "", fmt.Errorf("%w: decode transaction: %v", ErrAPI, err)
	}
	if sigCount == 0 || len(raw) < offset+ed25519.SignatureSize {
		return "", fmt.Errorf("%w: transaction has no signature", ErrAPI)
	}
	signature := raw[offset : offset+ed25519.SignatureSize]
	if bytes.Equal(signature, make([]byte, ed25519.SignatureSize)) {
		return "", fmt.Errorf("%w: transaction fee payer signature is empty", ErrAPI)
	}
	return encodeBase58(signature), nil
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
