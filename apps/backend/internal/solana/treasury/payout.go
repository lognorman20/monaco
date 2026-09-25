package treasury

// Treasury SPL USDC payout, ported from archive/main-before-dynamic
// apps/backend/internal/privy/payout.go. The relayer pays SOL fees and the recipient's token
// account rent; Privy co-signs as the treasury owner.

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
)

// PrepareUSDCPayout builds and signs a treasury USDC payout without broadcasting it.
func (c *HTTPClient) PrepareUSDCPayout(ctx context.Context, req PayUSDCRequest) (PreparedPayout, error) {
	if req.Amount <= 0 || strings.TrimSpace(req.ToAddress) == "" || req.TreasuryRef.SolanaAddress == "" {
		return PreparedPayout{}, fmt.Errorf("%w: invalid payout request", ErrAPI)
	}
	if req.TreasuryRef.PrivyWalletID == "" {
		return PreparedPayout{}, fmt.Errorf("%w: treasury wallet id required", ErrAPI)
	}
	if c.cfg.RelayerPrivateKey == "" {
		return PreparedPayout{}, fmt.Errorf("%w: relayer key required", ErrAPI)
	}

	blockhash, lastValidBlockHeight, err := c.getLatestBlockhashWithExpiry(ctx)
	if err != nil {
		return PreparedPayout{}, err
	}
	// Without it a transfer that never lands could never be told apart from one still in
	// flight, so nothing is signed.
	if lastValidBlockHeight == 0 {
		return PreparedPayout{}, fmt.Errorf("%w: solana rpc did not report a last valid block height", ErrAPI)
	}

	txBase64, err := buildUSDCPayoutTransaction(c.cfg.RelayerPrivateKey, req.TreasuryRef.SolanaAddress, strings.TrimSpace(req.ToAddress), req.Amount, blockhash)
	if err != nil {
		return PreparedPayout{}, err
	}
	// The relayer is the fee payer, so its signature is the transaction id.
	relayerSignature, err := feePayerSignature(txBase64)
	if err != nil {
		return PreparedPayout{}, err
	}

	signed, err := c.signSolanaTransaction(ctx, req.TreasuryRef.PrivyWalletID, txBase64)
	if err != nil {
		return PreparedPayout{}, err
	}
	signature, err := feePayerSignature(signed)
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

// BroadcastUSDCPayout sends a prepared payout. Sending the same signed transaction again is
// harmless: it can only land once. An error does not prove the transfer missed the chain;
// USDCPayoutStatus is the only authority.
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
	if payout.TxSignature == "" || payout.LastValidBlockHeight == 0 {
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
	// that is final by now, so one more look settles it.
	status, seen, err = c.getSignatureStatus(ctx, payout.TxSignature)
	if err != nil {
		return PayoutStatus{}, err
	}
	if seen {
		return status, nil
	}
	return PayoutStatus{State: PayoutStateDropped}, nil
}

func buildUSDCPayoutTransaction(relayerKey, treasuryAddress, toAddress string, amount int64, recentBlockhash []byte) (string, error) {
	relayerPriv, relayerPub, err := decodeSolanaKeypair(relayerKey)
	if err != nil {
		return "", fmt.Errorf("%w: invalid relayer key: %v", ErrAPI, err)
	}
	treasuryPub, err := decodeBase58Pubkey(treasuryAddress)
	if err != nil {
		return "", fmt.Errorf("%w: invalid treasury address: %v", ErrAPI, err)
	}
	recipientPub, err := decodeBase58Pubkey(toAddress)
	if err != nil {
		return "", fmt.Errorf("%w: invalid payout address: %v", ErrAPI, err)
	}
	mintPub, tokenProgram, ataProgram, err := usdcPrograms()
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
		relayerPub,    // 0 fee payer, signer, writable
		treasuryPub,   // 1 treasury owner, signer
		sourceATA,     // 2 writable
		destATA,       // 3 writable
		recipientPub,  // 4
		mintPub,       // 5
		systemProgram, // 6
		tokenProgram,  // 7
		ataProgram,    // 8
	}

	// Associated token account CreateIdempotent: a no-op when the agent already holds USDC.
	createATA := compiledInstruction{
		programIDIndex: 8,
		accounts:       []byte{0, 3, 4, 5, 6, 7},
		data:           []byte{1},
	}
	transferData := make([]byte, 9)
	transferData[0] = 3 // SPL Token Transfer
	binary.LittleEndian.PutUint64(transferData[1:], uint64(amount))
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
	signed, err := signTransaction(encodeTransaction(message, 2), relayerPriv, 0)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signed), nil
}
