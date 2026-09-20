package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/solana/swapchain"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

// venueJupiter wraps the fake Jupiter client to count what actually reaches the venue and to
// inject behaviour at the submit boundary.
type venueJupiter struct {
	jupiter.Client
	submits atomic.Int32
	// requestSuffix makes this instance's venue request ids distinct, as Jupiter's are.
	requestSuffix string
	beforeOrder   func()
	afterSubmit   func()
}

func (v *venueJupiter) OrderBuy(ctx context.Context, params jupiter.OrderBuyParams) (jupiter.BuyOrder, error) {
	if v.beforeOrder != nil {
		v.beforeOrder()
	}
	order, err := v.Client.OrderBuy(ctx, params)
	order.RequestID += v.requestSuffix
	return order, err
}

func (v *venueJupiter) QuoteSell(ctx context.Context, params jupiter.QuoteSellParams) (jupiter.SellQuote, error) {
	quote, err := v.Client.QuoteSell(ctx, params)
	quote.RequestID += v.requestSuffix
	return quote, err
}

func (v *venueJupiter) ExecuteBuy(ctx context.Context, params jupiter.ExecuteBuyParams) (jupiter.ExecuteResult, error) {
	v.submits.Add(1)
	result, err := v.Client.ExecuteBuy(ctx, params)
	if v.afterSubmit != nil {
		v.afterSubmit()
	}
	return result, err
}

func (v *venueJupiter) SellToUSDC(ctx context.Context, params jupiter.SellToUSDCParams) (jupiter.ExecuteResult, error) {
	v.submits.Add(1)
	return v.Client.SellToUSDC(ctx, params)
}

// exactlyOnceHarness is one API instance: its own swap service and chain view over the shared
// database and the shared venue.
type exactlyOnceHarness struct {
	executeOnPassHarness
	Venue *venueJupiter
	Chain *swapchain.FakeReader
}

func integrationExactlyOnceApp(t *testing.T) exactlyOnceHarness {
	t.Helper()
	h := integrationExecuteOnPassApp(t)
	venue := &venueJupiter{Client: h.App.Jupiter}
	chain := swapchain.NewFakeReader()
	h.App.Swap, h.ExecutePass = newSwapInstance(h, venue, chain)
	return exactlyOnceHarness{executeOnPassHarness: h, Venue: venue, Chain: chain}
}

// newSwapInstance builds the swap stack of one more API process against the same store.
func newSwapInstance(h executeOnPassHarness, venue jupiter.Client, chain *swapchain.FakeReader) (*SwapService, *ExecuteOnPassService) {
	swap := NewSwapService(h.App.Store, NewBuyService(venue, h.App.XStocks), venue, h.App.Privy, NewFakePrivyTreasurySigner(), "", h.App.Symbols)
	swap.SetPollConfigForTests(jupiter.TestPollConfig())
	swap.SetChainReader(chain)
	return swap, NewExecuteOnPassService(swap, h.App.Store)
}

// neverConfirms makes every poll of requestID come back Pending, so the inline poll times out
// after the signed transaction was submitted.
func neverConfirms(client jupiter.Client, requestID string) {
	polls := make([]jupiter.ExecuteResult, 50)
	for i := range polls {
		polls[i] = jupiter.ExecuteResult{Status: jupiter.ExecuteStatusPending, Code: -1}
	}
	jupiter.RegisterExecutePoll(client, requestID, polls)
}

func proposalTransactions(t *testing.T, store *postgres.Store, proposal Proposal) []postgres.TransactionRow {
	t.Helper()
	rows, err := store.ListTransactionsByGroupID(context.Background(), proposal.GroupID)
	if err != nil {
		t.Fatalf("ListTransactionsByGroupID: %v", err)
	}
	var out []postgres.TransactionRow
	for _, row := range rows {
		if row.ProposalID.Valid && row.ProposalID.String == proposal.ID {
			out = append(out, row)
		}
	}
	return out
}

