package jupiter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/solana/swapchain"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

const resolveTreasury = "TreasuryResolve1111111111111111111111111111"

func pendingBuy() swapprovider.PendingSwap {
	return swapprovider.PendingSwap{
		Request: swapprovider.Request{
			GroupID:    "group-1",
			Side:       swapprovider.SideBuy,
			InputMint:  USDCMint,
			OutputMint: AAPLxMint,
			Amount:     2_000_000,
			Wallet:     swapprovider.Wallet{PrivyWalletID: "wallet-1", SolanaAddress: resolveTreasury},
		},
		RequestID:   "req-resolve",
		Submitted:   true,
		TxSignature: "sig-resolve",
		Blockhash:   "hash-resolve",
	}
}

func resolveProvider(chain swapprovider.ChainReader) *SwapProvider {
	provider := NewSwapProvider(NewFakeClient(), nil)
	if chain != nil {
		provider.SetChainReader(chain)
	}
	return provider
}

func TestJupiterResolve_finalizedSwap_readsFillFromChain(t *testing.T) {
	chain := swapchain.NewFakeReader()
	chain.Land("sig-resolve", map[string]int64{USDCMint: -1_995_000, AAPLxMint: 997_500})

	got, err := resolveProvider(chain).Resolve(context.Background(), pendingBuy())

	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Outcome != swapprovider.OutcomeFilled || !got.Fill.Confirmed || got.Fill.Signature != "sig-resolve" {
		t.Fatalf("Resolve() = %+v, want filled with the signed transaction's signature", got)
	}
	if got.Fill.InputAmount != 1_995_000 || got.Fill.OutputAmount != 997_500 {
		t.Fatalf("fill = %d/%d, want 1995000/997500", got.Fill.InputAmount, got.Fill.OutputAmount)
	}
}

func TestJupiterResolve_sell_readsProceedsFromChain(t *testing.T) {
	chain := swapchain.NewFakeReader()
	chain.Land("sig-resolve", map[string]int64{AAPLxMint: -1_000_000, USDCMint: 3_351_309})
	pending := pendingBuy()
	pending.Request.Side = swapprovider.SideSell
	pending.Request.InputMint, pending.Request.OutputMint = AAPLxMint, USDCMint

	got, err := resolveProvider(chain).Resolve(context.Background(), pending)

	if err != nil || got.Outcome != swapprovider.OutcomeFilled || got.Fill.OutputAmount != 3_351_309 {
		t.Fatalf("Resolve() = %+v, %v, want filled with 3351309 USDC proceeds", got, err)
	}
}

func TestJupiterResolve_notFoundWhileBlockhashValid_staysUnknown(t *testing.T) {
	chain := swapchain.NewFakeReader()

	got, err := resolveProvider(chain).Resolve(context.Background(), pendingBuy())

	if err != nil || got.Outcome != swapprovider.OutcomeUnknown {
		t.Fatalf("Resolve() = %+v, %v, want unknown: the transaction can still land", got, err)
	}
}

func TestJupiterResolve_notFoundAfterBlockhashExpired_isFailed(t *testing.T) {
	chain := swapchain.NewFakeReader()
	chain.ExpireBlockhashes()

	got, err := resolveProvider(chain).Resolve(context.Background(), pendingBuy())

	if err != nil || got.Outcome != swapprovider.OutcomeFailed {
		t.Fatalf("Resolve() = %+v, %v, want failed", got, err)
	}
	if calls := chain.StatusCalls(); calls != 2 {
		t.Fatalf("signature looked up %d times, want 2: once more after expiry is established", calls)
	}
}

func TestJupiterResolve_landsBetweenLookupAndExpiry_isNotFailed(t *testing.T) {
	// The transaction lands in the last valid slot, after the first signature lookup.
	chain := swapchain.NewFakeReader()
	chain.ExpireBlockhashes()
	chain.OnBlockhashLookup(func() {
		chain.Land("sig-resolve", map[string]int64{USDCMint: -2_000_000, AAPLxMint: 1_000_000})
	})

	got, err := resolveProvider(chain).Resolve(context.Background(), pendingBuy())

	if err != nil || got.Outcome != swapprovider.OutcomeFilled {
		t.Fatalf("Resolve() = %+v, %v, want filled: a retry here would double-buy", got, err)
	}
}

