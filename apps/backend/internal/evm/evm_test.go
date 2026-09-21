package evm

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestNormalizeAddress_lowercasesAndValidates(t *testing.T) {
	got, err := NormalizeAddress("0xAbCdEf0123456789AbCdEf0123456789AbCdEf01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "0xabcdef0123456789abcdef0123456789abcdef01"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, err := NormalizeAddress("not-an-address"); err == nil {
		t.Fatal("expected invalid address error")
	}
}

func TestAuthorizationNonce_isDeterministic(t *testing.T) {
	a := AuthorizationNonce("deposit:abc")
	b := AuthorizationNonce("deposit:abc")
	if a != b {
		t.Fatal("nonce not deterministic")
	}
	if AuthorizationNonce("deposit:xyz") == a {
		t.Fatal("expected different nonce for different intent")
	}
}

func TestTransferAuthorizationTypedData_matchesUSDCDomain(t *testing.T) {
	nonce := AuthorizationNonce("intent-1")
	td := TransferAuthorizationTypedData(
		"0x1111111111111111111111111111111111111111",
		"0x2222222222222222222222222222222222222222",
		big.NewInt(1_000_000),
		time.Now().Add(time.Hour).Unix(),
		nonce,
	)
	if td.Domain.Name != "USD Coin" || td.Domain.Version != "2" || td.Domain.ChainID != ChainID {
		t.Fatalf("unexpected domain: %+v", td.Domain)
	}
	if td.Domain.VerifyingContract != USDCAddress {
		t.Fatalf("verifying contract %q", td.Domain.VerifyingContract)
	}
}

func TestWaitReceipt_returnsWhenFound(t *testing.T) {
	chain := NewFakeClient()
	chain.SetReceipt("0xabc", Receipt{Status: 1})
	got, err := WaitReceipt(t.Context(), chain, "0xabc")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Status != 1 {
		t.Fatalf("receipt = %+v", got)
	}
}

func TestDecodeHex_oddLength(t *testing.T) {
	b, err := decodeHex("0xf")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 1 || b[0] != 0x0f {
		t.Fatalf("got %x", b)
	}
}

func TestEncodeTransferWithAuthorization_selector(t *testing.T) {
	data, err := EncodeTransferWithAuthorization(
		"0x1111111111111111111111111111111111111111",
		"0x2222222222222222222222222222222222222222",
		big.NewInt(1),
		big.NewInt(0),
		big.NewInt(999),
		AuthorizationNonce("x"),
		27,
		[32]byte{1},
		[32]byte{2},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 4 {
		t.Fatal("expected calldata")
	}
	// transferWithAuthorization(address,address,uint256,uint256,uint256,bytes32,uint8,bytes32,bytes32)
	wantSel := crypto.Keccak256([]byte("transferWithAuthorization(address,address,uint256,uint256,uint256,bytes32,uint8,bytes32,bytes32)"))[:4]
	if string(data[:4]) != string(wantSel) {
		t.Fatalf("selector mismatch")
	}
}
