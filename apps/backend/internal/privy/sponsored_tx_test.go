package privy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/solana/txsign"
)

// flashSetupInstructions mirrors a Flash first-trade setup: a relayer-paid Token-2022
// create-associated-token-account, then an SPL Approve signed by the treasury.
func flashSetupInstructions(relayer, treasury string) []Instruction {
	const (
		xStockMint = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"
		xStockATA  = "DK2ZeJzewxjESATTTwQwh7GNx3QxhWbbLpqcgPVGryot"
		usdcATA    = "FGETo8T8wMcN2wCjav8VK6eh3dLk63evNDPxzLSJra8B"
		delegate   = "3jBWeQrnfEhw5LY53hwcoYKQJsrTUbtLbmGrNYCr1Fiq"
	)
	return []Instruction{
		{
			ProgramID: associatedTokenProg,
			Accounts: []InstructionAccount{
				{Pubkey: relayer, IsSigner: true, IsWritable: true},
				{Pubkey: xStockATA, IsWritable: true},
				{Pubkey: treasury},
				{Pubkey: xStockMint},
				{Pubkey: systemProgramID},
				{Pubkey: token2022ProgramID},
			},
			Data: []byte{1},
		},
		{
			ProgramID: tokenProgramID,
			Accounts: []InstructionAccount{
				{Pubkey: usdcATA, IsWritable: true},
				{Pubkey: delegate},
				{Pubkey: treasury, IsSigner: true},
			},
			Data: []byte{4, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		},
	}
}

func TestBuildSponsoredTransaction_relayerPaysAndTreasuryCoSigns(t *testing.T) {
	// Arrange
	sweep, keys := testSweepRequest(t)
	relayerPub := keys.relayer.Public().(ed25519.PublicKey)
	req := SponsoredInstructionsRequest{
		WalletID:      "wallet-treasury",
		WalletAddress: sweep.TreasuryAddress,
		RelayerKey:    sweep.RelayerKey,
		Instructions:  flashSetupInstructions(encodeBase58(relayerPub), sweep.TreasuryAddress),
	}
	blockhash := bytes.Repeat([]byte{9}, 32)

	// Act
	txBase64, err := buildSponsoredTransaction(req, blockhash)

	// Assert
	if err != nil {
		t.Fatalf("buildSponsoredTransaction: %v", err)
	}
	tx, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		t.Fatalf("decode tx: %v", err)
	}
	signers, err := txsign.RequiredSignerPubkeys(tx)
	if err != nil {
		t.Fatalf("required signers: %v", err)
	}
	if len(signers) != 2 || !bytes.Equal(signers[0], relayerPub) || !bytes.Equal(signers[1], keys.treasury) {
		t.Fatalf("signers = %x, want [relayer, treasury]", signers)
	}
	if ok, _ := txsign.HasSignature(tx, 0); !ok {
		t.Fatal("expected relayer fee payer signature at index 0")
	}
	if ok, _ := txsign.HasSignature(tx, 1); ok {
		t.Fatal("treasury slot must be left for Privy to sign")
	}
	message, err := txsign.TransactionMessage(tx)
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	// Header: relayer (writable) and treasury (read-only) are the only required signers.
	if message[0] != 2 || message[1] != 1 {
		t.Fatalf("header = %v, want 2 signers with 1 read-only", message[:3])
	}
	tx[1] ^= 0xff
	if ed25519.Verify(relayerPub, message, tx[1:65]) {
		t.Fatal("sanity: corrupted signature must not verify")
	}
	tx[1] ^= 0xff
	if !ed25519.Verify(relayerPub, message, tx[1:65]) {
		t.Fatal("relayer signature does not verify over the compiled message")
	}
}

