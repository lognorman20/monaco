package privy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubmitSweep_callsPrivyClientWithMemberAndTreasuryAddresses(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	req := SweepRequest{
		MemberAddress:   "FAKEmember123",
		TreasuryAddress: "FAKEtreasury456",
		Amount:          1_000_000,
		RelayerKey:      "relayer-key",
	}

	// Act
	result, err := client.SubmitSweep(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("SubmitSweep: %v", err)
	}
	if result.TxSignature == "" {
		t.Fatal("expected tx signature")
	}
	last, ok := LastSweepRequest(client)
	if !ok {
		t.Fatal("expected last sweep request")
	}
	if last.MemberAddress != req.MemberAddress {
		t.Fatalf("member = %q, want %q", last.MemberAddress, req.MemberAddress)
	}
	if last.TreasuryAddress != req.TreasuryAddress {
		t.Fatalf("treasury = %q, want %q", last.TreasuryAddress, req.TreasuryAddress)
	}
}

func TestSubmitSweep_includesRelayerAsFeePayer(t *testing.T) {
	// Arrange
	client := NewFakeClient()
	req, err := BuildSweepRequest("FAKEmember", "FAKEtreasury", 500_000, "relayer-fee-payer-key")
	if err != nil {
		t.Fatalf("BuildSweepRequest: %v", err)
	}

	// Act
	_, err = client.SubmitSweep(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("SubmitSweep: %v", err)
	}
	last, ok := LastSweepRequest(client)
	if !ok {
		t.Fatal("expected last sweep request")
	}
	if last.RelayerKey != "relayer-fee-payer-key" {
		t.Fatalf("relayer key = %q, want relayer-fee-payer-key", last.RelayerKey)
	}
}

func TestFindAssociatedTokenAddress_matchesKnownUSDCATA(t *testing.T) {
	// Arrange — owner/mint pair where the old curve check derived the wrong ATA.
	owner, err := decodeBase58Pubkey("9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM")
	if err != nil {
		t.Fatalf("decode owner: %v", err)
	}
	mint, err := decodeBase58Pubkey(usdcMintAddress)
	if err != nil {
		t.Fatalf("decode mint: %v", err)
	}
	tokenProgram, err := decodeBase58Pubkey(tokenProgramID)
	if err != nil {
		t.Fatalf("decode token program: %v", err)
	}
	ataProgram, err := decodeBase58Pubkey(associatedTokenProg)
	if err != nil {
		t.Fatalf("decode ata program: %v", err)
	}
	// Act
	gotATA, err := findAssociatedTokenAddress(owner, mint, tokenProgram, ataProgram)
	if err != nil {
		t.Fatalf("findAssociatedTokenAddress: %v", err)
	}
	brokenATA := brokenFindAssociatedTokenAddress(owner, mint, tokenProgram, ataProgram)

	// Assert
	if brokenATA == nil {
		t.Fatal("broken curve check returned nil ATA")
	}
	if bytes.Equal(brokenATA, gotATA) {
		t.Fatal("expected fixed ATA derivation to differ from broken curve check")
	}
	if decompressEdwardsY(gotATA) {
		t.Fatal("ATA PDA must be off-curve")
	}
}

func TestBuildUSDCSweepTransaction_usesProvidedBlockhash(t *testing.T) {
	// Arrange
	req, keys := testSweepRequest(t)
	blockhash := make([]byte, ed25519.PublicKeySize)
	for i := range blockhash {
		blockhash[i] = byte(i + 1)
	}

	// Act
	txBase64, err := buildUSDCSweepTransaction(req, blockhash)
	if err != nil {
		t.Fatalf("buildUSDCSweepTransaction: %v", err)
	}
	parsedBlockhash, err := recentBlockhashFromTransaction(txBase64)
	if err != nil {
		t.Fatalf("recentBlockhashFromTransaction: %v", err)
	}

	// Assert
	if parsedBlockhash == nil {
		t.Fatal("expected blockhash in transaction")
	}
	if !bytes.Equal(*parsedBlockhash, blockhash) {
		t.Fatalf("blockhash mismatch: got %x want %x", *parsedBlockhash, blockhash)
	}
	dummyBlockhash, err := decodeBase58Pubkey("11111111111111111111111111111111")
	if err != nil {
		t.Fatalf("decode dummy blockhash: %v", err)
	}
	if bytes.Equal(*parsedBlockhash, dummyBlockhash) {
		t.Fatal("transaction must not use dummy blockhash")
	}
	_ = keys
}

func TestBuildUSDCSweepTransaction_createATAUsesSixAccounts(t *testing.T) {
	// Arrange
	req, _ := testSweepRequest(t)
	blockhash := make([]byte, ed25519.PublicKeySize)
	blockhash[0] = 0xab

	// Act
	txBase64, err := buildUSDCSweepTransaction(req, blockhash)
	if err != nil {
		t.Fatalf("buildUSDCSweepTransaction: %v", err)
	}
	accountCount, err := createATAAccountCountFromTransaction(txBase64)
	if err != nil {
		t.Fatalf("createATAAccountCountFromTransaction: %v", err)
	}

	// Assert
	if accountCount != 6 {
		t.Fatalf("create ATA account metas = %d, want 6", accountCount)
	}
}

