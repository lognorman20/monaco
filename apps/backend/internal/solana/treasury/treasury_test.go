package treasury

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
)

const (
	knownOwner   = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"
	knownUSDCATA = "FGETo8T8wMcN2wCjav8VK6eh3dLk63evNDPxzLSJra8B" // verified with solders find_program_address
)

func TestUSDCTokenAccount_matchesKnownATA(t *testing.T) {
	got, err := USDCTokenAccount(knownOwner)
	if err != nil {
		t.Fatalf("USDCTokenAccount: %v", err)
	}
	if got != knownUSDCATA {
		t.Fatalf("ata = %s, want %s", got, knownUSDCATA)
	}
}

func TestValidateAddress(t *testing.T) {
	if err := ValidateAddress(knownOwner); err != nil {
		t.Fatalf("valid address rejected: %v", err)
	}
	for _, bad := range []string{"", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", "not-base58-0OIl", "abc"} {
		if err := ValidateAddress(bad); err == nil {
			t.Fatalf("ValidateAddress(%q) accepted", bad)
		}
	}
}

func testRelayerKey(t *testing.T) (string, ed25519.PublicKey) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return solanakey.EncodeBase58(priv), priv.Public().(ed25519.PublicKey)
}

func TestBuildUSDCPayoutTransaction_layoutAndRelayerSignature(t *testing.T) {
	relayerKey, relayerPub := testRelayerKey(t)
	treasury := FakeAddress("treasury")
	blockhash := make([]byte, 32)
	blockhash[0] = 7

	txBase64, err := buildUSDCPayoutTransaction(relayerKey, treasury, knownOwner, 2_500_000, blockhash)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := base64.StdEncoding.DecodeString(txBase64)
	if raw[0] != 2 {
		t.Fatalf("signature slots = %d, want 2", raw[0])
	}
	message := raw[1+2*64:]
	if !ed25519.Verify(relayerPub, message, raw[1:65]) {
		t.Fatal("relayer signature does not verify")
	}
	for _, b := range raw[65:129] {
		if b != 0 {
			t.Fatal("treasury signature slot must stay empty for Privy")
		}
	}
	if got := message[:3]; got[0] != 2 || got[1] != 0 || got[2] != 5 {
		t.Fatalf("header = %v", got)
	}
	// accounts: count byte, then 9 keys; recipient ATA is index 3.
	accountsStart := 4
	destATA := solanakey.EncodeBase58(message[accountsStart+3*32 : accountsStart+4*32])
	if destATA != knownUSDCATA {
		t.Fatalf("destination ata = %s, want %s", destATA, knownUSDCATA)
	}
	// Last 8 bytes of the message are the transfer amount.
	if amount := binary.LittleEndian.Uint64(message[len(message)-8:]); amount != 2_500_000 {
		t.Fatalf("amount = %d", amount)
	}
	sig, err := feePayerSignature(txBase64)
	if err != nil || sig != solanakey.EncodeBase58(raw[1:65]) {
		t.Fatalf("feePayerSignature = %q, %v", sig, err)
	}
}

type rpcStub struct {
	t        *testing.T
	handlers map[string]func(params []json.RawMessage) any
	calls    map[string]int
}

func (s *rpcStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		s.t.Fatalf("decode rpc: %v", err)
	}
	s.calls[req.Method]++
	handler, ok := s.handlers[req.Method]
	if !ok {
		s.t.Fatalf("unexpected rpc method %s", req.Method)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": handler(req.Params)})
}

func newStubClient(t *testing.T, stub *rpcStub, privyURL string) *HTTPClient {
	t.Helper()
	stub.t = t
	stub.calls = map[string]int{}
	rpc := httptest.NewServer(stub)
	t.Cleanup(rpc.Close)
	return NewHTTPClientForTest(Config{RPCURL: rpc.URL}, privyURL, rpc.Client())
}

func TestTreasuryUSDCBalance_sumsUSDCAccounts(t *testing.T) {
	stub := &rpcStub{handlers: map[string]func([]json.RawMessage) any{
		"getTokenAccountsByOwner": func([]json.RawMessage) any {
			return map[string]any{"value": []any{
				tokenAccount(USDCMint, "1500000"),
				tokenAccount(USDCMint, "250000"),
			}}
		},
	}}
	c := newStubClient(t, stub, "")
	got, err := c.TreasuryUSDCBalance(context.Background(), knownOwner)
	if err != nil || got != 1_750_000 {
		t.Fatalf("balance = %d, %v", got, err)
	}
}

func tokenAccount(mint, amount string) map[string]any {
	return map[string]any{"account": map[string]any{"data": map[string]any{"parsed": map[string]any{"info": map[string]any{
		"mint": mint, "tokenAmount": map[string]any{"amount": amount},
	}}}}}
}