func TestBuildSponsoredTransaction_unknownSigner_isRefused(t *testing.T) {
	// Arrange
	sweep, keys := testSweepRequest(t)
	instructions := flashSetupInstructions(encodeBase58(keys.relayer.Public().(ed25519.PublicKey)), sweep.TreasuryAddress)
	instructions[1].Accounts[2].Pubkey = sweep.MemberAddress

	// Act
	_, err := buildSponsoredTransaction(SponsoredInstructionsRequest{
		WalletID:      "wallet-treasury",
		WalletAddress: sweep.TreasuryAddress,
		RelayerKey:    sweep.RelayerKey,
		Instructions:  instructions,
	}, bytes.Repeat([]byte{9}, 32))

	// Assert
	if !errors.Is(err, ErrAPI) || !strings.Contains(err.Error(), "unknown signer") {
		t.Fatalf("error = %v, want unknown signer refusal", err)
	}
}

func TestSubmitSponsoredInstructions_validatesInput(t *testing.T) {
	client := NewHTTPClientWithTransport(testConfig(), "http://privy.invalid", http.DefaultTransport)

	cases := map[string]SponsoredInstructionsRequest{
		"no relayer":      {WalletID: "w", WalletAddress: "a", Instructions: []Instruction{{}}},
		"no wallet":       {RelayerKey: "k", Instructions: []Instruction{{}}},
		"no instructions": {RelayerKey: "k", WalletID: "w", WalletAddress: "a"},
	}
	for name, req := range cases {
		if _, err := client.SubmitSponsoredInstructions(context.Background(), req); !errors.Is(err, ErrAPI) {
			t.Fatalf("%s: error = %v, want ErrAPI", name, err)
		}
	}
}

func TestHTTPClient_SignSolanaMessage_returnsRawSignature(t *testing.T) {
	// Arrange
	want := bytes.Repeat([]byte{0xab}, ed25519.SignatureSize)
	var gotBody signMessageRPCRequest
	var gotPath, gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get(authorizationSignatureHeader)
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"method":"signMessage","data":{"signature":"` + base64.StdEncoding.EncodeToString(want) + `","encoding":"base64"}}`))
	}))
	defer server.Close()
	cfg := testConfig()
	cfg.PrivyAuthorizationPrivateKey = testAuthorizationPrivateKey(t)
	client := NewHTTPClientWithTransport(cfg, server.URL, server.Client().Transport)
	message := []byte("DFS|m=EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v|t=5000000|h=abc")

	// Act
	signature, err := client.SignSolanaMessage(context.Background(), "wallet-treasury", message)

	// Assert
	if err != nil {
		t.Fatalf("SignSolanaMessage: %v", err)
	}
	if !bytes.Equal(signature, want) {
		t.Fatalf("signature = %x", signature)
	}
	if gotPath != "/v1/wallets/wallet-treasury/rpc" || gotAuthorization == "" {
		t.Fatalf("path = %q authorization = %q", gotPath, gotAuthorization)
	}
	if gotBody.Method != "signMessage" || gotBody.ChainType != "solana" || gotBody.Params.Encoding != "base64" {
		t.Fatalf("unexpected rpc body %+v", gotBody)
	}
	if decoded, _ := base64.StdEncoding.DecodeString(gotBody.Params.Message); !bytes.Equal(decoded, message) {
		t.Fatalf("message = %q", decoded)
	}
}

func TestHTTPClient_SignSolanaMessage_unhappyResponses(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"non-200":         {http.StatusUnauthorized, `{"error":"invalid authorization signature"}`},
		"malformed json":  {http.StatusOK, `{"data":`},
		"missing sig":     {http.StatusOK, `{"data":{}}`},
		"not base64":      {http.StatusOK, `{"data":{"signature":"***"}}`},
		"short signature": {http.StatusOK, `{"data":{"signature":"` + base64.StdEncoding.EncodeToString([]byte("short")) + `"}}`},
	}
	for name, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		cfg := testConfig()
		cfg.PrivyAuthorizationPrivateKey = testAuthorizationPrivateKey(t)
		client := NewHTTPClientWithTransport(cfg, server.URL, server.Client().Transport)

		_, err := client.SignSolanaMessage(context.Background(), "wallet-treasury", []byte("msg"))

		server.Close()
		if !errors.Is(err, ErrAPI) {
			t.Fatalf("%s: error = %v, want ErrAPI", name, err)
		}
	}
}