func onlyProposalTransaction(t *testing.T, store *postgres.Store, proposal Proposal, wantStatus string) postgres.TransactionRow {
	t.Helper()
	rows := proposalTransactions(t, store, proposal)
	if len(rows) != 1 {
		t.Fatalf("proposal has %d transactions, want exactly 1: %+v", len(rows), rows)
	}
	if rows[0].Status != wantStatus {
		t.Fatalf("transaction status = %q, want %q", rows[0].Status, wantStatus)
	}
	return rows[0]
}

func TestExecuteOnPass_pollTimeoutAfterSubmit_neverBuysTwice(t *testing.T) {
	// Arrange: the signed buy is submitted, then Jupiter never reports it within the poll window.
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h.executeOnPassHarness, "poll-timeout")
	neverConfirms(h.App.Jupiter, testRequestID(h.App.ISO, "poll-timeout"))

	// Act
	_, err := h.ExecutePass.ExecuteOnPass(ctx, passed)

	// Assert: unknown is not failed. The row stays pending and holds the proposal.
	if !errors.Is(err, ErrSwapOutcomeUnknown) {
		t.Fatalf("ExecuteOnPass err = %v, want ErrSwapOutcomeUnknown", err)
	}
	pending := onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)

	// Act: the poller comes back around, repeatedly.
	for tick := 0; tick < 3; tick++ {
		if _, err := h.ExecutePass.ExecuteOnPass(ctx, passed); !errors.Is(err, ErrSwapInFlight) {
			t.Fatalf("tick %d: ExecuteOnPass err = %v, want ErrSwapInFlight", tick, err)
		}
	}

	// Assert: nothing else reached the venue.
	if got := h.Venue.submits.Load(); got != 1 {
		t.Fatalf("venue received %d submits, want 1: the buy was submitted again", got)
	}
	listed, err := h.App.Store.ListPassedProposalsPendingExecute(ctx, 500)
	if err != nil {
		t.Fatalf("ListPassedProposalsPendingExecute: %v", err)
	}
	for _, row := range listed {
		if row.ID == passed.ID {
			t.Fatal("proposal with a pending swap is still listed for execute")
		}
	}

	// Act: the transaction did land; the reconciler reads the fill off the chain.
	h.Chain.LandEverything(map[string]int64{jupiter.USDCMint: -1_990_000, jupiter.AAPLxMint: 990_000})
	outcome, err := h.App.Swap.ReconcilePendingSwap(ctx, pending.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReconcilePendingSwap: %v", err)
	}
	if outcome != "filled" {
		t.Fatalf("outcome = %q, want filled", outcome)
	}

	// Assert: one confirmed buy carrying the on-chain amounts, and execute is now idempotent.
	confirmed := onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusConfirmed)
	if confirmed.ID != pending.ID {
		t.Fatalf("confirmed row %q is not the pending row %q", confirmed.ID, pending.ID)
	}
	if confirmed.CostBasisPrice.Int64 != 1_990_000 || confirmed.CostBasisAmount.Int64 != 990_000 {
		t.Fatalf("fill = %d/%d, want the on-chain 1990000/990000", confirmed.CostBasisPrice.Int64, confirmed.CostBasisAmount.Int64)
	}
	result, err := h.ExecutePass.ExecuteOnPass(ctx, passed)
	if err != nil || result.Created || result.Transaction.ID != confirmed.ID {
		t.Fatalf("ExecuteOnPass after reconcile = %+v, %v, want the confirmed row and created=false", result, err)
	}
	if got := h.Venue.submits.Load(); got != 1 {
		t.Fatalf("venue received %d submits, want 1", got)
	}
}

func TestExecuteOnPass_pollErrorAfterSubmit_staysPending(t *testing.T) {
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h.executeOnPassHarness, "poll-error")
	jupiter.RegisterExecuteError(h.App.Jupiter, testRequestID(h.App.ISO, "poll-error"), errors.New("read tcp: connection reset by peer"))

	_, err := h.ExecutePass.ExecuteOnPass(ctx, passed)

	if !errors.Is(err, ErrSwapOutcomeUnknown) {
		t.Fatalf("ExecuteOnPass err = %v, want ErrSwapOutcomeUnknown", err)
	}
	onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)
}