func TestUSDCPayoutStatus_states(t *testing.T) {
	cases := []struct {
		name   string
		status any
		height uint64
		want   PayoutState
	}{
		{"confirmed", map[string]any{"confirmationStatus": "confirmed", "err": nil}, 0, PayoutStateConfirmed},
		{"failed", map[string]any{"confirmationStatus": "finalized", "err": map[string]any{"InstructionError": []any{1, "Custom"}}}, 0, PayoutStateFailed},
		{"processed is pending", map[string]any{"confirmationStatus": "processed", "err": nil}, 0, PayoutStatePending},
		{"unseen live blockhash", nil, 900, PayoutStatePending},
		{"unseen expired blockhash", nil, 1001, PayoutStateDropped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &rpcStub{handlers: map[string]func([]json.RawMessage) any{
				"getSignatureStatuses": func([]json.RawMessage) any { return map[string]any{"value": []any{tc.status}} },
				"getBlockHeight":       func([]json.RawMessage) any { return tc.height },
			}}
			c := newStubClient(t, stub, "")
			got, err := c.USDCPayoutStatus(context.Background(), PreparedPayout{TxSignature: "sig", LastValidBlockHeight: 1000})
			if err != nil || got.State != tc.want {
				t.Fatalf("status = %+v, %v; want %s", got, err, tc.want)
			}
		})
	}
}

func testAuthorizationKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return "wallet-auth:" + base64.StdEncoding.EncodeToString(der)
}

