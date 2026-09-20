package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/solana/swapchain"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

// These tests tick pollers that claim every due proposal in the database, so they do not run
// in parallel with each other.

type executePollerFixture struct {
	testApp    *workerTestApp
	fake       jupiter.Client
	xstocks    xstocks.Resolver
	governance *app.GovernanceService
	proposal   app.Proposal
	requestID  string
}

// newInstance is one API process: its own swap stack and poller over the shared database.
func (fx *executePollerFixture) newInstance(clock Clock) (*ProposalExecutePoller, *app.SwapService, *recordingJupiter, *swapchain.FakeReader) {
	venue := &recordingJupiter{Client: fx.fake}
	buy := app.NewBuyService(venue, fx.xstocks)
	swap := app.NewSwapService(fx.testApp.Store, buy, venue, fx.testApp.Privy, app.NewFakePrivyTreasurySigner(), "", app.NewSymbolResolver(nil))
	swap.SetPollConfigForTests(jupiter.TestPollConfig())
	chain := swapchain.NewFakeReader()
	swap.SetChainReader(chain)
	return NewProposalExecutePoller(fx.testApp.Store, app.NewExecuteOnPassService(swap, fx.testApp.Store), clock), swap, venue, chain
}

func (r *recordingJupiter) count(op, groupID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, call := range r.calls {
		if call == op+":"+groupID {
			n++
		}
	}
	return n
}

