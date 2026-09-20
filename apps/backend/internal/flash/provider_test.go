package flash

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"testing"
	"time"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

const testSponsor = "5ZWj7a1f8tWkjBESHKgrLmXshuXxqeY9SYcfbshpAqPG"

// testClock sits well before the fixture deadline so quotes are live unless a test says otherwise.
var testClock = time.Unix(1_789_861_000, 0)

const testDeadline = "1789861368"

// recordingSigner signs with a real Ed25519 key so tests can verify what was signed.
type recordingSigner struct {
	priv       ed25519.PrivateKey
	messages   [][]byte
	signedTxs  []string
	messageErr error
}

func newRecordingSigner() *recordingSigner {
	return &recordingSigner{priv: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))}
}

func (s *recordingSigner) SignTreasuryMessage(ctx context.Context, walletID string, message []byte) ([]byte, error) {
	if s.messageErr != nil {
		return nil, s.messageErr
	}
	s.messages = append(s.messages, message)
	return ed25519.Sign(s.priv, message), nil
}

func (s *recordingSigner) SignTreasuryTransaction(ctx context.Context, walletID, unsignedTxBase64 string) (string, error) {
	s.signedTxs = append(s.signedTxs, unsignedTxBase64)
	return "signed:" + unsignedTxBase64, nil
}

type recordingSetup struct {
	calls [][]Instruction
	err   error
}

func (s *recordingSetup) SubmitTreasurySetup(ctx context.Context, wallet swapprovider.Wallet, instructions []Instruction) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.calls = append(s.calls, instructions)
	return "setup-sig", nil
}

func testBuyRequest() swapprovider.Request {
	return swapprovider.Request{
		GroupID:        "group-1",
		UserID:         "user-1",
		Symbol:         "AAPLx",
		Side:           swapprovider.SideBuy,
		InputMint:      testUSDCMint,
		OutputMint:     testAAPLxMint,
		InputDecimals:  6,
		OutputDecimals: 8,
		Amount:         5_000_000,
		Wallet:         swapprovider.Wallet{PrivyWalletID: "wallet-1", SolanaAddress: testFunder},
	}
}

func testSellRequest() swapprovider.Request {
	req := testBuyRequest()
	req.Side = swapprovider.SideSell
	req.InputMint, req.OutputMint = testAAPLxMint, testUSDCMint
	req.InputDecimals, req.OutputDecimals = 8, 6
	req.Amount = 1_000_000
	return req
}

func readyQuote(id, mint string, total int64) Quote {
	return Quote{
		QuoteID:      id,
		OrderMessage: fmt.Sprintf("DFS|m=%s|t=%d|h=5UkgEbSVUhBHyPYDpDDpQY", mint, total),
		Nonce:        "932385860354111",
		Deadline:     testDeadline,
	}
}

func newTestProvider(client Client, signer TreasurySigner, setup SetupSubmitter) *SwapProvider {
	return NewSwapProvider(client, signer, setup, ProviderConfig{
		SponsorAddress: testSponsor,
		SetupPoll:      TestPollConfig(),
		Now:            func() time.Time { return testClock },
	})
}

func TestFlashProvider_buy_signsOrderMessageAndSubmits(t *testing.T) {
	t.Parallel()

	// Arrange
	client := NewFakeClient()
	RegisterQuotes(client, "buy", testAAPLxMint, "5", readyQuote("quote-1", testUSDCMint, 5_000_000))
	signer := newRecordingSigner()
	setup := &recordingSetup{}
	provider := newTestProvider(client, signer, setup)

	// Act
	sub, err := provider.SubmitBuy(context.Background(), testBuyRequest())

	// Assert
	if err != nil {
		t.Fatalf("SubmitBuy() error = %v", err)
	}
	if sub.RequestID != FakeOrderID("quote-1") || sub.Receipt != testFunder {
		t.Fatalf("unexpected submission %+v", sub)
	}
	if len(setup.calls) != 0 {
		t.Fatal("expected no onchain setup for a ready treasury")
	}
	submitted := Submissions(client)
	if len(submitted) != 1 {
		t.Fatalf("submissions = %d, want 1", len(submitted))
	}
	got := submitted[0]
	if got.Quote.MaxSlippage != DefaultMaxSlippage || got.Quote.FunderAddress != testFunder {
		t.Fatalf("unexpected order fields %+v", got.Quote)
	}
	if got.Nonce != "932385860354111" || got.Deadline != testDeadline {
		t.Fatalf("nonce/deadline not echoed: %+v", got)
	}
	signature, err := solanakey.DecodeBase58(got.UserSignature)
	if err != nil {
		t.Fatalf("decode userSignature: %v", err)
	}
	message := []byte(readyQuote("quote-1", testUSDCMint, 5_000_000).OrderMessage)
	if !ed25519.Verify(signer.priv.Public().(ed25519.PublicKey), message, signature) {
		t.Fatal("userSignature does not verify against the quote order message")
	}
}

