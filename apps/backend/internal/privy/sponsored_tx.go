package privy

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
)

// maxLegacyAccounts is the largest account index a compiled instruction can address.
const maxLegacyAccounts = 256

// InstructionAccount is one account of an instruction to compile into a transaction.
type InstructionAccount struct {
	Pubkey     string
	IsSigner   bool
	IsWritable bool
}

// Instruction is a Solana instruction supplied by a third party. Data is raw bytes.
type Instruction struct {
	ProgramID string
	Accounts  []InstructionAccount
	Data      []byte
}

// SponsoredInstructionsRequest sends instructions authorized by a server wallet with
// the relayer as SOL fee payer.
type SponsoredInstructionsRequest struct {
	WalletID      string
	WalletAddress string
	RelayerKey    string
	Instructions  []Instruction
}

type signMessageRPCRequest struct {
	ChainType string               `json:"chain_type"`
	Method    string               `json:"method"`
	Params    signMessageRPCParams `json:"params"`
}

type signMessageRPCParams struct {
	Message  string `json:"message"`
	Encoding string `json:"encoding"`
}

type signMessageRPCResponse struct {
	Data struct {
		Signature string `json:"signature"`
		Encoding  string `json:"encoding"`
	} `json:"data"`
}

// SignSolanaMessage returns the wallet's raw 64-byte Ed25519 signature over message.
func (c *HTTPClient) SignSolanaMessage(ctx context.Context, walletID string, message []byte) ([]byte, error) {
	if walletID == "" || len(message) == 0 {
		return nil, fmt.Errorf("%w: wallet id and message are required", ErrAPI)
	}
	payload, err := json.Marshal(signMessageRPCRequest{
		ChainType: "solana",
		Method:    "signMessage",
		Params: signMessageRPCParams{
			Message:  base64.StdEncoding.EncodeToString(message),
			Encoding: "base64",
		},
	})
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/v1/wallets/%s/rpc", url.PathEscape(walletID))
	respBody, status, err := c.doPrivyRequestWithAuthorization(ctx, http.MethodPost, path, payload, "", true)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%w: sign message status %d: %s", ErrAPI, status, string(respBody))
	}

	var rpcResp signMessageRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("%w: sign message invalid json: %v", ErrAPI, err)
	}
	if rpcResp.Data.Signature == "" {
		return nil, fmt.Errorf("%w: sign message missing signature", ErrAPI)
	}
	signature, err := base64.StdEncoding.DecodeString(rpcResp.Data.Signature)
	if err != nil {
		return nil, fmt.Errorf("%w: sign message signature is not base64: %v", ErrAPI, err)
	}
	if len(signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: sign message signature is %d bytes, want %d", ErrAPI, len(signature), ed25519.SignatureSize)
	}
	return signature, nil
}

// SubmitSponsoredInstructions compiles instructions into a transaction with the relayer
// as fee payer, signs the relayer slot locally, then has Privy sign for the wallet and broadcast.
func (c *HTTPClient) SubmitSponsoredInstructions(ctx context.Context, req SponsoredInstructionsRequest) (string, error) {
	if req.RelayerKey == "" {
		return "", fmt.Errorf("%w: relayer key required", ErrAPI)
	}
	if req.WalletID == "" || req.WalletAddress == "" {
		return "", fmt.Errorf("%w: wallet id and address are required", ErrAPI)
	}
	if len(req.Instructions) == 0 {
		return "", fmt.Errorf("%w: no instructions to submit", ErrAPI)
	}

	blockhash, err := c.getLatestBlockhash(ctx)
	if err != nil {
		return "", err
	}
	txBase64, err := buildSponsoredTransaction(req, blockhash)
	if err != nil {
		return "", err
	}
	return c.signAndSendSolanaTransaction(ctx, req.WalletID, txBase64)
}

type accountFlags struct {
	pubkey   []byte
	signer   bool
	writable bool
	order    int
}