func TestJupiterResolve_landedButNotFinalized_staysUnknown(t *testing.T) {
	chain := swapchain.NewFakeReader()
	chain.SetStatus("sig-resolve", swapprovider.SignatureStatus{Found: true})

	got, err := resolveProvider(chain).Resolve(context.Background(), pendingBuy())

	if err != nil || got.Outcome != swapprovider.OutcomeUnknown {
		t.Fatalf("Resolve() = %+v, %v, want unknown until finalized", got, err)
	}
}

func TestJupiterResolve_failedOnChain_isFailed(t *testing.T) {
	chain := swapchain.NewFakeReader()
	chain.SetStatus("sig-resolve", swapprovider.SignatureStatus{Found: true, Finalized: true, Failed: true, Err: `{"InstructionError":[3,{"Custom":6001}]}`})

	got, err := resolveProvider(chain).Resolve(context.Background(), pendingBuy())

	if err != nil || got.Outcome != swapprovider.OutcomeFailed || !strings.Contains(got.Reason, "6001") {
		t.Fatalf("Resolve() = %+v, %v, want failed carrying the chain error", got, err)
	}
}

func TestJupiterResolve_rpcErrorOrMissingReader_givesNoVerdict(t *testing.T) {
	chain := swapchain.NewFakeReader()
	chain.ExpireBlockhashes()
	chain.SetError(errors.New("solana rpc status 429"))

	for name, provider := range map[string]*SwapProvider{
		"rpc error":       resolveProvider(chain),
		"no chain reader": resolveProvider(nil),
	} {
		got, err := provider.Resolve(context.Background(), pendingBuy())
		if err == nil || got.Outcome == swapprovider.OutcomeFailed || got.Outcome == swapprovider.OutcomeFilled {
			t.Fatalf("%s: Resolve() = %+v, %v, want an error and no verdict", name, got, err)
		}
	}
}

func TestJupiterResolve_finalizedWithoutExpectedBalanceChange_givesNoVerdict(t *testing.T) {
	chain := swapchain.NewFakeReader()
	chain.Land("sig-resolve", map[string]int64{USDCMint: -2_000_000})

	got, err := resolveProvider(chain).Resolve(context.Background(), pendingBuy())

	if err == nil || got.Outcome != "" {
		t.Fatalf("Resolve() = %+v, %v, want an error: never record a fill with no output", got, err)
	}
}

func TestJupiterResolve_rowWithoutSignedTransaction_staysUnknown(t *testing.T) {
	chain := swapchain.NewFakeReader()
	chain.ExpireBlockhashes()
	pending := pendingBuy()
	pending.TxSignature, pending.Blockhash = "", ""

	got, err := resolveProvider(chain).Resolve(context.Background(), pending)

	if err != nil || got.Outcome != swapprovider.OutcomeUnknown {
		t.Fatalf("Resolve() = %+v, %v, want unknown", got, err)
	}
}

// opaqueSigner returns bytes that are not a Solana transaction.
type opaqueSigner struct{}

func (opaqueSigner) SignTreasuryTransaction(ctx context.Context, walletID, unsignedTxBase64 string) (string, error) {
	return "SIGNEDdeadbeef", nil
}

func TestJupiterPrepare_unreadableSignedTransaction_isNeverSubmittable(t *testing.T) {
	client := NewFakeClient()
	RegisterQuoteBuy(client, AAPLxMint, 2_000_000, BuyQuote{Routable: true, InAmount: "2000000", OutAmount: "1000000", RequestID: "req-opaque"})
	provider := NewSwapProvider(client, opaqueSigner{})
	req := pendingBuy().Request

	_, err := provider.PrepareBuy(context.Background(), req)

	if err == nil {
		t.Fatal("PrepareBuy() error = nil, want a refusal: a swap with no readable signature cannot be reconciled")
	}
	if stage, _ := swapprovider.StageOf(err, ""); stage != "read_signed_tx" {
		t.Fatalf("stage = %q, want read_signed_tx", stage)
	}
}