func TestFlashProvider_sell_quotesTargetUnits(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	RegisterQuotes(client, "sell", testAAPLxMint, "0.01", readyQuote("quote-sell", testAAPLxMint, 1_000_000))
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	sub, err := provider.SubmitSell(context.Background(), testSellRequest())

	if err != nil {
		t.Fatalf("SubmitSell() error = %v", err)
	}
	got := Submissions(client)[0].Quote
	if got.Side != "sell" || got.TargetAsset != testAAPLxMint || got.ContraAsset != testUSDCMint || got.Qty != "0.01" {
		t.Fatalf("unexpected sell order fields %+v", got)
	}
	if sub.Request.Side != swapprovider.SideSell {
		t.Fatalf("submission side = %q", sub.Request.Side)
	}
}

func TestFlashProvider_firstTrade_landsSponsoredSetupThenRequotes(t *testing.T) {
	t.Parallel()

	// Arrange
	needsSetup, err := ParseQuoteResponse(FixtureQuoteNeedsSetup(testFunder, testAAPLxMint, testUSDCMint))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	client := NewFakeClient()
	RegisterQuotes(client, "buy", testAAPLxMint, "5", needsSetup, needsSetup, readyQuote("quote-after-setup", testUSDCMint, 5_000_000))
	setup := &recordingSetup{}
	provider := newTestProvider(client, newRecordingSigner(), setup)

	// Act
	sub, err := provider.SubmitBuy(context.Background(), testBuyRequest())

	// Assert
	if err != nil {
		t.Fatalf("SubmitBuy() error = %v", err)
	}
	if sub.RequestID != FakeOrderID("quote-after-setup") {
		t.Fatalf("order must use the post-setup quote, got %q", sub.RequestID)
	}
	if len(setup.calls) != 1 || len(setup.calls[0]) != 2 {
		t.Fatalf("expected one setup tx with create-ATA + approve, got %+v", setup.calls)
	}
	createATA, approve := setup.calls[0][0], setup.calls[0][1]
	if createATA.Accounts[0].Pubkey != testSponsor {
		t.Fatalf("rent payer = %q, want sponsor %q", createATA.Accounts[0].Pubkey, testSponsor)
	}
	if createATA.Accounts[2].Pubkey != testFunder {
		t.Fatalf("token account owner = %q, want treasury %q", createATA.Accounts[2].Pubkey, testFunder)
	}
	if approve.Accounts[2].Pubkey != testFunder || !approve.Accounts[2].IsSigner {
		t.Fatalf("approve authority must stay the treasury signer: %+v", approve.Accounts[2])
	}
	if needsSetup.ATASetupIxs[0].Accounts[0].Pubkey != testFunder {
		t.Fatal("SponsorATASetup must not mutate the quote's instructions")
	}
}