// buildSponsoredTransaction compiles a legacy message: relayer first as fee payer, then
// signers before non-signers and writable before read-only, as the Solana wire format requires.
// Only the relayer and the wallet may sign; any other required signer is refused.
func buildSponsoredTransaction(req SponsoredInstructionsRequest, recentBlockhash []byte) (string, error) {
	relayerPriv, relayerPub, err := decodeSolanaKeypair(req.RelayerKey)
	if err != nil {
		return "", fmt.Errorf("%w: invalid relayer key: %v", ErrAPI, err)
	}
	walletPub, err := decodeBase58Pubkey(req.WalletAddress)
	if err != nil {
		return "", fmt.Errorf("%w: invalid wallet address: %v", ErrAPI, err)
	}
	if len(recentBlockhash) != ed25519.PublicKeySize {
		return "", fmt.Errorf("%w: invalid recent blockhash length %d", ErrAPI, len(recentBlockhash))
	}

	index := map[string]*accountFlags{}
	touch := func(pubkey []byte, signer, writable bool) {
		key := string(pubkey)
		flags, ok := index[key]
		if !ok {
			flags = &accountFlags{pubkey: pubkey, order: len(index)}
			index[key] = flags
		}
		flags.signer = flags.signer || signer
		flags.writable = flags.writable || writable
	}
	touch(relayerPub, true, true)

	type decodedInstruction struct {
		programID []byte
		accounts  [][]byte
		data      []byte
	}
	decoded := make([]decodedInstruction, 0, len(req.Instructions))
	for i, ix := range req.Instructions {
		programID, err := decodeBase58Pubkey(ix.ProgramID)
		if err != nil {
			return "", fmt.Errorf("%w: instruction %d program id: %v", ErrAPI, i, err)
		}
		entry := decodedInstruction{programID: programID, data: ix.Data}
		for j, account := range ix.Accounts {
			pubkey, err := decodeBase58Pubkey(account.Pubkey)
			if err != nil {
				return "", fmt.Errorf("%w: instruction %d account %d: %v", ErrAPI, i, j, err)
			}
			if account.IsSigner && string(pubkey) != string(relayerPub) && string(pubkey) != string(walletPub) {
				return "", fmt.Errorf("%w: instruction %d requires unknown signer %s", ErrAPI, i, account.Pubkey)
			}
			touch(pubkey, account.IsSigner, account.IsWritable)
			entry.accounts = append(entry.accounts, pubkey)
		}
		touch(programID, false, false)
		decoded = append(decoded, entry)
	}
	if len(index) > maxLegacyAccounts {
		return "", fmt.Errorf("%w: too many accounts (%d)", ErrAPI, len(index))
	}

	ordered := make([]*accountFlags, 0, len(index))
	for _, flags := range index {
		ordered = append(ordered, flags)
	}
	rank := func(f *accountFlags) int {
		switch {
		case f.signer && f.writable:
			return 0
		case f.signer:
			return 1
		case f.writable:
			return 2
		default:
			return 3
		}
	}
	// The relayer was touched first (order 0, signer+writable), so it stays the fee payer.
	sort.SliceStable(ordered, func(a, b int) bool {
		if rank(ordered[a]) != rank(ordered[b]) {
			return rank(ordered[a]) < rank(ordered[b])
		}
		return ordered[a].order < ordered[b].order
	})

	accounts := make([][]byte, len(ordered))
	position := make(map[string]byte, len(ordered))
	var signers, readonlySigned, readonlyUnsigned byte
	for i, flags := range ordered {
		accounts[i] = flags.pubkey
		position[string(flags.pubkey)] = byte(i)
		switch {
		case flags.signer:
			signers++
			if !flags.writable {
				readonlySigned++
			}
		case !flags.writable:
			readonlyUnsigned++
		}
	}

	compiled := make([]compiledInstruction, 0, len(decoded))
	for _, ix := range decoded {
		indexes := make([]byte, len(ix.accounts))
		for i, pubkey := range ix.accounts {
			indexes[i] = position[string(pubkey)]
		}
		compiled = append(compiled, compiledInstruction{
			programIDIndex: position[string(ix.programID)],
			accounts:       indexes,
			data:           ix.data,
		})
	}

	message := encodeLegacyMessage([]byte{signers, readonlySigned, readonlyUnsigned}, accounts, recentBlockhash, compiled)
	tx := encodeTransaction(message, make([][]byte, signers))
	signed, err := signTransaction(tx, relayerPriv, 0)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signed), nil
}
