package treasury

// Solana wire encoding for the treasury USDC payout. Ported from the pre-Dynamic Privy client
// (archive/main-before-dynamic apps/backend/internal/privy/sweep.go) so the transaction layout
// matches what already ran against mainnet.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
)

const (
	// USDCMint is Circle's SPL USDC mint on Solana mainnet.
	USDCMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	tokenProgramID      = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	associatedTokenProg = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
	systemProgramID     = "11111111111111111111111111111111"
)

// ValidateAddress reports whether address decodes to a 32-byte Solana public key.
func ValidateAddress(address string) error {
	if _, err := decodeBase58Pubkey(address); err != nil {
		return fmt.Errorf("invalid solana address: %w", err)
	}
	return nil
}

// USDCTokenAccount returns the associated USDC token account for owner.
func USDCTokenAccount(owner string) (string, error) {
	ownerPub, err := decodeBase58Pubkey(owner)
	if err != nil {
		return "", err
	}
	mintPub, tokenProgram, ataProgram, err := usdcPrograms()
	if err != nil {
		return "", err
	}
	ata, err := findAssociatedTokenAddress(ownerPub, mintPub, tokenProgram, ataProgram)
	if err != nil {
		return "", err
	}
	return solanakey.EncodeBase58(ata), nil
}

func usdcPrograms() (mint, tokenProgram, ataProgram []byte, err error) {
	if mint, err = decodeBase58Pubkey(USDCMint); err != nil {
		return nil, nil, nil, err
	}
	if tokenProgram, err = decodeBase58Pubkey(tokenProgramID); err != nil {
		return nil, nil, nil, err
	}
	if ataProgram, err = decodeBase58Pubkey(associatedTokenProg); err != nil {
		return nil, nil, nil, err
	}
	return mint, tokenProgram, ataProgram, nil
}

// feePayerSignature returns the base58 transaction id of a fee-payer signed transaction:
// the first signature slot, which the payout builder fills with the relayer signature.
func feePayerSignature(txBase64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		return "", fmt.Errorf("%w: decode transaction: %v", ErrAPI, err)
	}
	sigCount, offset, err := decodeCompactU16(raw)
	if err != nil {
		return "", fmt.Errorf("%w: transaction signatures: %v", ErrAPI, err)
	}
	if sigCount == 0 || offset+ed25519.SignatureSize > len(raw) {
		return "", fmt.Errorf("%w: transaction has no fee payer signature", ErrAPI)
	}
	return solanakey.EncodeBase58(raw[offset : offset+ed25519.SignatureSize]), nil
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

// encodeTransaction reserves one empty signature slot per signer ahead of the message.
func encodeTransaction(message []byte, signerCount int) []byte {
	var out []byte
	out = append(out, encodeCompactU16(signerCount)...)
	out = append(out, make([]byte, signerCount*ed25519.SignatureSize)...)
	out = append(out, message...)
	return out
}

func signTransaction(tx []byte, signer ed25519.PrivateKey, signerIndex int) ([]byte, error) {
	sigCount, offset, err := decodeCompactU16(tx)
	if err != nil {
		return nil, err
	}
	sigStart := offset
	offset += sigCount * ed25519.SignatureSize
	if offset > len(tx) {
		return nil, fmt.Errorf("transaction missing message")
	}
	message := tx[offset:]

	signature := ed25519.Sign(signer, message)
	sigOffset := sigStart + signerIndex*ed25519.SignatureSize
	if signerIndex >= sigCount || sigOffset+ed25519.SignatureSize > len(tx) {
		return nil, fmt.Errorf("signer index out of range")
	}
	copy(tx[sigOffset:sigOffset+ed25519.SignatureSize], signature)
	return tx, nil
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
	return findProgramAddress([][]byte{owner, tokenProgram, mint}, ataProgram)
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

// isOnCurve reports whether pubkey decompresses to an Edwards point. PDAs must be off-curve.
func isOnCurve(pubkey []byte) bool {
	if len(pubkey) != ed25519.PublicKeySize {
		return false
	}
	yBytes := make([]byte, len(pubkey))
	copy(yBytes, pubkey)
	yBytes[31] &= 0x7f

	p := ed25519FieldModulus
	y := new(big.Int).SetBytes(reverseBytes(yBytes))
	if y.Cmp(p) >= 0 {
		return false
	}

	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, p)

	u := new(big.Int).Sub(y2, big.NewInt(1))
	u.Mod(u, p)

	v := new(big.Int).Mul(ed25519CurveD, y2)
	v.Mod(v, p)
	v.Add(v, big.NewInt(1))
	v.Mod(v, p)

	vInv := new(big.Int).ModInverse(v, p)
	if vInv == nil {
		return false
	}
	x2 := new(big.Int).Mul(u, vInv)
	x2.Mod(x2, p)

	if x2.Sign() == 0 {
		return true
	}
	exp := new(big.Int).Sub(p, big.NewInt(1))
	exp.Rsh(exp, 1)
	return new(big.Int).Exp(x2, exp, p).Cmp(big.NewInt(1)) == 0
}

var ed25519FieldModulus = func() *big.Int {
	modulus, _ := new(big.Int).SetString("7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffed", 16)
	return modulus
}()

var ed25519CurveD = func() *big.Int {
	p := ed25519FieldModulus
	num := new(big.Int).SetInt64(-121665)
	num.Mod(num, p)
	denInv := new(big.Int).ModInverse(big.NewInt(121666), p)
	d := new(big.Int).Mul(num, denInv)
	d.Mod(d, p)
	return d
}()

func reverseBytes(in []byte) []byte {
	out := make([]byte, len(in))
	for i := range in {
		out[i] = in[len(in)-1-i]
	}
	return out
}

func decodeSolanaKeypair(encoded string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	normalized, err := solanakey.ParsePrivateKey(encoded)
	if err != nil {
		return nil, nil, err
	}
	raw, err := solanakey.DecodeBase58(normalized)
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
	raw, err := solanakey.DecodeBase58(encoded)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("unexpected pubkey length %d", len(raw))
	}
	return raw, nil
}