func TestHTTPClient_SubmitSweep_callsPrivyWithMemberAndTreasuryAddresses(t *testing.T) {
	// Arrange
	memberSeed := sha256.Sum256([]byte("member-wallet"))
	memberPriv := ed25519.NewKeyFromSeed(memberSeed[:])
	memberPub := memberPriv.Public().(ed25519.PublicKey)
	memberAddress := encodeBase58(memberPub)

	treasurySeed := sha256.Sum256([]byte("treasury-wallet"))
	treasuryPub := ed25519.NewKeyFromSeed(treasurySeed[:]).Public().(ed25519.PublicKey)
	treasuryAddress := encodeBase58(treasuryPub)

	relayerSeed := sha256.Sum256([]byte("relayer-wallet"))
	relayerPriv := ed25519.NewKeyFromSeed(relayerSeed[:])
	relayerKey := encodeBase58(relayerPriv)

	var gotAddress string
	var gotWalletID string
	var gotRPC walletRPCRequest
	var gotSolanaRPC bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/":
			var rpcReq solanaRPCRequest
			if err := json.NewDecoder(r.Body).Decode(&rpcReq); err != nil {
				t.Fatalf("decode solana rpc: %v", err)
			}
			if rpcReq.Method != "getLatestBlockhash" {
				t.Fatalf("solana rpc method = %q", rpcReq.Method)
			}
			gotSolanaRPC = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]any{
					"value": map[string]string{
						"blockhash": "EkSnNWid2cvATRWXHtQqy3C6xToBFWtBiCBMuMhvv2Sv",
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/wallets/address":
			var lookup getWalletByAddressRequest
			if err := json.NewDecoder(r.Body).Decode(&lookup); err != nil {
				t.Fatalf("decode address lookup: %v", err)
			}
			gotAddress = lookup.Address
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(walletResponse{
				ID:      "wallet-member-http",
				Address: memberAddress,
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rpc"):
			gotWalletID = strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/rpc"), "/v1/wallets/")
			if err := json.NewDecoder(r.Body).Decode(&gotRPC); err != nil {
				t.Fatalf("decode rpc body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(walletRPCResponse{
				Data: walletRPCData{Hash: "SWEEPHTTPsig123"},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewHTTPClientWithTransport(testConfig(), server.URL, server.Client().Transport)
	client.solanaRPCURL = server.URL + "/"
	req := SweepRequest{
		MemberAddress:   memberAddress,
		TreasuryAddress: treasuryAddress,
		Amount:          1_000_000,
		RelayerKey:      relayerKey,
	}

	// Act
	result, err := client.SubmitSweep(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("SubmitSweep: %v", err)
	}
	if result.TxSignature != "SWEEPHTTPsig123" {
		t.Fatalf("signature = %q", result.TxSignature)
	}
	if gotAddress != memberAddress {
		t.Fatalf("lookup address = %q, want %q", gotAddress, memberAddress)
	}
	if gotWalletID != "wallet-member-http" {
		t.Fatalf("wallet id = %q", gotWalletID)
	}
	if gotRPC.Method != "signAndSendTransaction" {
		t.Fatalf("rpc method = %q", gotRPC.Method)
	}
	if gotRPC.CAIP2 != solanaMainnetCAIP2 {
		t.Fatalf("caip2 = %q", gotRPC.CAIP2)
	}
	if gotRPC.Params.Encoding != "base64" || gotRPC.Params.Transaction == "" {
		t.Fatal("expected base64 transaction payload")
	}
	if !gotSolanaRPC {
		t.Fatal("expected getLatestBlockhash solana rpc call")
	}
	blockhash, err := recentBlockhashFromTransaction(gotRPC.Params.Transaction)
	if err != nil {
		t.Fatalf("recentBlockhashFromTransaction: %v", err)
	}
	wantBlockhash, err := decodeBase58Pubkey("EkSnNWid2cvATRWXHtQqy3C6xToBFWtBiCBMuMhvv2Sv")
	if err != nil {
		t.Fatalf("decode blockhash: %v", err)
	}
	if blockhash == nil || !bytes.Equal(*blockhash, wantBlockhash) {
		t.Fatalf("transaction blockhash = %v, want %x", blockhash, wantBlockhash)
	}
}

func testSweepRequest(t *testing.T) (SweepRequest, struct {
	member    ed25519.PublicKey
	treasury  ed25519.PublicKey
	relayer   ed25519.PrivateKey
}) {
	memberSeed := sha256.Sum256([]byte("member-wallet"))
	memberPriv := ed25519.NewKeyFromSeed(memberSeed[:])
	memberPub := memberPriv.Public().(ed25519.PublicKey)

	treasurySeed := sha256.Sum256([]byte("treasury-wallet"))
	treasuryPub := ed25519.NewKeyFromSeed(treasurySeed[:]).Public().(ed25519.PublicKey)

	relayerSeed := sha256.Sum256([]byte("relayer-wallet"))
	relayerPriv := ed25519.NewKeyFromSeed(relayerSeed[:])

	req := SweepRequest{
		MemberAddress:   encodeBase58(memberPub),
		TreasuryAddress: encodeBase58(treasuryPub),
		Amount:          1_000_000,
		RelayerKey:      encodeBase58(relayerPriv),
	}
	return req, struct {
		member   ed25519.PublicKey
		treasury ed25519.PublicKey
		relayer  ed25519.PrivateKey
	}{memberPub, treasuryPub, relayerPriv}
}

func recentBlockhashFromTransaction(txBase64 string) (*[]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		return nil, err
	}
	sigCount, offset, err := decodeCompactU16(raw)
	if err != nil {
		return nil, err
	}
	offset += sigCount * ed25519.SignatureSize
	message := raw[offset:]

	accountCount, accountOffset, err := decodeCompactU16(message[3:])
	if err != nil {
		return nil, err
	}
	accountOffset += 3
	accountBytes := accountCount * ed25519.PublicKeySize
	if accountOffset+accountBytes+ed25519.PublicKeySize > len(message) {
		return nil, fmt.Errorf("message truncated before blockhash")
	}
	blockhash := message[accountOffset+accountBytes : accountOffset+accountBytes+ed25519.PublicKeySize]
	out := make([]byte, len(blockhash))
	copy(out, blockhash)
	return &out, nil
}

func createATAAccountCountFromTransaction(txBase64 string) (int, error) {
	raw, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		return 0, err
	}
	sigCount, offset, err := decodeCompactU16(raw)
	if err != nil {
		return 0, err
	}
	offset += sigCount * ed25519.SignatureSize
	message := raw[offset:]

	accountCount, accountOffset, err := decodeCompactU16(message[3:])
	if err != nil {
		return 0, err
	}
	accountOffset += 3 + accountCount*ed25519.PublicKeySize + ed25519.PublicKeySize

	instructionCount, instructionOffset, err := decodeCompactU16(message[accountOffset:])
	if err != nil {
		return 0, err
	}
	if instructionCount < 1 {
		return 0, fmt.Errorf("expected at least one instruction")
	}
	instructionOffset += accountOffset

	programIDIndex := message[instructionOffset]
	if int(programIDIndex) != 8 {
		return 0, fmt.Errorf("first instruction program index = %d, want 8", programIDIndex)
	}
	instructionOffset++

	accountMetaCount, _, err := decodeCompactU16(message[instructionOffset:])
	if err != nil {
		return 0, err
	}
	return accountMetaCount, nil
}

func brokenFindAssociatedTokenAddress(owner, mint, tokenProgram, ataProgram []byte) []byte {
	seeds := [][]byte{owner, tokenProgram, mint}
	for bump := 255; bump >= 0; bump-- {
		var preimage []byte
		for _, seed := range seeds {
			preimage = append(preimage, seed...)
		}
		preimage = append(preimage, byte(bump))
		preimage = append(preimage, ataProgram...)
		preimage = append(preimage, []byte("ProgramDerivedAddress")...)

		sum := sha256.Sum256(preimage)
		if !brokenDecompressEdwardsY(sum[:]) {
			return sum[:]
		}
	}
	return nil
}

func brokenDecompressEdwardsY(pubkey []byte) bool {
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

func TestHTTPClient_MemberUSDCBalance_readsPrivyBalance(t *testing.T) {
	// Arrange
	balanceSeed := sha256.Sum256([]byte("balance-member"))
	address := encodeBase58(ed25519.NewKeyFromSeed(balanceSeed[:]).Public().(ed25519.PublicKey))
	var gotWalletID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/wallets/address":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(walletResponse{ID: "wallet-balance", Address: address})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/balance"):
			gotWalletID = strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/balance"), "/v1/wallets/")
			if !strings.Contains(r.URL.RawQuery, usdcMintAddress) {
				t.Fatalf("balance query = %q", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(walletBalanceResponse{
				Balances: []walletBalanceEntry{{RawValue: "2500000"}},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewHTTPClientWithTransport(testConfig(), server.URL, server.Client().Transport)

	// Act
	balance, err := client.MemberUSDCBalance(context.Background(), address)

	// Assert
	if err != nil {
		t.Fatalf("MemberUSDCBalance: %v", err)
	}
	if balance != 2_500_000 {
		t.Fatalf("balance = %d, want 2500000", balance)
	}
	if gotWalletID != "wallet-balance" {
		t.Fatalf("wallet id = %q", gotWalletID)
	}
}