func TestPrepareUSDCPayout_privyCosignsAndSignatureIsKnownBeforeBroadcast(t *testing.T) {
	relayerKey, _ := testRelayerKey(t)
	blockhash := solanakey.EncodeBase58(make([]byte, 32))
	stub := &rpcStub{handlers: map[string]func([]json.RawMessage) any{
		"getLatestBlockhash": func([]json.RawMessage) any {
			return map[string]any{"value": map[string]any{"blockhash": blockhash, "lastValidBlockHeight": 4242}}
		},
	}}
	var sawAuthSignature bool
	privy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/wallets/wallet-1/rpc" {
			t.Fatalf("unexpected privy path %s", r.URL.Path)
		}
		sawAuthSignature = r.Header.Get(authorizationSignatureHeader) != ""
		var body signTransactionRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Privy returns the transaction with the treasury slot filled; the fee payer slot stays.
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"signed_transaction": body.Params.Transaction}})
	}))
	t.Cleanup(privy.Close)

	c := newStubClient(t, stub, privy.URL)
	c.cfg.RelayerPrivateKey = relayerKey
	c.cfg.PrivyAuthorizationPrivateKey = testAuthorizationKey(t)

	prepared, err := c.PrepareUSDCPayout(context.Background(), PayUSDCRequest{
		TreasuryRef: TreasuryRef{PrivyWalletID: "wallet-1", SolanaAddress: FakeAddress("treasury")},
		ToAddress:   knownOwner,
		Amount:      1_000_000,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !sawAuthSignature {
		t.Fatal("privy wallet rpc must carry an authorization signature")
	}
	if prepared.TxSignature == "" || prepared.LastValidBlockHeight != 4242 {
		t.Fatalf("prepared = %+v", prepared)
	}
	if stub.calls["sendTransaction"] != 0 {
		t.Fatal("prepare must not broadcast")
	}
}

func TestInboundUSDCAmount_onlyUSDCFromSenderIntoTreasury(t *testing.T) {
	treasuryATA := knownUSDCATA
	from := FakeAddress("agent")
	fromATA, _ := USDCTokenAccount(from)
	other := FakeAddress("stranger")

	ix := func(typ, source, dest, authority, mint, amount string) parsedInstruction {
		var p parsedInstruction
		p.Program = "spl-token"
		p.Parsed = &struct {
			Type string `json:"type"`
			Info struct {
				Source            string `json:"source"`
				Destination       string `json:"destination"`
				Authority         string `json:"authority"`
				MultisigAuthority string `json:"multisigAuthority"`
				Mint              string `json:"mint"`
				Amount            string `json:"amount"`
				TokenAmount       *struct {
					Amount string `json:"amount"`
				} `json:"tokenAmount"`
			} `json:"info"`
		}{Type: typ}
		p.Parsed.Info.Source = source
		p.Parsed.Info.Destination = dest
		p.Parsed.Info.Authority = authority
		p.Parsed.Info.Mint = mint
		p.Parsed.Info.Amount = amount
		return p
	}
	tx := &parsedTransaction{}
	tx.Meta = &struct {
		Err               any `json:"err"`
		InnerInstructions []struct {
			Instructions []parsedInstruction `json:"instructions"`
		} `json:"innerInstructions"`
	}{}
	tx.Transaction.Message.Instructions = []parsedInstruction{
		ix("transfer", fromATA, treasuryATA, from, "", "400000"),
		ix("transferChecked", "x", treasuryATA, from, "So11111111111111111111111111111111111111112", "999"),
		ix("transfer", "y", treasuryATA, other, "", "123"),
		ix("transfer", fromATA, "elsewhere", from, "", "777"),
		ix("transferChecked", "z", treasuryATA, from, USDCMint, "100000"),
	}

	got, err := inboundUSDCAmount(tx, treasuryATA, from, fromATA)
	if err != nil || got != 500_000 {
		t.Fatalf("amount = %d, %v; want 500000", got, err)
	}

	tx.Meta.Err = map[string]any{"InstructionError": []any{0, "x"}}
	if got, _ := inboundUSDCAmount(tx, treasuryATA, from, fromATA); got != 0 {
		t.Fatalf("failed tx credited %d", got)
	}
}

func TestListInboundUSDCTransfers_scansTreasuryATAAndStopsAtSince(t *testing.T) {
	treasury := knownOwner
	from := FakeAddress("agent")
	fromATA, _ := USDCTokenAccount(from)
	now := time.Now().Unix()
	old := now - 3600

	stub := &rpcStub{handlers: map[string]func([]json.RawMessage) any{
		"getSignaturesForAddress": func(params []json.RawMessage) any {
			var addr string
			_ = json.Unmarshal(params[0], &addr)
			if addr != knownUSDCATA {
				t.Fatalf("scanned %s, want treasury usdc ata", addr)
			}
			return []any{
				map[string]any{"signature": "new-in", "blockTime": now, "confirmationStatus": "confirmed", "err": nil},
				map[string]any{"signature": "failed", "blockTime": now, "confirmationStatus": "confirmed", "err": map[string]any{"x": 1}},
				map[string]any{"signature": "before-since", "blockTime": old, "confirmationStatus": "finalized", "err": nil},
			}
		},
		"getTransaction": func(params []json.RawMessage) any {
			var sig string
			_ = json.Unmarshal(params[0], &sig)
			if sig != "new-in" {
				t.Fatalf("fetched %s", sig)
			}
			return map[string]any{
				"blockTime": now,
				"meta":      map[string]any{"err": nil, "innerInstructions": []any{}},
				"transaction": map[string]any{"message": map[string]any{"instructions": []any{
					map[string]any{"program": "spl-token", "parsed": map[string]any{"type": "transfer", "info": map[string]any{
						"source": fromATA, "destination": knownUSDCATA, "authority": from, "amount": "750000",
					}}},
				}}},
			}
		},
	}}
	c := newStubClient(t, stub, "")
	got, err := c.ListInboundUSDCTransfers(context.Background(), InboundQuery{
		TreasuryAddress: treasury,
		FromAddress:     from,
		Since:           time.Unix(now-60, 0),
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].TxSignature != "new-in" || got[0].Amount != 750_000 || got[0].FromAddress != from {
		t.Fatalf("transfers = %+v", got)
	}
	if !strings.HasPrefix(c.cfg.RPCURL, "http://") {
		t.Fatal("stub rpc not used")
	}
}

func TestListInboundUSDCTransfers_unindexedTransactionIsRetriedNotForgotten(t *testing.T) {
	from := FakeAddress("late-agent")
	fromATA, _ := USDCTokenAccount(from)
	now := time.Now().Unix()
	ready := false
	stub := &rpcStub{handlers: map[string]func([]json.RawMessage) any{
		"getSignaturesForAddress": func([]json.RawMessage) any {
			return []any{map[string]any{"signature": "late-sig", "blockTime": now, "confirmationStatus": "confirmed", "err": nil}}
		},
		"getTransaction": func([]json.RawMessage) any {
			if !ready {
				return nil
			}
			return map[string]any{
				"blockTime": now,
				"meta":      map[string]any{"err": nil},
				"transaction": map[string]any{"message": map[string]any{"instructions": []any{
					map[string]any{"program": "spl-token", "parsed": map[string]any{"type": "transfer", "info": map[string]any{
						"source": fromATA, "destination": knownUSDCATA, "authority": from, "amount": "10",
					}}},
				}}},
			}
		},
	}}
	c := newStubClient(t, stub, "")
	q := InboundQuery{TreasuryAddress: knownOwner, FromAddress: from}
	if got, err := c.ListInboundUSDCTransfers(context.Background(), q); err != nil || len(got) != 0 {
		t.Fatalf("first poll = %+v, %v", got, err)
	}
	ready = true
	if got, err := c.ListInboundUSDCTransfers(context.Background(), q); err != nil || len(got) != 1 || got[0].Amount != 10 {
		t.Fatalf("second poll = %+v, %v; want the transfer", got, err)
	}
}
