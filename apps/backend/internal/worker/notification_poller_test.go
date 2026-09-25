package worker

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

type pollerPushes struct {
	mu   sync.Mutex
	msgs []app.PushMessage
}

func (p *pollerPushes) Send(_ context.Context, msgs []app.PushMessage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, msgs...)
}

// notificationPollerFixture is a three-member cabal with a notifier and a governance service
// whose clock the test moves.
type notificationPollerFixture struct {
	app        *workerTestApp
	governance *app.GovernanceService
	notifier   *app.Notifier
	jupiter    jupiter.Client
	resolver   xstocks.Resolver
	groupID    string
	members    []string
	tokens     []string
	now        time.Time
}

func newNotificationPollerFixture(t *testing.T, voteExpiry time.Duration) *notificationPollerFixture {
	t.Helper()
	testApp := integrationWorkerApp(t)
	f := &notificationPollerFixture{
		app: testApp,
		// Far from any other test's rows, so the poller's windows only hold this test's proposals.
		now:      time.Unix(1_950_000_000+int64(len(testApp.ISO.Suffix()))*1000, 0).UTC(),
		jupiter:  jupiter.NewFakeClient(),
		resolver: xstocks.NewFakeResolver(),
	}
	f.notifier = app.NewNotifier(testApp.Store, &pollerPushes{}).SendPushInline()
	f.notifier.SetClock(func() time.Time { return f.now })
	f.governance = app.NewGovernanceService(testApp.Store, testApp.Privy)
	f.governance.SetBuyService(app.NewBuyService(f.jupiter, f.resolver))
	f.governance.SetNotifier(f.notifier)
	f.governance.SetClock(func() time.Time { return f.now })

	ctx := context.Background()
	sessions := app.NewSessionService(testApp.Store, testApp.Privy)
	for _, name := range []string{"Jordan", "Priya", "Sam"} {
		token := testApp.ISO.UniqueToken(name)
		privy.RegisterToken(testApp.Privy, privy.AccessToken(token), privy.Identity{PrivyUserID: testApp.ISO.UniquePrivyID(name), DisplayName: name})
		session, err := sessions.OpenSession(ctx, token)
		if err != nil {
			t.Fatalf("open session: %v", err)
		}
		testApp.ISO.TrackUser(session.UserID)
		f.members = append(f.members, session.UserID)
		f.tokens = append(f.tokens, token)
	}
	rules := app.DefaultGroupRules()
	rules.VoteExpirySeconds = domain.VoteExpirySeconds(voteExpiry.Seconds())
	group, err := f.governance.CreateGroupWithRules(ctx, f.tokens[0], "Poller "+testApp.ISO.Suffix(), rules)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	testApp.ISO.TrackGroup(group.GroupID)
	f.groupID = group.GroupID
	privy.SetTreasuryUSDCBalance(testApp.Privy, group.TreasuryAddress, 1_000_000_000)
	for _, token := range f.tokens[1:] {
		if _, err := f.governance.JoinGroup(ctx, token, f.groupID); err != nil {
			t.Fatalf("join: %v", err)
		}
	}
	return f
}

