package chain_test

import (
	"context"
	"math/big"
	"testing"

	"pgregory.net/rapid"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	adapter "github.com/monaco/monaco/apps/backend/internal/modules/trading/adapters/chain"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/app"
	platform "github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain/solana"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
)

type readerRPC struct {
	statuses    []solana.Status
	transfers   []solana.Transfer
	statusErr   error
	transferErr error
	mint        platform.SolanaAddress
	valid       bool
	blockhash   string
}

func (r *readerRPC) BlockhashValid(_ context.Context, hash string) (bool, error) {
	r.blockhash = hash
	return r.valid, r.statusErr
}

func (r *readerRPC) SignatureStatuses(
	context.Context,
	[]platform.Signature,
) ([]solana.Status, error) {
	return r.statuses, r.statusErr
}

func (r *readerRPC) InboundTransfersForMint(
	_ context.Context,
	_ platform.Signature,
	_ platform.SolanaAddress,
	mint platform.SolanaAddress,
) ([]solana.Transfer, error) {
	r.mint = mint
	return r.transfers, r.transferErr
}

func TestReader_signatureStatusesMapsEveryStateAndRefusesUnknownData(t *testing.T) {
	t.Parallel()
	rpc := &readerRPC{statuses: []solana.Status{
		{State: solana.StateFinalized, Failed: true, BlockHeight: 12},
		{State: solana.StateProcessing, BlockHeight: 13},
		{State: solana.StateNotFound, BlockHeight: 14},
	}}
	got, err := adapter.NewReader(rpc).
		SignatureStatuses(t.Context(), []platform.Signature{"a", "b", "c"})
	want := []app.SigStatus{
		{State: app.SigFinalized, Failed: true, BlockHeight: 12},
		{State: app.SigProcessing, BlockHeight: 13},
		{State: app.SigNotFound, BlockHeight: 14},
	}
	if err != nil || len(got) != len(want) {
		t.Fatalf("SignatureStatuses = %+v, %v", got, err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SignatureStatuses[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	_, err = adapter.NewReader(&readerRPC{statuses: []solana.Status{{State: 99}}}).
		SignatureStatuses(t.Context(), nil)
	if errs.CodeOf(err) != errs.CodeDecodeFailed {
		t.Fatalf("unknown status error = %v, want decode_failed", err)
	}
	_, err = adapter.NewReader(&readerRPC{
		statusErr: errs.New(errs.CodeRPCUnavailable, "test"),
	}).SignatureStatuses(t.Context(), nil)
	if errs.CodeOf(err) != errs.CodeRPCUnavailable {
		t.Fatalf("status error = %v, want rpc_unavailable", err)
	}
}

func TestReader_inboundAmountSumsOnlyTheRequestedMintNetOfFees(t *testing.T) {
	t.Parallel()
	mint := aaplx()
	rpc := &readerRPC{transfers: []solana.Transfer{
		{
			Mint:   mint,
			Amount: money.NewBaseUnits(15, 8),
			Fee:    money.NewBaseUnits(2, 8),
			Net:    money.NewBaseUnits(13, 8),
		},
		{
			Mint:   mint,
			Amount: money.NewBaseUnits(8, 8),
			Fee:    money.NewBaseUnits(1, 8),
			Net:    money.NewBaseUnits(7, 8),
		},
	}}
	got, err := adapter.NewReader(rpc).InboundAmount(t.Context(), "sig", "treasury", mint)
	if err != nil || got != money.NewBaseUnits(20, 8) {
		t.Fatalf("InboundAmount = %v, %v, want 20", got, err)
	}
	if rpc.mint != mint.Address {
		t.Fatalf("requested mint = %s, want %s", rpc.mint, mint.Address)
	}
	got, err = adapter.NewReader(&readerRPC{}).InboundAmount(t.Context(), "sig", "treasury", mint)
	if err != nil || !got.IsZero() || got.Decimals() != mint.Decimals {
		t.Fatalf("empty InboundAmount = %v, %v", got, err)
	}
	_, err = adapter.NewReader(&readerRPC{transferErr: errs.New(errs.CodeRPCUnavailable, "test")}).
		InboundAmount(
			t.Context(), "sig", "treasury", mint)
	if errs.CodeOf(err) != errs.CodeRPCUnavailable {
		t.Fatalf("transfer error = %v, want rpc_unavailable", err)
	}
	_, err = adapter.NewReader(&readerRPC{transfers: []solana.Transfer{{
		Mint: mint, Net: money.NewBaseUnits(1, mint.Decimals-1),
	}}}).InboundAmount(t.Context(), "sig", "treasury", mint)
	if errs.CodeOf(err) != errs.CodeDecodeFailed {
		t.Fatalf("mismatched decimals = %v, want decode_failed", err)
	}
}

func TestReader_blockhashValidityComesFromTheSignedTransaction(t *testing.T) {
	t.Parallel()
	hash := make([]byte, 32)
	hash[31] = 7
	key := make([]byte, 32)
	key[31] = 1
	message := append([]byte{1, 0, 0, 1}, key...)
	message = append(message, hash...)
	message = append(message, 0)
	signed := append(append([]byte{1}, make([]byte, 64)...), message...)
	rpc := &readerRPC{valid: true}
	valid, err := adapter.NewReader(rpc).BlockhashValid(t.Context(), signed)
	if err != nil || !valid || rpc.blockhash != platform.EncodeBase58(hash) {
		t.Fatalf("BlockhashValid = %t, %v, hash %q", valid, err, rpc.blockhash)
	}
	if _, err = adapter.NewReader(rpc).BlockhashValid(t.Context(), nil); errs.CodeOf(
		err,
	) != errs.CodeInvalidInput {
		t.Fatalf("invalid signed transaction = %v", err)
	}
}

func TestReader_inboundAmountSumsRandomNetTransfers(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mint := aaplx()
		values := rapid.SliceOfN(rapid.Uint64Range(0, 1_000_000), 0, 20).Draw(t, "nets")
		transfers := make([]solana.Transfer, len(values))
		want := new(big.Int)
		for i, value := range values {
			transfers[i] = solana.Transfer{
				Mint: mint, Amount: money.NewBaseUnits(value+1, 8),
				Fee: money.NewBaseUnits(1, 8), Net: money.NewBaseUnits(value, 8),
			}
			want.Add(want, new(big.Int).SetUint64(value))
		}
		got, err := adapter.NewReader(&readerRPC{transfers: transfers}).InboundAmount(
			t.Context(), "sig", "treasury", mint,
		)
		if err != nil || got.Uint64() != want.Uint64() {
			t.Fatalf("InboundAmount = %v, %v, want %s", got, err, want)
		}
	})
}
