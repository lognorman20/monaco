package privy

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math/big"
	"strconv"
)

const (
	usdcMintAddress      = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	solanaMainnetCAIP2   = "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
	solanaDummyBlockhash = "11111111111111111111111111111111"
	tokenProgramID       = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	associatedTokenProg  = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
	systemProgramID      = "11111111111111111111111111111111"
)

// SubmitSweep submits a server-signed USDC sweep with the relayer as SOL fee payer.
func (c *HTTPClient) SubmitSweep(ctx context.Context, req SweepRequest) (SweepResult, error) {
	if req.RelayerKey == "" {
		return SweepResult{}, fmt.Errorf("%w: relayer key required", ErrAPI)
	}
	if req.MemberAddress == "" || req.TreasuryAddress == "" {
		return SweepResult{}, fmt.Errorf("%w: missing addresses", ErrAPI)
	}
	if req.Amount <= 0 {
		return SweepResult{}, fmt.Errorf("%w: invalid amount", ErrAPI)
	}

	wallet, err := c.getWalletByAddress(ctx, req.MemberAddress)
	if err != nil {
		return SweepResult{}, err
	}

	txBase64, err := buildUSDCSweepTransaction(req)
	if err != nil {
		return SweepResult{}, err
	}

	hash, err := c.signAndSendSolanaTransaction(ctx, wallet.ID, txBase64)
	if err != nil {
		return SweepResult{}, err
	}

	return SweepResult{TxSignature: hash}, nil
}

// MemberUSDCBalance returns member wallet USDC balance via Privy + RPC.
func (c *HTTPClient) MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error) {
	return c.walletUSDCBalance(ctx, memberAddress)
}

// TreasuryUSDCBalance returns treasury wallet USDC balance via Privy + RPC.
func (c *HTTPClient) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	return c.walletUSDCBalance(ctx, treasuryAddress)
}

func (c *HTTPClient) walletUSDCBalance(ctx context.Context, address string) (int64, error) {
	if address == "" {
		return 0, fmt.Errorf("%w: missing wallet address", ErrAPI)
	}
	wallet, err := c.getWalletByAddress(ctx, address)
	if err != nil {
		return 0, err
	}
	return c.getWalletUSDCBalance(ctx, wallet.ID)
}

// BuildSweepRequest validates sweep inputs and attaches relayer fee payer key.
func BuildSweepRequest(memberAddress, treasuryAddress string, amount int64, relayerKey string) (SweepRequest, error) {
	if memberAddress == "" || treasuryAddress == "" {
		return SweepRequest{}, fmt.Errorf("member and treasury addresses are required")
	}
	if amount <= 0 {
		return SweepRequest{}, fmt.Errorf("amount must be positive")
	}
	if relayerKey == "" {
		return SweepRequest{}, fmt.Errorf("relayer key is required")
	}
	return SweepRequest{
		MemberAddress:   memberAddress,
		TreasuryAddress: treasuryAddress,
		Amount:          amount,
		RelayerKey:      relayerKey,
	}, nil
}

func parseRawTokenAmount(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	amount, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid balance %q", ErrAPI, raw)
	}
	return amount, nil
}