func (f *notificationPollerFixture) propose(t *testing.T, usdc int64) postgres.ProposalRow {
	t.Helper()
	mint := "MintAAPLx"
	xstocks.RegisterSolanaMint(f.resolver, "AAPLx", mint)
	jupiter.RegisterQuoteBuy(f.jupiter, mint, usdc, jupiter.BuyQuote{
		Routable: true, OutputMint: mint, InAmount: strconv.FormatInt(usdc, 10), OutAmount: strconv.FormatInt(usdc, 10),
	})
	p, err := f.governance.CreateProposal(context.Background(), app.CreateProposalInput{
		GroupID: f.groupID, ProposerID: f.members[0], Symbol: "AAPLx", UsdcMicros: usdc,
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	row, _, err := f.app.Store.GetProposalByID(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	return row
}

func (f *notificationPollerFixture) poller() *NotificationPoller {
	p := NewNotificationPoller(f.app.Store, f.governance, f.notifier, f.app.Privy, clockFunc(func() time.Time { return f.now }))
	// Other tests share the database; a large batch keeps their leftovers from crowding ours out.
	p.limit = 1000
	return p
}

func (f *notificationPollerFixture) count(t *testing.T, member int, kind string) int {
	t.Helper()
	rows, err := f.app.Store.ListNotifications(context.Background(), f.members[member], nil, 100)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	n := 0
	for _, r := range rows {
		if r.Kind == kind {
			n++
		}
	}
	return n
}

type clockFunc func() time.Time

func (c clockFunc) Now() time.Time { return c() }

func TestNotificationPoller_remindsNonVotersOnceAnHourBeforeClose(t *testing.T) {
	// Arrange: a one-day vote; Priya has voted, Jordan (who proposed) and Sam have not.
	f := newNotificationPollerFixture(t, 24*time.Hour)
	row := f.propose(t, 10_000_000)
	if _, err := f.governance.CastVote(context.Background(), app.CastVoteInput{ProposalID: row.ID, VoterID: f.members[1], Choice: domain.VoteYes}); err != nil {
		t.Fatalf("vote: %v", err)
	}
	poller := f.poller()

	// Act: two hours out nothing happens; 55 minutes out the reminder goes; the next tick repeats nothing.
	f.now = row.ExpiresAt.Add(-2 * time.Hour)
	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	early := f.count(t, 2, app.NotifyProposalExpiring)
	f.now = row.ExpiresAt.Add(-55 * time.Minute)
	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	f.now = f.now.Add(time.Minute)
	if err := poller.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// Assert
	if early != 0 {
		t.Fatalf("reminded two hours out")
	}
	want := map[int]int{0: 1, 1: 0, 2: 1}
	for member, n := range want {
		if got := f.count(t, member, app.NotifyProposalExpiring); got != n {
			t.Fatalf("member %d got %d closing reminders, want %d", member, got, n)
		}
	}
}

func TestNotificationPoller_shortVotesGetNoClosingReminder(t *testing.T) {
	// A 90-minute vote was announced moments ago; a second buzz would be noise.
	f := newNotificationPollerFixture(t, 90*time.Minute)
	row := f.propose(t, 10_000_000)
	f.now = row.ExpiresAt.Add(-30 * time.Minute)

	if err := f.poller().Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if got := f.count(t, 1, app.NotifyProposalExpiring); got != 0 {
		t.Fatalf("got %d reminders on a short vote", got)
	}
}

func TestNotificationPoller_closesVotesThatRanOutOfTime(t *testing.T) {
	// Arrange: nobody opens the proposal after its deadline.
	f := newNotificationPollerFixture(t, 24*time.Hour)
	row := f.propose(t, 10_000_000)
	f.now = row.ExpiresAt.Add(time.Minute)

	// Act
	if err := f.poller().Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// Assert: the vote is closed and every member heard.
	after, _, err := f.app.Store.GetProposalByID(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if after.Status != domain.ProposalExpired {
		t.Fatalf("status = %s, want expired", after.Status)
	}
	for member := range f.members {
		if got := f.count(t, member, app.NotifyProposalExpired); got != 1 {
			t.Fatalf("member %d got %d expired notifications, want 1", member, got)
		}
	}
}

func TestNotificationPoller_watchesTheBalanceOfMembersWithPushOn(t *testing.T) {
	// Arrange: Priya turned push on; Sam did not.
	f := newNotificationPollerFixture(t, 24*time.Hour)
	ctx := context.Background()
	store := f.app.Store
	if err := store.UpsertDeviceToken(ctx, f.members[1], "ab"+strconv.FormatInt(f.now.Unix(), 16)+"0000000000000000000000000000000000000000000000", "ios", "debug", f.now); err != nil {
		t.Fatalf("register device: %v", err)
	}
	wallets := map[int]string{}
	for _, member := range []int{1, 2} {
		wallet, _, err := store.GetMemberWalletByUserID(ctx, f.members[member])
		if err != nil {
			t.Fatalf("wallet: %v", err)
		}
		wallets[member] = wallet.SolanaAddress
		privy.SetMemberUSDCBalance(f.app.Privy, wallet.SolanaAddress, 10_000_000)
	}
	poller := f.poller()

	// Act: the first read sets the mark; $500 arrives; a tick inside two minutes skips her;
	// the next one reads again.
	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	for _, member := range []int{1, 2} {
		privy.SetMemberUSDCBalance(f.app.Privy, wallets[member], 510_000_000)
	}
	f.now = f.now.Add(time.Minute)
	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	tooSoon := f.count(t, 1, app.NotifyFundsArrived)
	f.now = f.now.Add(2 * time.Minute)
	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("third tick: %v", err)
	}

	// Assert
	if tooSoon != 0 {
		t.Fatal("read the balance again inside two minutes")
	}
	if got := f.count(t, 1, app.NotifyFundsArrived); got != 1 {
		t.Fatalf("Priya got %d arrivals, want 1", got)
	}
	if got := f.count(t, 2, app.NotifyFundsArrived); got != 0 {
		t.Fatalf("Sam (no push) got %d arrivals from the background watch", got)
	}
}

func TestNotificationPoller_nilSafe(t *testing.T) {
	var p *NotificationPoller
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("nil tick: %v", err)
	}
}

func TestProposalExecutePoller_fillTellsEveryMemberWhatTheCabalBought(t *testing.T) {
	// Arrange: a passed $2 buy of Apple in a three-member cabal.
	f := newNotificationPollerFixture(t, 24*time.Hour)
	ctx := context.Background()
	const usdc int64 = 2_000_000
	requestID := "req-notify-" + f.app.ISO.Suffix()
	signature := "sig-notify-" + f.app.ISO.Suffix()
	xstocks.RegisterSolanaMint(f.resolver, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(f.jupiter, jupiter.AAPLxMint, usdc, jupiter.BuyQuote{
		Routable: true, InputMint: jupiter.USDCMint, OutputMint: jupiter.AAPLxMint, InAmount: "2000000", OutAmount: "1000000", RequestID: requestID,
	})
	jupiter.RegisterBuyOrder(f.jupiter, requestID, jupiter.BuyOrder{
		RequestID: requestID, Transaction: "unsigned-buy-tx", InAmount: "2000000", OutAmount: "1000000", InputMint: jupiter.USDCMint, OutputMint: jupiter.AAPLxMint,
	})
	jupiter.RegisterExecutePoll(f.jupiter, requestID, []jupiter.ExecuteResult{
		{Status: jupiter.ExecuteStatusSuccess, Code: 0, Signature: signature, InputAmountResult: "2000000", OutputAmountResult: "1000000"},
	})
	proposal, err := f.governance.CreateProposal(ctx, app.CreateProposalInput{GroupID: f.groupID, ProposerID: f.members[0], Symbol: "AAPLx", UsdcMicros: usdc})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	for _, member := range f.members[:2] {
		if _, err := f.governance.CastVote(ctx, app.CastVoteInput{ProposalID: proposal.ID, VoterID: member, Choice: domain.VoteYes}); err != nil {
			t.Fatalf("vote: %v", err)
		}
	}
	buy := app.NewBuyService(f.jupiter, f.resolver)
	swap := app.NewSwapService(f.app.Store, buy, f.jupiter, f.app.Privy, app.NewFakePrivyTreasurySigner(), "", app.NewSymbolResolver(nil))
	swap.SetPollConfigForTests(jupiter.TestPollConfig())
	executeOnPass := app.NewExecuteOnPassService(swap, f.app.Store)
	executeOnPass.SetNotifier(f.notifier)

	// Act: the execute poller fills it, twice over (the second tick finds it done).
	poller := NewProposalExecutePoller(f.app.Store, executeOnPass, nil)
	poller.tick(ctx)
	poller.tick(ctx)

	// Assert
	for member := range f.members {
		if got := f.count(t, member, app.NotifyTradeBought); got != 1 {
			t.Fatalf("member %d got %d trade_bought, want 1", member, got)
		}
	}
}