func TestExecuteOnPass_jupiterReportsFailed_waitsForTheChain(t *testing.T) {
	// Jupiter reports timeouts and expiries as Failed; only the chain proves nothing landed.
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h.executeOnPassHarness, "venue-failed")
	jupiter.RegisterExecutePoll(h.App.Jupiter, testRequestID(h.App.ISO, "venue-failed"), []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusFailed, Code: -1006, Error: "transaction timed out"},
	})

	_, err := h.ExecutePass.ExecuteOnPass(ctx, passed)

	if !errors.Is(err, ErrSwapOutcomeUnknown) {
		t.Fatalf("ExecuteOnPass err = %v, want ErrSwapOutcomeUnknown", err)
	}
	onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)
}

func TestReconcile_blockhashExpiredAndNotFound_failsThenProposalRetriesOnce(t *testing.T) {
	// Arrange: submitted, never observed.
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h.executeOnPassHarness, "expired")
	requestID := testRequestID(h.App.ISO, "expired")
	neverConfirms(h.App.Jupiter, requestID)
	if _, err := h.ExecutePass.ExecuteOnPass(ctx, passed); !errors.Is(err, ErrSwapOutcomeUnknown) {
		t.Fatalf("ExecuteOnPass err = %v, want ErrSwapOutcomeUnknown", err)
	}
	pending := onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)
	now := time.Now().UTC()

	// Act: while the blockhash is still valid the swap can land, so it must stay pending.
	outcome, err := h.App.Swap.ReconcilePendingSwap(ctx, pending.ID, now)
	if err != nil || outcome != "unknown" {
		t.Fatalf("reconcile with a live blockhash = %q, %v, want unknown", outcome, err)
	}
	onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)

	// Act: the blockhash expires and the signature was never seen.
	h.Chain.ExpireBlockhashes()
	outcome, err = h.App.Swap.ReconcilePendingSwap(ctx, pending.ID, now)
	if err != nil || outcome != "failed" {
		t.Fatalf("reconcile with an expired blockhash = %q, %v, want failed", outcome, err)
	}

	// Assert: failed for good, and the retry backoff is persisted rather than remembered.
	onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusFailed)
	claimed, err := h.App.Store.ClaimPassedProposalsForExecute(ctx, now, time.Minute, 500)
	if err != nil {
		t.Fatalf("ClaimPassedProposalsForExecute: %v", err)
	}
	for _, row := range claimed {
		if row.Proposal.ID == passed.ID {
			t.Fatal("proposal was claimable immediately after a failed swap; want persisted backoff")
		}
	}

	// Act: the retry is a fresh order and goes through.
	h.Venue.requestSuffix = "-retry"
	jupiter.RegisterExecutePoll(h.App.Jupiter, requestID+"-retry", []jupiter.ExecuteResult{{
		Status:             jupiter.ExecuteStatusSuccess,
		Code:               0,
		Signature:          testTxSignature(h.App.ISO, "expired-retry"),
		InputAmountResult:  "2000000",
		OutputAmountResult: "1000000",
	}})
	result, err := h.ExecutePass.ExecuteOnPass(ctx, passed)
	if err != nil || !result.Created {
		t.Fatalf("retry ExecuteOnPass = %+v, %v, want a newly confirmed buy", result, err)
	}

	// Assert: one failed attempt, one confirmed buy, two submits in total.
	rows := proposalTransactions(t, h.App.Store, passed)
	statuses := map[string]int{}
	for _, row := range rows {
		statuses[row.Status]++
	}
	if len(rows) != 2 || statuses[postgres.TransactionStatusFailed] != 1 || statuses[postgres.TransactionStatusConfirmed] != 1 {
		t.Fatalf("transaction statuses = %v, want one failed and one confirmed", statuses)
	}
	if got := h.Venue.submits.Load(); got != 2 {
		t.Fatalf("venue received %d submits, want 2", got)
	}
}