func TestFlashProvider_setupNeverLands_returnsErrSetupNotConfirmed(t *testing.T) {
	t.Parallel()

	needsSetup, _ := ParseQuoteResponse(FixtureQuoteNeedsSetup(testFunder, testAAPLxMint, testUSDCMint))
	client := NewFakeClient()
	RegisterQuotes(client, "buy", testAAPLxMint, "5", needsSetup)
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	_, err := provider.SubmitBuy(context.Background(), testBuyRequest())

	if !errors.Is(err, ErrSetupNotConfirmed) {
		t.Fatalf("SubmitBuy() error = %v, want ErrSetupNotConfirmed", err)
	}
	if stage, _ := swapprovider.StageOf(err, ""); stage != "onchain_setup" {
		t.Fatalf("stage = %q, want onchain_setup", stage)
	}
	if len(Submissions(client)) != 0 {
		t.Fatal("no order may be submitted before setup lands")
	}
}

func TestFlashProvider_setupSubmitFails_doesNotSignOrSubmit(t *testing.T) {
	t.Parallel()

	needsSetup, _ := ParseQuoteResponse(FixtureQuoteNeedsSetup(testFunder, testAAPLxMint, testUSDCMint))
	client := NewFakeClient()
	RegisterQuotes(client, "buy", testAAPLxMint, "5", needsSetup)
	signer := newRecordingSigner()
	provider := newTestProvider(client, signer, &recordingSetup{err: errors.New("privy: rpc down")})

	_, err := provider.SubmitBuy(context.Background(), testBuyRequest())

	if err == nil || len(signer.messages) != 0 || len(Submissions(client)) != 0 {
		t.Fatalf("err = %v signed = %d submitted = %d", err, len(signer.messages), len(Submissions(client)))
	}
}

func TestFlashProvider_quoteExpired_refusesToSign(t *testing.T) {
	t.Parallel()

	// Arrange
	client := NewFakeClient()
	RegisterQuotes(client, "buy", testAAPLxMint, "5", readyQuote("quote-stale", testUSDCMint, 5_000_000))
	signer := newRecordingSigner()
	provider := NewSwapProvider(client, signer, &recordingSetup{}, ProviderConfig{
		Now: func() time.Time { return time.Unix(1_789_861_368, 0) },
	})

	// Act
	_, err := provider.SubmitBuy(context.Background(), testBuyRequest())

	// Assert
	if !errors.Is(err, ErrQuoteExpired) {
		t.Fatalf("SubmitBuy() error = %v, want ErrQuoteExpired", err)
	}
	if len(signer.messages) != 0 || len(Submissions(client)) != 0 {
		t.Fatal("an expired quote must not be signed or submitted")
	}
}

func TestFlashProvider_orderMessageMismatch_refusesToSign(t *testing.T) {
	t.Parallel()

	cases := map[string]Quote{
		"wrong mint":      readyQuote("q", testAAPLxMint, 5_000_000),
		"larger total":    readyQuote("q", testUSDCMint, 50_000_000),
		"missing payload": {QuoteID: "q"},
		"opaque message":  {QuoteID: "q", OrderMessage: "sign me", Nonce: "1", Deadline: testDeadline},
	}
	for name, quote := range cases {
		client := NewFakeClient()
		RegisterQuotes(client, "buy", testAAPLxMint, "5", quote)
		signer := newRecordingSigner()
		provider := newTestProvider(client, signer, &recordingSetup{})

		_, err := provider.SubmitBuy(context.Background(), testBuyRequest())

		if err == nil {
			t.Fatalf("%s: expected error", name)
		}
		if name != "missing payload" && !errors.Is(err, ErrOrderMessageMismatch) {
			t.Fatalf("%s: error = %v, want ErrOrderMessageMismatch", name, err)
		}
		if len(signer.messages) != 0 || len(Submissions(client)) != 0 {
			t.Fatalf("%s: treasury must not sign or submit", name)
		}
	}
}

func TestFlashProvider_noRoute_mapsToErrNotRoutable(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	RegisterQuoteError(client, "buy", testAAPLxMint, "5", fmt.Errorf("%w: asset not found", ErrNoRoute))
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	_, err := provider.SubmitBuy(context.Background(), testBuyRequest())

	if !errors.Is(err, swapprovider.ErrNotRoutable) {
		t.Fatalf("SubmitBuy() error = %v, want ErrNotRoutable", err)
	}
}