func seedExecutePollerFixture(t *testing.T, label string) *executePollerFixture {
	t.Helper()
	testApp := integrationWorkerApp(t)
	ctx := context.Background()
	fake := jupiter.NewFakeClient()
	resolver := xstocks.NewFakeResolver()
	governance := app.NewGovernanceService(testApp.Store, testApp.Privy)
	governance.SetBuyService(app.NewBuyService(fake, resolver))

	token := privy.AccessToken(testApp.ISO.UniqueToken(label))
	privy.RegisterToken(testApp.Privy, token, privy.Identity{PrivyUserID: testApp.ISO.UniquePrivyID(label), DisplayName: "Exactly Once"})
	session, err := app.NewSessionService(testApp.Store, testApp.Privy).OpenSession(ctx, string(token))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	testApp.ISO.TrackUser(session.UserID)

	const usdcAmount int64 = 2_000_000
	group, err := governance.CreateGroupWithRules(ctx, string(token), label+" "+testApp.ISO.Suffix(), app.DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	testApp.ISO.TrackGroup(group.GroupID)
	privy.SetTreasuryUSDCBalance(testApp.Privy, group.TreasuryAddress, usdcAmount)

	requestID := "req-" + label + "-" + testApp.ISO.Suffix()
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(fake, jupiter.AAPLxMint, usdcAmount, jupiter.BuyQuote{
		Routable: true, InputMint: jupiter.USDCMint, OutputMint: jupiter.AAPLxMint,
		InAmount: "2000000", OutAmount: "1000000", RequestID: requestID,
	})

	proposal, err := governance.CreateProposal(ctx, app.CreateProposalInput{
		GroupID: group.GroupID, ProposerID: session.UserID, Symbol: "AAPLx", UsdcMicros: usdcAmount,
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	passed, err := governance.CastVote(ctx, app.CastVoteInput{ProposalID: proposal.ID, VoterID: session.UserID, Choice: domain.VoteYes})
	if err != nil {
		t.Fatalf("cast vote: %v", err)
	}
	if passed.Status != app.ProposalPassed {
		t.Fatalf("status = %q, want passed", passed.Status)
	}
	return &executePollerFixture{testApp: testApp, fake: fake, xstocks: resolver, governance: governance, proposal: passed, requestID: requestID}
}

func (fx *executePollerFixture) registerSuccess(signature string) {
	jupiter.RegisterExecutePoll(fx.fake, fx.requestID, []jupiter.ExecuteResult{{
		Status: jupiter.ExecuteStatusSuccess, Code: 0, Signature: signature,
		InputAmountResult: "2000000", OutputAmountResult: "1000000",
	}})
}

func (fx *executePollerFixture) registerNeverConfirms() {
	polls := make([]jupiter.ExecuteResult, 50)
	for i := range polls {
		polls[i] = jupiter.ExecuteResult{Status: jupiter.ExecuteStatusPending, Code: -1}
	}
	jupiter.RegisterExecutePoll(fx.fake, fx.requestID, polls)
}

func (fx *executePollerFixture) transactionStatuses(t *testing.T) map[string]int {
	t.Helper()
	rows, err := fx.testApp.Store.ListTransactionsByGroupID(context.Background(), fx.proposal.GroupID)
	if err != nil {
		t.Fatalf("ListTransactionsByGroupID: %v", err)
	}
	statuses := map[string]int{}
	for _, row := range rows {
		statuses[row.Status]++
	}
	return statuses
}

func TestProposalExecutePoller_pollTimeoutThenTicks_noSecondBuy(t *testing.T) {
	// Arrange: the buy is submitted and the poll window closes without an answer.
	fx := seedExecutePollerFixture(t, "tick-after-timeout")
	fx.registerNeverConfirms()
	clock := &advancingClock{now: time.Now().UTC()}
	poller, swap, venue, chain := fx.newInstance(clock)
	ctx := context.Background()

	// Act: tick, then keep ticking well past the claim lease and any backoff.
	poller.tick(ctx)
	for i := 0; i < 3; i++ {
		clock.advance(ProposalExecuteLease + time.Hour)
		poller.tick(ctx)
	}

	// Assert
	if got := venue.count("execute_buy", fx.proposal.GroupID); got != 1 {
		t.Fatalf("venue received %d buy submits, want 1", got)
	}
	if statuses := fx.transactionStatuses(t); statuses[postgres.TransactionStatusPending] != 1 || len(statuses) != 1 {
		t.Fatalf("transaction statuses = %v, want exactly one pending", statuses)
	}

	// Act: the swap landed; the reconcile poller settles it and later ticks stay quiet.
	chain.LandEverything(map[string]int64{jupiter.USDCMint: -2_000_000, jupiter.AAPLxMint: 1_000_000})
	NewSwapReconcilePoller(swap, clock).tick(ctx)
	clock.advance(time.Hour)
	poller.tick(ctx)

	// Assert
	if statuses := fx.transactionStatuses(t); statuses[postgres.TransactionStatusConfirmed] != 1 || len(statuses) != 1 {
		t.Fatalf("transaction statuses = %v, want exactly one confirmed", statuses)
	}
	if got := venue.count("execute_buy", fx.proposal.GroupID); got != 1 {
		t.Fatalf("venue received %d buy submits, want 1", got)
	}
}

func TestProposalExecutePoller_backoffSurvivesRestartAndIsSharedAcrossInstances(t *testing.T) {
	// Arrange: Jupiter's order endpoint is down, so the first attempt fails before any submit.
	fx := seedExecutePollerFixture(t, "persisted-backoff")
	jupiter.RegisterQuoteBuyError(fx.fake, jupiter.AAPLxMint, 2_000_000, errors.New("jupiter: order status 503"))
	clock := &advancingClock{now: time.Now().UTC()}
	ctx := context.Background()
	poller, _, venue, _ := fx.newInstance(clock)

	// Act
	poller.tick(ctx)

	// Assert
	if got := venue.count("order_buy", fx.proposal.GroupID); got != 1 {
		t.Fatalf("first tick ordered %d times, want 1", got)
	}

	// Act: a restarted process and a second instance tick right away. Neither has any memory.
	restarted, _, restartedVenue, _ := fx.newInstance(clock)
	other, _, otherVenue, _ := fx.newInstance(clock)
	restarted.tick(ctx)
	other.tick(ctx)

	// Assert: the persisted backoff held both back.
	if got := restartedVenue.count("order_buy", fx.proposal.GroupID) + otherVenue.count("order_buy", fx.proposal.GroupID); got != 0 {
		t.Fatalf("instances without in-memory state ordered %d times inside the backoff, want 0", got)
	}

	// Act: Jupiter recovers and the backoff runs out.
	jupiter.RegisterQuoteBuy(fx.fake, jupiter.AAPLxMint, 2_000_000, jupiter.BuyQuote{
		Routable: true, InputMint: jupiter.USDCMint, OutputMint: jupiter.AAPLxMint,
		InAmount: "2000000", OutAmount: "1000000", RequestID: fx.requestID,
	})
	fx.registerSuccess("sig-backoff-" + fx.testApp.ISO.Suffix())
	clock.advance(app.ProposalExecuteBackoff(1) + time.Second)
	restarted.tick(ctx)

	// Assert
	if statuses := fx.transactionStatuses(t); statuses[postgres.TransactionStatusConfirmed] != 1 || len(statuses) != 1 {
		t.Fatalf("transaction statuses = %v, want exactly one confirmed", statuses)
	}
}

func TestProposalExecutePoller_twoInstancesTickTogether_oneBuy(t *testing.T) {
	fx := seedExecutePollerFixture(t, "two-pollers")
	fx.registerSuccess("sig-two-pollers-" + fx.testApp.ISO.Suffix())
	clock := &advancingClock{now: time.Now().UTC()}
	ctx := context.Background()
	first, _, firstVenue, _ := fx.newInstance(clock)
	second, _, secondVenue, _ := fx.newInstance(clock)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, poller := range []*ProposalExecutePoller{first, second} {
		wg.Add(1)
		go func(poller *ProposalExecutePoller) {
			defer wg.Done()
			<-start
			poller.tick(ctx)
		}(poller)
	}
	close(start)
	wg.Wait()

	if got := firstVenue.count("execute_buy", fx.proposal.GroupID) + secondVenue.count("execute_buy", fx.proposal.GroupID); got != 1 {
		t.Fatalf("venue received %d buy submits from two instances, want 1", got)
	}
	if statuses := fx.transactionStatuses(t); statuses[postgres.TransactionStatusConfirmed] != 1 || len(statuses) != 1 {
		t.Fatalf("transaction statuses = %v, want exactly one confirmed", statuses)
	}
}

func TestSwapReconcilePoller_expiredSwapFailsThenPollerRetriesAfterBackoff(t *testing.T) {
	// Arrange: submitted, never observed, and it never landed.
	fx := seedExecutePollerFixture(t, "reconcile-expired")
	fx.registerNeverConfirms()
	clock := &advancingClock{now: time.Now().UTC()}
	ctx := context.Background()
	poller, swap, venue, chain := fx.newInstance(clock)
	reconciler := NewSwapReconcilePoller(swap, clock)
	poller.tick(ctx)

	// Act: too young to touch, even though the chain already says expired.
	chain.ExpireBlockhashes()
	reconciler.tick(ctx)
	if statuses := fx.transactionStatuses(t); statuses[postgres.TransactionStatusPending] != 1 {
		t.Fatalf("transaction statuses = %v, want the young swap left pending", statuses)
	}

	// Act: old enough.
	clock.advance(app.SwapReconcileMinAge + time.Minute)
	reconciler.tick(ctx)

	// Assert
	if statuses := fx.transactionStatuses(t); statuses[postgres.TransactionStatusFailed] != 1 || len(statuses) != 1 {
		t.Fatalf("transaction statuses = %v, want exactly one failed", statuses)
	}

	// Act: the poller retries only after the persisted backoff, with a fresh Jupiter order.
	fx.requestID += "-retry"
	jupiter.RegisterQuoteBuy(fx.fake, jupiter.AAPLxMint, 2_000_000, jupiter.BuyQuote{
		Routable: true, InputMint: jupiter.USDCMint, OutputMint: jupiter.AAPLxMint,
		InAmount: "2000000", OutAmount: "1000000", RequestID: fx.requestID,
	})
	fx.registerSuccess("sig-reconcile-retry-" + fx.testApp.ISO.Suffix())
	poller.tick(ctx)
	if got := venue.count("execute_buy", fx.proposal.GroupID); got != 1 {
		t.Fatalf("venue received %d buy submits inside the backoff, want 1", got)
	}
	clock.advance(ProposalExecuteLease + time.Hour)
	poller.tick(ctx)

	// Assert: one failed attempt, one confirmed buy.
	if got := venue.count("execute_buy", fx.proposal.GroupID); got != 2 {
		t.Fatalf("venue received %d buy submits, want 2", got)
	}
	statuses := fx.transactionStatuses(t)
	if statuses[postgres.TransactionStatusFailed] != 1 || statuses[postgres.TransactionStatusConfirmed] != 1 || len(statuses) != 2 {
		t.Fatalf("transaction statuses = %v, want one failed and one confirmed", statuses)
	}
}

type failingReconciler struct{ calls int }

func (f *failingReconciler) ReconcilePendingSwaps(ctx context.Context, now time.Time) (app.SwapReconcileSummary, error) {
	f.calls++
	return app.SwapReconcileSummary{}, errors.New("postgres: connection refused")
}

func TestSwapReconcilePoller_nilSafeAndSurvivesErrors(t *testing.T) {
	RunSwapReconcilePoller(context.Background(), nil, DefaultSwapReconcileInterval)
	NewSwapReconcilePoller(nil, nil).tick(context.Background())

	reconciler := &failingReconciler{}
	poller := NewSwapReconcilePoller(reconciler, nil)
	poller.tick(context.Background())
	poller.tick(context.Background())
	if reconciler.calls != 2 {
		t.Fatalf("reconcile calls = %d, want 2: an error must not stop later ticks", reconciler.calls)
	}
}

// advancingClock is a test clock the test moves forward.
type advancingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *advancingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *advancingClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