func TestReconcile_chainUnavailable_leavesSwapPending(t *testing.T) {
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h.executeOnPassHarness, "rpc-down")
	neverConfirms(h.App.Jupiter, testRequestID(h.App.ISO, "rpc-down"))
	if _, err := h.ExecutePass.ExecuteOnPass(ctx, passed); !errors.Is(err, ErrSwapOutcomeUnknown) {
		t.Fatalf("ExecuteOnPass err = %v, want ErrSwapOutcomeUnknown", err)
	}
	pending := onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)
	h.Chain.ExpireBlockhashes()
	h.Chain.SetError(errors.New("solana rpc status 429"))

	_, err := h.App.Swap.ReconcilePendingSwap(ctx, pending.ID, time.Now().UTC())

	if err == nil {
		t.Fatal("ReconcilePendingSwap err = nil, want the RPC error")
	}
	onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)
}

func TestExecuteOnPass_crashBetweenSubmitAndRecord_ledgerStillSeesTheSwap(t *testing.T) {
	// Arrange: the process dies the instant /execute returns.
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h.executeOnPassHarness, "crash")
	h.Venue.afterSubmit = func() { panic("process killed after submit") }

	// Act
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected the simulated crash")
			}
		}()
		_, _ = h.ExecutePass.ExecuteOnPass(ctx, passed)
	}()

	// Assert: the intent was durable before the submit, with what the chain lookup needs.
	pending := onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)
	intent, found, err := h.App.Store.GetPendingSwapByID(ctx, pending.ID)
	if err != nil || !found {
		t.Fatalf("GetPendingSwapByID: found=%v err=%v", found, err)
	}
	if !intent.SignedTxSignature.Valid || !intent.SignedTxBlockhash.Valid || intent.Provider.String != "jupiter" {
		t.Fatalf("intent = %+v, want provider, signed tx signature and blockhash recorded before submit", intent)
	}

	// Act: a fresh process boots, its poller ticks, and its reconciler finds the swap on chain.
	chain := swapchain.NewFakeReader()
	venue := &venueJupiter{Client: h.App.Jupiter}
	restartedSwap, restartedExec := newSwapInstance(h.executeOnPassHarness, venue, chain)
	if _, err := restartedExec.ExecuteOnPass(ctx, passed); !errors.Is(err, ErrSwapInFlight) {
		t.Fatalf("restarted ExecuteOnPass err = %v, want ErrSwapInFlight", err)
	}
	chain.Land(intent.SignedTxSignature.String, map[string]int64{jupiter.USDCMint: -2_000_000, jupiter.AAPLxMint: 1_000_000})
	summary, err := restartedSwap.ReconcilePendingSwaps(ctx, time.Now().UTC().Add(SwapReconcileMinAge+time.Minute))
	if err != nil {
		t.Fatalf("ReconcilePendingSwaps: %v", err)
	}
	if summary.Confirmed < 1 {
		t.Fatalf("reconcile summary = %+v, want the crashed swap confirmed", summary)
	}

	// Assert: holdings come from the ledger, and the ledger now has the swap exactly once.
	confirmed := onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusConfirmed)
	if confirmed.TxSignature.String != intent.SignedTxSignature.String {
		t.Fatalf("tx_signature = %q, want the signed transaction %q", confirmed.TxSignature.String, intent.SignedTxSignature.String)
	}
	holdings, err := h.App.Store.ListNetTokenHoldingsByGroup(ctx, passed.GroupID)
	if err != nil {
		t.Fatalf("ListNetTokenHoldingsByGroup: %v", err)
	}
	if len(holdings) != 1 || holdings[0].Mint != jupiter.AAPLxMint || holdings[0].Amount != 1_000_000 {
		t.Fatalf("holdings = %+v, want 1000000 AAPLx", holdings)
	}
	if got := h.Venue.submits.Load() + venue.submits.Load(); got != 1 {
		t.Fatalf("venue received %d submits across both processes, want 1", got)
	}
}

func TestReconcilePendingSwaps_skipsSwapsAnExecutorMayStillBePolling(t *testing.T) {
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h.executeOnPassHarness, "too-young")
	neverConfirms(h.App.Jupiter, testRequestID(h.App.ISO, "too-young"))
	if _, err := h.ExecutePass.ExecuteOnPass(ctx, passed); !errors.Is(err, ErrSwapOutcomeUnknown) {
		t.Fatalf("ExecuteOnPass err = %v, want ErrSwapOutcomeUnknown", err)
	}
	h.Chain.ExpireBlockhashes()

	if _, err := h.App.Swap.ReconcilePendingSwaps(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("ReconcilePendingSwaps: %v", err)
	}

	onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)
}