func TestFlashProvider_quoteOutage_isNotReportedAsNotRoutable(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	RegisterQuoteError(client, "buy", testAAPLxMint, "5", &APIError{Status: 503, Message: "unavailable"})
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	_, err := provider.SubmitBuy(context.Background(), testBuyRequest())

	if err == nil || errors.Is(err, swapprovider.ErrNotRoutable) {
		t.Fatalf("SubmitBuy() error = %v, want a non-routability error", err)
	}
	if stage, _ := swapprovider.StageOf(err, ""); stage != "quote" {
		t.Fatalf("stage = %q, want quote", stage)
	}
}

func TestFlashProvider_signFailsOrShortSignature_doesNotSubmit(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	RegisterQuotes(client, "buy", testAAPLxMint, "5", readyQuote("quote-1", testUSDCMint, 5_000_000))
	signer := newRecordingSigner()
	signer.messageErr = errors.New("privy: sign message status 401")
	provider := newTestProvider(client, signer, &recordingSetup{})

	_, err := provider.SubmitBuy(context.Background(), testBuyRequest())

	if stage, _ := swapprovider.StageOf(err, ""); err == nil || stage != "sign_treasury" {
		t.Fatalf("err = %v stage = %q, want sign_treasury failure", err, stage)
	}
	if len(Submissions(client)) != 0 {
		t.Fatal("no order may be submitted without a treasury signature")
	}
}

func TestFlashProvider_orderSubmitRejected_returnsStageError(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	RegisterQuotes(client, "buy", testAAPLxMint, "5", readyQuote("quote-1", testUSDCMint, 5_000_000))
	SetSubmitError(client, &APIError{Status: 422, Code: "FAILED_PRECONDITION", Message: "delegation does not cover order total"})
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	_, err := provider.SubmitBuy(context.Background(), testBuyRequest())

	stage, requestID := swapprovider.StageOf(err, "")
	if err == nil || stage != "order_submit" || requestID != "quote-1" {
		t.Fatalf("err = %v stage = %q request = %q", err, stage, requestID)
	}
}

func TestFlashProvider_sponsoredDelegate_signsAndEchoesTransaction(t *testing.T) {
	t.Parallel()

	quote := readyQuote("quote-sponsored", testUSDCMint, 5_000_000)
	quote.SponsoredDelegateTx = "dW5zaWduZWQtZGVsZWdhdGU="
	client := NewFakeClient()
	RegisterQuotes(client, "buy", testAAPLxMint, "5", quote)
	setup := &recordingSetup{}
	provider := newTestProvider(client, newRecordingSigner(), setup)

	_, err := provider.SubmitBuy(context.Background(), testBuyRequest())

	if err != nil {
		t.Fatalf("SubmitBuy() error = %v", err)
	}
	if got := Submissions(client)[0].SignedSponsoredDelegateTx; got != "signed:"+quote.SponsoredDelegateTx {
		t.Fatalf("svmSponsoredDelegateTx = %q", got)
	}
	if len(setup.calls) != 0 {
		t.Fatal("a sponsored delegation must not also be sent onchain by us")
	}
}

func TestFlashProvider_awaitFill_buy_convertsTotalsToAtomics(t *testing.T) {
	t.Parallel()

	// Arrange
	client := NewFakeClient()
	RegisterOrderPoll(client, "ord-1",
		Order{Status: OrderStatusPending},
		Order{Status: OrderStatusAccepted},
		Order{Status: OrderStatusFilled},
		Order{Status: OrderStatusFilled, TransactionID: "flash-sig", FilledTargetAmount: "0.01486083", FilledContraAmount: "5"},
	)
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	// Act
	fill, err := provider.AwaitFill(context.Background(),
		swapprovider.Submission{RequestID: "ord-1", Receipt: testFunder, Request: testBuyRequest()}, TestPollConfig())

	// Assert
	if err != nil {
		t.Fatalf("AwaitFill() error = %v", err)
	}
	if !fill.Confirmed || fill.Signature != "flash-sig" {
		t.Fatalf("unexpected fill %+v", fill)
	}
	if fill.InputAmount != 5_000_000 || fill.OutputAmount != 1_486_083 {
		t.Fatalf("amounts = %d/%d, want 5000000/1486083", fill.InputAmount, fill.OutputAmount)
	}
}

