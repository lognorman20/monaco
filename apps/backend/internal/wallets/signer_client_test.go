package wallets

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/signer"
)

func testSharesKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func TestSignerClient_EnsureMemberWallet_isIdempotent(t *testing.T) {
	ctx := context.Background()
	sig := signer.NewFakeClient()
	store := NewMemoryStore()
	c := NewSignerClient(sig, evm.NewFakeClient(), store, testSharesKey(), "0xrelayer")

	first, err := c.EnsureMemberWallet(ctx, "dyn-1", UserID("user-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.EnsureMemberWallet(ctx, "dyn-1", UserID("user-1"))
	if err != nil {
		t.Fatal(err)
	}
	if first.WalletID != second.WalletID || first.Address != second.Address {
		t.Fatalf("not idempotent: %+v vs %+v", first, second)
	}
}

func TestSignerClient_SubmitSweep_buildsEIP3009AndRelays(t *testing.T) {
	ctx := context.Background()
	sig := signer.NewFakeClient()
	store := NewMemoryStore()
	chain := evm.NewFakeClient()
	c := NewSignerClient(sig, chain, store, testSharesKey(), "0xrelayer")
	wallet, err := c.EnsureMemberWallet(ctx, "dyn-1", UserID("user-1"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.SubmitSweep(ctx, SweepRequest{
		MemberAddress:   wallet.Address,
		TreasuryAddress: "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
		Amount:          1_000_000,
		IntentID:        "deposit-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.TxHash == "" {
		t.Fatal("expected tx hash")
	}
	if signer.RelayerSendCount(sig) != 1 {
		t.Fatalf("relayer sends = %d, want 1", signer.RelayerSendCount(sig))
	}
}

func TestSignerClient_SubmitSweep_looksUpMemberWithoutMemoryStoreTypeAssert(t *testing.T) {
	ctx := context.Background()
	sig := signer.NewFakeClient()
	inner := NewMemoryStore()
	store := wrappingStore{WalletStore: inner}
	c := NewSignerClient(sig, evm.NewFakeClient(), store, testSharesKey(), "0xrelayer")
	wallet, err := c.EnsureMemberWallet(ctx, "dyn-wrap", UserID("user-wrap"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SubmitSweep(ctx, SweepRequest{
		MemberAddress:   wallet.Address,
		TreasuryAddress: "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
		Amount:          1_000_000,
		IntentID:        "deposit-wrap",
	}); err != nil {
		t.Fatal(err)
	}
}

type wrappingStore struct {
	WalletStore
}

func TestSplitSignature_normalizesRecoveryID(t *testing.T) {
	raw := make([]byte, 65)
	raw[64] = 0
	v, _, _, err := splitSignature("0x" + hex.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	if v != 27 {
		t.Fatalf("v = %d, want 27", v)
	}
	raw[64] = 1
	v, _, _, err = splitSignature("0x" + hex.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	if v != 28 {
		t.Fatalf("v = %d, want 28", v)
	}
}

func TestSignerClient_PayUSDC_topsUpGasWhenBelowFloor(t *testing.T) {
	ctx := context.Background()
	sig := signer.NewFakeClient()
	store := NewMemoryStore()
	chain := evm.NewFakeClient()
	c := NewSignerClient(sig, chain, store, testSharesKey(), "0xrelayer")
	treasury, err := c.EnsureTreasury(ctx, GroupID("group-1"))
	if err != nil {
		t.Fatal(err)
	}
	chain.SetETHBalance(treasury.Address, big.NewInt(0))
	_, err = c.PayUSDC(ctx, PayUSDCRequest{
		TreasuryRef: treasury,
		ToAddress:   "0x000000000000000000000000000000000000dEaD",
		Amount:      1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if signer.RelayerSendCount(sig) < 1 {
		t.Fatal("expected gas top-up relayer send")
	}
	_ = json.RawMessage(nil)
}