func TestExecuteOnPass_twoInstancesExecuteConcurrently_oneSubmit(t *testing.T) {
	// Arrange: two API instances (rolling deploy) share the database and the venue.
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h.executeOnPassHarness, "two-instances")

	// Both instances get past the "anything in flight?" check before either records an intent.
	var atOrder sync.WaitGroup
	atOrder.Add(2)
	released := make(chan struct{})
	barrier := func() {
		atOrder.Done()
		<-released
	}
	first := &venueJupiter{Client: h.App.Jupiter, beforeOrder: barrier, requestSuffix: "-instance-1"}
	second := &venueJupiter{Client: h.App.Jupiter, beforeOrder: barrier, requestSuffix: "-instance-2"}
	for _, venue := range []*venueJupiter{first, second} {
		jupiter.RegisterExecutePoll(h.App.Jupiter, testRequestID(h.App.ISO, "two-instances")+venue.requestSuffix, []jupiter.ExecuteResult{{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          testTxSignature(h.App.ISO, "two-instances"+venue.requestSuffix),
			InputAmountResult:  "2000000",
			OutputAmountResult: "1000000",
		}})
	}
	_, firstExec := newSwapInstance(h.executeOnPassHarness, first, swapchain.NewFakeReader())
	_, secondExec := newSwapInstance(h.executeOnPassHarness, second, swapchain.NewFakeReader())

	// Act
	results := make([]ExecuteOnPassResult, 2)
	errs := make([]error, 2)
	var done sync.WaitGroup
	for i, exec := range []*ExecuteOnPassService{firstExec, secondExec} {
		done.Add(1)
		go func(i int, exec *ExecuteOnPassService) {
			defer done.Done()
			results[i], errs[i] = exec.ExecuteOnPass(ctx, passed)
		}(i, exec)
	}
	atOrder.Wait()
	close(released)
	done.Wait()

	// Assert: one signed transaction reached the venue; the loser saw the slot taken.
	if got := first.submits.Load() + second.submits.Load(); got != 1 {
		t.Fatalf("venue received %d submits, want exactly 1", got)
	}
	created := 0
	for i, err := range errs {
		switch {
		case err == nil && results[i].Created:
			created++
		case err == nil, errors.Is(err, ErrSwapInFlight):
		default:
			t.Fatalf("instance %d: err = %v, want success or ErrSwapInFlight", i, err)
		}
	}
	if created != 1 {
		t.Fatalf("%d instances created the buy, want exactly 1", created)
	}
	onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusConfirmed)
}

func TestExecuteOnPass_sellPollTimeout_staysPendingAndFailedSellRetries(t *testing.T) {
	// Arrange: a treasury holding and a passed sell whose poll never confirms.
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed, requestID := seedPassedSellProposal(t, h.executeOnPassHarness, "sell-timeout")
	neverConfirms(h.App.Jupiter, requestID)

	// Act
	_, err := h.ExecutePass.ExecuteOnPass(ctx, passed)

	// Assert: same rules as a buy.
	if !errors.Is(err, ErrSwapOutcomeUnknown) {
		t.Fatalf("ExecuteOnPass sell err = %v, want ErrSwapOutcomeUnknown", err)
	}
	pending := onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)
	if _, err := h.ExecutePass.ExecuteOnPass(ctx, passed); !errors.Is(err, ErrSwapInFlight) {
		t.Fatalf("second ExecuteOnPass sell err = %v, want ErrSwapInFlight", err)
	}
	if got := h.Venue.submits.Load(); got != 1 {
		t.Fatalf("venue received %d sell submits, want 1", got)
	}

	// Act: definitively failed, then retried.
	h.Chain.ExpireBlockhashes()
	if outcome, err := h.App.Swap.ReconcilePendingSwap(ctx, pending.ID, time.Now().UTC()); err != nil || outcome != "failed" {
		t.Fatalf("reconcile = %q, %v, want failed", outcome, err)
	}
	h.Venue.requestSuffix = "-retry"
	jupiter.RegisterExecutePoll(h.App.Jupiter, requestID+"-retry", []jupiter.ExecuteResult{{
		Status:             jupiter.ExecuteStatusSuccess,
		Code:               0,
		Signature:          testTxSignature(h.App.ISO, "sell-timeout-retry"),
		InputAmountResult:  "100000000",
		OutputAmountResult: "10000000",
	}})
	result, err := h.ExecutePass.ExecuteOnPass(ctx, passed)
	if err != nil || !result.Created || result.Transaction.Action != postgres.TransactionActionSell {
		t.Fatalf("retry ExecuteOnPass sell = %+v, %v, want a newly confirmed sell", result, err)
	}
}