func buildUSDCSweepTransaction(req SweepRequest) (string, error) {
	relayerPriv, relayerPub, err := decodeSolanaKeypair(req.RelayerKey)
	if err != nil {
		return "", fmt.Errorf("%w: invalid relayer key: %v", ErrAPI, err)
	}

	memberPub, err := decodeBase58Pubkey(req.MemberAddress)
	if err != nil {
		return "", fmt.Errorf("%w: invalid member address: %v", ErrAPI, err)
	}
	treasuryPub, err := decodeBase58Pubkey(req.TreasuryAddress)
	if err != nil {
		return "", fmt.Errorf("%w: invalid treasury address: %v", ErrAPI, err)
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
	blockhash, err := decodeBase58Pubkey(solanaDummyBlockhash)
	if err != nil {
		return "", err
	}

	sourceATA, err := findAssociatedTokenAddress(memberPub, mintPub, tokenProgram, ataProgram)
	if err != nil {
		return "", err
	}
	destATA, err := findAssociatedTokenAddress(treasuryPub, mintPub, tokenProgram, ataProgram)
	if err != nil {
		return "", err
	}

	accounts := [][]byte{
		relayerPub,
		memberPub,
		sourceATA,
		destATA,
		treasuryPub,
		mintPub,
		systemProgram,
		tokenProgram,
		ataProgram,
	}

	createATA := compiledInstruction{
		programIDIndex: 8,
		accounts:       []byte{0, 3, 4, 5, 6, 7, 8},
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
		blockhash,
		[]compiledInstruction{createATA, transfer},
	)

	tx := encodeTransaction(message, [][]byte{relayerPriv, nil})
	signed, err := signTransaction(tx, relayerPriv, 0)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(signed), nil
}

type compiledInstruction struct {
	programIDIndex byte
	accounts       []byte
	data           []byte
}

func encodeLegacyMessage(header []byte, accounts [][]byte, blockhash []byte, instructions []compiledInstruction) []byte {
	var out []byte
	out = append(out, header...)
	out = append(out, encodeCompactU16(len(accounts))...)
	for _, account := range accounts {
		out = append(out, account...)
	}
	out = append(out, blockhash...)
	out = append(out, encodeCompactU16(len(instructions))...)
	for _, ix := range instructions {
		out = append(out, ix.programIDIndex)
		out = append(out, encodeCompactU16(len(ix.accounts))...)
		out = append(out, ix.accounts...)
		out = append(out, encodeCompactU16(len(ix.data))...)
		out = append(out, ix.data...)
	}
	return out
}

func encodeTransaction(message []byte, signers [][]byte) []byte {
	var out []byte
	out = append(out, encodeCompactU16(len(signers))...)
	for _, signer := range signers {
		if signer == nil {
			out = append(out, make([]byte, ed25519.SignatureSize)...)
			continue
		}
		out = append(out, make([]byte, ed25519.SignatureSize)...)
	}
	out = append(out, message...)
	return out
}

func signTransaction(tx []byte, signer ed25519.PrivateKey, signerIndex int) ([]byte, error) {
	sigCount, offset, err := decodeCompactU16(tx)
	if err != nil {
		return nil, err
	}
	offset += sigCount * ed25519.SignatureSize
	if offset > len(tx) {
		return nil, fmt.Errorf("transaction missing message")
	}
	message := tx[offset:]

	signature := ed25519.Sign(signer, message)
	sigOffset, err := compactU16Size(sigCount)
	if err != nil {
		return nil, err
	}
	sigOffset += signerIndex * ed25519.SignatureSize
	if sigOffset+ed25519.SignatureSize > len(tx) {
		return nil, fmt.Errorf("signer index out of range")
	}
	copy(tx[sigOffset:sigOffset+ed25519.SignatureSize], signature)
	return tx, nil
}

func compactU16Size(value int) (int, error) {
	encoded := encodeCompactU16(value)
	return len(encoded), nil
}

func decodeCompactU16(data []byte) (int, int, error) {
	if len(data) == 0 {
		return 0, 0, fmt.Errorf("empty compact-u16")
	}
	size := int(data[0])
	if size < 0x80 {
		return size, 1, nil
	}
	if len(data) < 2 {
		return 0, 0, fmt.Errorf("truncated compact-u16")
	}
	size = int(data[0]&0x7f) | int(data[1])<<7
	return size, 2, nil
}

func encodeCompactU16(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	return []byte{byte(n&0x7f | 0x80), byte(n >> 7)}
}

func findAssociatedTokenAddress(owner, mint, tokenProgram, ataProgram []byte) ([]byte, error) {
	seeds := [][]byte{owner, tokenProgram, mint}
	return findProgramAddress(seeds, ataProgram)
}

func findProgramAddress(seeds [][]byte, programID []byte) ([]byte, error) {
	for bump := 255; bump >= 0; bump-- {
		var preimage []byte
		for _, seed := range seeds {
			preimage = append(preimage, seed...)
		}
		preimage = append(preimage, byte(bump))
		preimage = append(preimage, programID...)
		preimage = append(preimage, []byte("ProgramDerivedAddress")...)

		sum := sha256.Sum256(preimage)
		if !isOnCurve(sum[:]) {
			return sum[:], nil
		}
	}
	return nil, fmt.Errorf("unable to find program address")
}

func isOnCurve(pubkey []byte) bool {
	if len(pubkey) != ed25519.PublicKeySize {
		return false
	}
	// Off-curve PDAs fail Edwards-y decompression; on-curve keys succeed.
	return decompressEdwardsY(pubkey)
}

func decodeSolanaKeypair(encoded string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	raw, err := decodeBase58(encoded)
	if err != nil {
		return nil, nil, err
	}
	switch len(raw) {
	case ed25519.SeedSize:
		priv := ed25519.NewKeyFromSeed(raw)
		return priv, priv.Public().(ed25519.PublicKey), nil
	case ed25519.PrivateKeySize:
		priv := ed25519.PrivateKey(raw)
		return priv, priv.Public().(ed25519.PublicKey), nil
	default:
		return nil, nil, fmt.Errorf("unexpected key length %d", len(raw))
	}
}

func decodeBase58Pubkey(encoded string) ([]byte, error) {
	raw, err := decodeBase58(encoded)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("unexpected pubkey length %d", len(raw))
	}
	return raw, nil
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func decodeBase58(input string) ([]byte, error) {
	zeros := 0
	for zeros < len(input) && input[zeros] == '1' {
		zeros++
	}

	size := (len(input)*733/1000) + 1
	buf := make([]byte, size)
	for _, r := range input {
		val := int8(-1)
		for i := 0; i < len(base58Alphabet); i++ {
			if base58Alphabet[i] == byte(r) {
				val = int8(i)
				break
			}
		}
		if val < 0 {
			return nil, fmt.Errorf("invalid base58 character %q", r)
		}

		carry := int(val)
		for i := len(buf) - 1; i >= 0; i-- {
			carry += 58 * int(buf[i])
			buf[i] = byte(carry & 0xff)
			carry >>= 8
		}
		if carry != 0 {
			return nil, fmt.Errorf("base58 overflow")
		}
	}

	start := 0
	for start < len(buf) && buf[start] == 0 {
		start++
	}

	out := make([]byte, zeros+len(buf)-start)
	copy(out[zeros:], buf[start:])
	return out, nil
}

func decompressEdwardsY(pubkey []byte) bool {
	p := new(big.Int).SetBytes(reverseBytes(pubkey))
	if p.Cmp(ed25519FieldModulus()) >= 0 {
		return false
	}

	y2 := new(big.Int).Mul(p, p)
	y2.Mod(y2, ed25519FieldModulus())

	d := new(big.Int).Mul(big.NewInt(121665), y2)
	d.Mod(d, ed25519FieldModulus())
	d.Add(d, big.NewInt(1))
	d.Mod(d, ed25519FieldModulus())

	u := new(big.Int).Sub(y2, big.NewInt(1))
	u.Mod(u, ed25519FieldModulus())

	return hasSquareRoot(u, ed25519FieldModulus()) && hasSquareRoot(d, ed25519FieldModulus())
}

var ed25519FieldModulusValue = func() *big.Int {
	modulus, _ := new(big.Int).SetString("7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffed", 16)
	return modulus
}()

func ed25519FieldModulus() *big.Int {
	return ed25519FieldModulusValue
}

func reverseBytes(in []byte) []byte {
	out := make([]byte, len(in))
	for i := range in {
		out[i] = in[len(in)-1-i]
	}
	return out
}

func hasSquareRoot(n, p *big.Int) bool {
	if n.Sign() == 0 {
		return true
	}
	exp := new(big.Int).Sub(p, big.NewInt(1))
	exp.Rsh(exp, 1)
	result := new(big.Int).Exp(n, exp, p)
	return result.Cmp(big.NewInt(1)) == 0
}

func encodeBase58(input []byte) string {
	if len(input) == 0 {
		return ""
	}

	zeros := 0
	for zeros < len(input) && input[zeros] == 0 {
		zeros++
	}

	size := len(input)*138/100 + 1
	buf := make([]byte, size)
	start := size
	for _, b := range input {
		carry := int(b)
		for i := size - 1; i >= start; i-- {
			carry += 256 * int(buf[i])
			buf[i] = byte(carry % 58)
			carry /= 58
		}
		for carry > 0 {
			start--
			buf[start] = byte(carry % 58)
			carry /= 58
		}
	}

	for i := start; i < size && buf[i] == 0; i++ {
		zeros++
	}

	out := make([]byte, zeros+size-start)
	for i := 0; i < zeros; i++ {
		out[i] = '1'
	}
	for i := start; i < size; i++ {
		out[zeros+i-start] = base58Alphabet[buf[i]]
	}
	return string(out)
}