func TestFlashProvider_awaitFill_sell_readsUSDCProceeds(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	RegisterOrderPoll(client, "ord-sell",
		Order{Status: OrderStatusFilled, TransactionID: "sell-sig", FilledTargetAmount: "0.01", FilledContraAmount: "3.351309"})
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	fill, err := provider.AwaitFill(context.Background(),
		swapprovider.Submission{RequestID: "ord-sell", Receipt: testFunder, Request: testSellRequest()}, TestPollConfig())

	if err != nil {
		t.Fatalf("AwaitFill() error = %v", err)
	}
	if fill.InputAmount != 1_000_000 || fill.OutputAmount != 3_351_309 {
		t.Fatalf("amounts = %d/%d, want 1000000/3351309", fill.InputAmount, fill.OutputAmount)
	}
}

func TestFlashProvider_awaitFill_rejectedOrder_isUnconfirmed(t *testing.T) {
	t.Parallel()

	for _, status := range []string{OrderStatusRejected, OrderStatusCancelled, OrderStatusTerminated} {
		client := NewFakeClient()
		RegisterOrderPoll(client, "ord-bad", Order{Status: OrderStatusPending}, Order{Status: status, CloseReason: "REASON_UNSPECIFIED"})
		provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

		fill, err := provider.AwaitFill(context.Background(),
			swapprovider.Submission{RequestID: "ord-bad", Receipt: testFunder, Request: testBuyRequest()}, TestPollConfig())

		if !errors.Is(err, ErrOrderRejected) {
			t.Fatalf("%s: error = %v, want ErrOrderRejected", status, err)
		}
		if fill.Confirmed {
			t.Fatalf("%s: a closed unfilled order must not read as confirmed", status)
		}
	}
}

func TestFlashProvider_awaitFill_neverFills_exhaustsUnconfirmed(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	RegisterOrderPoll(client, "ord-slow", Order{Status: OrderStatusPartiallyFilled})
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	fill, err := provider.AwaitFill(context.Background(),
		swapprovider.Submission{RequestID: "ord-slow", Receipt: testFunder, Request: testBuyRequest()}, TestPollConfig())

	if err == nil || fill.Confirmed {
		t.Fatalf("err = %v confirmed = %v, want exhausted and unconfirmed", err, fill.Confirmed)
	}
}

func TestFlashProvider_awaitFill_filledWithUnreadableTotals_staysConfirmed(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	RegisterOrderPoll(client, "ord-odd", Order{Status: OrderStatusFilled, TransactionID: "sig", FilledTargetAmount: "n/a", FilledContraAmount: "5"})
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	fill, err := provider.AwaitFill(context.Background(),
		swapprovider.Submission{RequestID: "ord-odd", Receipt: testFunder, Request: testBuyRequest()}, TestPollConfig())

	if err == nil {
		t.Fatal("expected fill amount error")
	}
	if !fill.Confirmed {
		t.Fatal("funds moved: the fill must stay confirmed so the caller does not mark it failed")
	}
}

func TestFlashProvider_awaitFill_pollError_returnsError(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	RegisterOrderError(client, "ord-err", &APIError{Status: 500, Message: "internal"})
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	_, err := provider.AwaitFill(context.Background(),
		swapprovider.Submission{RequestID: "ord-err", Receipt: testFunder, Request: testBuyRequest()}, TestPollConfig())

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("AwaitFill() error = %v, want APIError", err)
	}
}

func TestFlashProvider_awaitFill_contextCancelled_stopsPolling(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.AwaitFill(ctx,
		swapprovider.Submission{RequestID: "ord-ctx", Receipt: testFunder, Request: testBuyRequest()},
		PollConfig{MaxAttempts: 3, Interval: time.Hour})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AwaitFill() error = %v, want context.Canceled", err)
	}
}