func seedPassedSellProposal(t *testing.T, h executeOnPassHarness, label string) (Proposal, string) {
	t.Helper()
	ctx := context.Background()
	sessions := NewSessionService(h.App.Store, h.App.Privy)
	user := openTestSession(t, h.App.ISO, sessions, h.App.Privy, label, "Sell Exec")
	created, err := h.Governance.CreateGroupWithRules(ctx, h.App.ISO.UniqueToken(label), testGroupName(h.App.ISO, label), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.App.ISO.TrackGroup(created.GroupID)

	const held = int64(100_000_000)
	xstocks.RegisterSolanaMint(h.App.XStocks, "AAPLx", jupiter.AAPLxMint)
	if _, _, err := h.App.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          created.GroupID,
		Amount:           10_000_000,
		InputMint:        jupiter.USDCMint,
		OutputMint:       jupiter.AAPLxMint,
		TxSignature:      testTxSignature(h.App.ISO, label+"-buy"),
		ExecuteRequestID: testRequestID(h.App.ISO, label+"-buy"),
		CostBasisPrice:   10_000_000,
		CostBasisAmount:  held,
	}); err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}
	requestID := testRequestID(h.App.ISO, label)
	jupiter.RegisterSellQuote(h.App.Jupiter, jupiter.AAPLxMint, held, jupiter.SellQuote{
		Routable:   true,
		InputMint:  jupiter.AAPLxMint,
		OutputMint: jupiter.USDCMint,
		InAmount:   "100000000",
		OutAmount:  "10000000",
		RequestID:  requestID,
	})
	treasury, err := h.App.Privy.EnsureTreasury(ctx, privy.GroupID(created.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	privy.SetTreasuryUSDCBalance(h.App.Privy, treasury.SolanaAddress, 1_000_000)

	proposal, err := h.Governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:     created.GroupID,
		ProposerID:  user.UserID,
		Symbol:      "AAPLx",
		Kind:        ProposalKindSell,
		TokenAmount: held,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	passed, err := h.Governance.CastVote(ctx, CastVoteInput{ProposalID: proposal.ID, VoterID: user.UserID, Choice: domain.VoteYes})
	if err != nil {
		t.Fatalf("CastVote: %v", err)
	}
	if passed.Status != ProposalPassed {
		t.Fatalf("status = %q, want passed", passed.Status)
	}
	return passed, requestID
}

func TestDevExecuteBuy_navSnapshotUsesOnChainTreasuryUSDC(t *testing.T) {
	// Arrange: the chain says the treasury holds 7 USDC after the buy settles.
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed := seedPassedExecuteProposal(t, h.executeOnPassHarness, "snapshot-chain")
	treasury, err := h.App.Privy.EnsureTreasury(ctx, privy.GroupID(passed.GroupID))
	if err != nil {
		t.Fatalf("EnsureTreasury: %v", err)
	}
	const onChainUSDC int64 = 7_000_000
	privy.SetTreasuryUSDCBalance(h.App.Privy, treasury.SolanaAddress, onChainUSDC)

	// Act
	if _, err := h.ExecutePass.ExecuteOnPass(ctx, passed); err != nil {
		t.Fatalf("ExecuteOnPass: %v", err)
	}

	// Assert: every snapshot the buy wrote prices cash from the chain, not from process memory.
	want, err := h.App.Store.ComputeNavSnapshotValues(ctx, passed.GroupID, onChainUSDC)
	if err != nil {
		t.Fatalf("ComputeNavSnapshotValues: %v", err)
	}
	snapshots, err := h.App.Store.ListNavSnapshotsByGroup(ctx, passed.GroupID)
	if err != nil {
		t.Fatalf("ListNavSnapshotsByGroup: %v", err)
	}
	checked := 0
	for _, snapshot := range snapshots {
		if snapshot.Reason != postgres.NavSnapshotReasonTransactionConfirm {
			continue
		}
		checked++
		if snapshot.PotNavMicros != want.PotNavMicros {
			t.Fatalf("snapshot pot NAV = %d, want %d (on-chain USDC %d plus holdings)", snapshot.PotNavMicros, want.PotNavMicros, onChainUSDC)
		}
	}
	if checked == 0 {
		t.Fatal("expected a transaction-confirm NAV snapshot")
	}
}

func TestSwapService_concurrentSwapsShareNoMutableState(t *testing.T) {
	// One swap service serves every group at once; run under -race.
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	const swaps = 6
	proposals := make([]Proposal, swaps)
	for i := range proposals {
		proposals[i] = seedPassedExecuteProposalForAmount(t, h.executeOnPassHarness, "concurrent-"+string(rune('a'+i)), 2_000_000+int64(i))
	}

	var wg sync.WaitGroup
	errs := make([]error, swaps)
	for i := range proposals {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = h.ExecutePass.ExecuteOnPass(ctx, proposals[i])
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("swap %d: %v", i, err)
		}
		onlyProposalTransaction(t, h.App.Store, proposals[i], postgres.TransactionStatusConfirmed)
	}
}

func TestRetryFailedSwap_proposalSell_sharesTheProposalSlotWithThePoller(t *testing.T) {
	// Arrange: a proposal sell that definitively failed.
	h := integrationExactlyOnceApp(t)
	ctx := context.Background()
	passed, requestID := seedPassedSellProposal(t, h.executeOnPassHarness, "sell-manual-retry")
	neverConfirms(h.App.Jupiter, requestID)
	if _, err := h.ExecutePass.ExecuteOnPass(ctx, passed); !errors.Is(err, ErrSwapOutcomeUnknown) {
		t.Fatalf("ExecuteOnPass sell err = %v, want ErrSwapOutcomeUnknown", err)
	}
	failed := onlyProposalTransaction(t, h.App.Store, passed, postgres.TransactionStatusPending)
	h.Chain.ExpireBlockhashes()
	if outcome, err := h.App.Swap.ReconcilePendingSwap(ctx, failed.ID, time.Now().UTC()); err != nil || outcome != "failed" {
		t.Fatalf("reconcile = %q, %v, want failed", outcome, err)
	}

	// Act: a member taps retry, then the execute poller comes around.
	h.Venue.requestSuffix = "-manual"
	jupiter.RegisterExecutePoll(h.App.Jupiter, requestID+"-manual", []jupiter.ExecuteResult{{
		Status:             jupiter.ExecuteStatusSuccess,
		Code:               0,
		Signature:          testTxSignature(h.App.ISO, "sell-manual-retry"),
		InputAmountResult:  "100000000",
		OutputAmountResult: "10000000",
	}})
	retried, err := h.App.Swap.RetryFailedSwap(ctx, RetryFailedSwapRequest{TransactionID: failed.ID, UserID: passed.ProposerID})
	if err != nil || !retried.Created {
		t.Fatalf("RetryFailedSwap = %+v, %v, want a newly confirmed sell", retried, err)
	}
	h.Venue.requestSuffix = "-poller"
	result, err := h.ExecutePass.ExecuteOnPass(ctx, passed)

	// Assert: the poller finds the manual retry's sell instead of selling again.
	if err != nil || result.Created || result.Transaction.ID != retried.Transaction.ID {
		t.Fatalf("ExecuteOnPass after manual retry = %+v, %v, want the retried sell and created=false", result, err)
	}
	if got := h.Venue.submits.Load(); got != 2 {
		t.Fatalf("venue received %d sell submits, want 2 (the failed attempt and one retry)", got)
	}
}
