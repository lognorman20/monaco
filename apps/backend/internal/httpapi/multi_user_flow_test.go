package httpapi

import (
	"context"
	"net/http"
	"testing"
)

// Multi-user product flows (issue #147). Each test signs in two or three distinct
// Privy identities against the same API and asserts what every token sees. The
// manual gold-sim counterpart is docs/multi-user-verification.md.

const (
	usdc5  = int64(5_000_000)
	usdc10 = int64(10_000_000)
	usdc20 = int64(20_000_000)
	usdc25 = int64(25_000_000)
	usdc30 = int64(30_000_000)
)

func TestMultiUserFlow_joinDepositProposeVote_boardsShowEachMembersPnL(t *testing.T) {
	t.Parallel()

	// Arrange: Alice creates, Bob joins by club id, both fund the pot.
	a := newMultiUserApp(t)
	alice := a.signIn(t, "flow-alice", "Alice")
	bob := a.signIn(t, "flow-bob", "Bob")
	c := a.createClub(t, alice, "Weekend Club")
	a.join(t, bob, c)
	a.deposit(t, alice, c, usdc10)
	a.deposit(t, bob, c, usdc30)

	// A joined member reaches the same club surfaces the creator does.
	getRec := a.call(t, a.Groups.GetGroupHandler, http.MethodGet, "/v1/groups/"+c.ID, bob.Token, "", "id", c.ID)
	requireStatus(t, getRec, http.StatusOK, "Bob GET /v1/groups/{id}")
	if got := decodeBody[getGroupResponse](t, getRec).TreasuryAddress; got != c.TreasuryAddress {
		t.Fatalf("Bob treasuryAddress = %q, want %q", got, c.TreasuryAddress)
	}
	treasuryRec := a.call(t, a.Deposits.GetTreasuryUsdcBalanceHandler, http.MethodGet, "/v1/groups/"+c.ID+"/treasury/usdc", bob.Token, "", "id", c.ID)
	requireStatus(t, treasuryRec, http.StatusOK, "Bob GET /v1/groups/{id}/treasury/usdc")
	if got := decodeBody[treasuryUsdcBalanceResponse](t, treasuryRec).UsdcBalance; got != usdc10+usdc30 {
		t.Fatalf("Bob treasury usdcBalance = %d, want %d", got, usdc10+usdc30)
	}
	a.registerRoutableAAPLx(usdc20)
	assetsRec := a.call(t, a.Catalog.SearchAssetsHandler, http.MethodGet, "/v1/groups/"+c.ID+"/assets?query=AAPL", bob.Token, "", "id", c.ID)
	requireStatus(t, assetsRec, http.StatusOK, "Bob GET /v1/groups/{id}/assets")
	quoteRec := a.call(t, a.Quotes.QuoteHandler, http.MethodPost, "/v1/groups/"+c.ID+"/quotes", bob.Token, `{"symbol":"AAPLx","usdc":20000000}`, "id", c.ID)
	requireStatus(t, quoteRec, http.StatusOK, "Bob POST /v1/groups/{id}/quotes")

	// Act: Alice proposes, Bob then Alice vote yes.
	proposalID := a.propose(t, alice, c, usdc20)

	listRec := a.call(t, a.Proposals.ListGroupProposalsHandler, http.MethodGet, "/v1/groups/"+c.ID+"/proposals?tab=open", bob.Token, "", "id", c.ID)
	requireStatus(t, listRec, http.StatusOK, "Bob GET /v1/groups/{id}/proposals")
	listed := decodeBody[listGroupProposalsResponse](t, listRec).Proposals
	if len(listed) != 1 || listed[0].ID != proposalID || listed[0].ProposerID != alice.UserID {
		t.Fatalf("Bob open proposals = %+v, want Alice's %s", listed, proposalID)
	}
	if !a.proposalDetail(t, bob, proposalID).CanVote {
		t.Fatal("Bob canVote = false before voting, want true")
	}

	requireStatus(t, a.vote(t, bob, proposalID, "yes"), http.StatusNoContent, "Bob POST /v1/proposals/{id}/votes")
	afterBob := a.proposalDetail(t, alice, proposalID)
	if afterBob.Status != "open" || afterBob.VoteSummary.YesCount != 1 || afterBob.VoteSummary.EligibleCount != 2 {
		t.Fatalf("after Bob's vote: status=%s summary=%+v, want open 1 yes of 2 eligible", afterBob.Status, afterBob.VoteSummary)
	}
	if len(afterBob.Votes) != 1 || afterBob.Votes[0].VoterID != bob.UserID || afterBob.Votes[0].Choice != "yes" {
		t.Fatalf("Alice sees votes %+v, want exactly Bob yes", afterBob.Votes)
	}

	requireStatus(t, a.vote(t, alice, proposalID, "yes"), http.StatusNoContent, "Alice POST /v1/proposals/{id}/votes")
	if status := a.proposalDetail(t, bob, proposalID).Status; status != "passed" {
		t.Fatalf("status after both votes = %s, want passed", status)
	}

	// The passed buy executes: $20 buys 10 AAPLx, which then marks at $3 (pot $50 on $40 in).
	a.buyAAPLxAtMark(t, c, usdc20, 10, 3_000_000)

	// Assert: member board lists both members with their own P&L, seen identically by both.
	for _, viewer := range []clubMember{alice, bob} {
		view := a.groupView(t, viewer, c)
		if view.PotTotalUsd != "50.00" {
			t.Fatalf("%s potTotalUsd = %q, want 50.00", viewer.Name, view.PotTotalUsd)
		}
		if len(view.Members) != 2 {
			t.Fatalf("%s member board len = %d, want 2", viewer.Name, len(view.Members))
		}
		aliceRow, bobRow := findViewMember(t, view, alice), findViewMember(t, view, bob)
		if aliceRow.DollarPnL != "+2.50" || bobRow.DollarPnL != "+7.50" {
			t.Fatalf("%s board dollarPnl alice=%s bob=%s, want +2.50 / +7.50", viewer.Name, aliceRow.DollarPnL, bobRow.DollarPnL)
		}
		requirePercent(t, viewer.Name+" board Alice", aliceRow.PercentReturn, "0.25")
		requirePercent(t, viewer.Name+" board Bob", bobRow.PercentReturn, "0.25")
	}

	// Each token's "you" slice is its own position, never the other member's.
	aliceYou, bobYou := a.groupView(t, alice, c).You, a.groupView(t, bob, c).You
	if aliceYou.EquityUsd != "12.50" || aliceYou.SlicePercent != "0.25" || aliceYou.DollarPnL != "+2.50" {
		t.Fatalf("Alice you = %+v, want 12.50 equity, 0.25 slice, +2.50", aliceYou)
	}
	if bobYou.EquityUsd != "37.50" || bobYou.SlicePercent != "0.75" || bobYou.DollarPnL != "+7.50" {
		t.Fatalf("Bob you = %+v, want 37.50 equity, 0.75 slice, +7.50", bobYou)
	}

	// Home People board and dashboard leaderboard carry per-user P&L for both viewers.
	for _, viewer := range []clubMember{alice, bob} {
		people := a.home(t, viewer).People
		if got := findPerson(t, people, alice).DollarPnL; got != "+2.50" {
			t.Fatalf("%s /v1/home Alice dollarPnl = %s, want +2.50", viewer.Name, got)
		}
		if got := findPerson(t, people, bob).DollarPnL; got != "+7.50" {
			t.Fatalf("%s /v1/home Bob dollarPnl = %s, want +7.50", viewer.Name, got)
		}
		leaderboard := a.dashboard(t, viewer).Leaderboard
		if leaderboard.Range != "ALL" {
			t.Fatalf("%s leaderboard range = %q, want ALL", viewer.Name, leaderboard.Range)
		}
		if got := findPerson(t, leaderboard.People, alice).DollarPnL; got != "+2.50" {
			t.Fatalf("%s dashboard leaderboard Alice dollarPnl = %s, want +2.50", viewer.Name, got)
		}
		if got := findPerson(t, leaderboard.People, bob).DollarPnL; got != "+7.50" {
			t.Fatalf("%s dashboard leaderboard Bob dollarPnl = %s, want +7.50", viewer.Name, got)
		}
	}

	aliceDash, bobDash := a.dashboard(t, alice), a.dashboard(t, bob)
	if aliceDash.NetWorthUsd != "12.50" || aliceDash.NetWorthDollarPnl != "+2.50" {
		t.Fatalf("Alice net worth = %s (%s), want 12.50 (+2.50)", aliceDash.NetWorthUsd, aliceDash.NetWorthDollarPnl)
	}
	if bobDash.NetWorthUsd != "37.50" || bobDash.NetWorthDollarPnl != "+7.50" {
		t.Fatalf("Bob net worth = %s (%s), want 37.50 (+7.50)", bobDash.NetWorthUsd, bobDash.NetWorthDollarPnl)
	}
	if row := findMyGroup(t, aliceDash.MyGroups, c); row.EquityUsd != "12.50" || row.SlicePercent != "0.25" {
		t.Fatalf("Alice myGroups row = %+v, want 12.50 equity at 0.25 slice", row)
	}
	if row := findMyGroup(t, bobDash.MyGroups, c); row.EquityUsd != "37.50" || row.SlicePercent != "0.75" {
		t.Fatalf("Bob myGroups row = %+v, want 37.50 equity at 0.75 slice", row)
	}
}

func TestMultiUserFlow_onlyJoinerFunded_creatorPnLStaysFlat(t *testing.T) {
	t.Parallel()

	// Arrange: Alice creates, Bob joins; only Bob funds, and the pot gains.
	a := newMultiUserApp(t)
	alice := a.signIn(t, "iso-alice", "Alice")
	bob := a.signIn(t, "iso-bob", "Bob")
	c := a.createClub(t, alice, "Bob Funded Club")
	a.join(t, bob, c)
	a.deposit(t, bob, c, usdc25)
	a.buyAAPLxAtMark(t, c, usdc10, 5, 4_000_000) // $10 buys 5 AAPLx, marked at $4 → pot $35.

	// Act
	aliceView := a.groupView(t, alice, c)
	bobView := a.groupView(t, bob, c)
	aliceHome, bobHome := a.home(t, alice), a.home(t, bob)
	aliceDash, bobDash := a.dashboard(t, alice), a.dashboard(t, bob)

	// Assert: none of Bob's deposit or gain lands in Alice's slice.
	if aliceView.You.EquityUsd != "0.00" || aliceView.You.DollarPnL != "+0.00" || aliceView.You.SlicePercent != "0" {
		t.Fatalf("Alice you = %+v, want 0.00 equity, +0.00, 0 slice", aliceView.You)
	}
	requirePercent(t, "Alice you", aliceView.You.PercentReturn, "")
	if bobView.You.EquityUsd != "35.00" || bobView.You.DollarPnL != "+10.00" || bobView.You.SlicePercent != "1" {
		t.Fatalf("Bob you = %+v, want 35.00 equity, +10.00, slice 1", bobView.You)
	}
	requirePercent(t, "Bob you", bobView.You.PercentReturn, "0.4")

	for _, view := range []groupViewResponse{aliceView, bobView} {
		if len(view.Members) != 2 {
			t.Fatalf("member board len = %d, want 2 (unfunded members still listed)", len(view.Members))
		}
		if row := findViewMember(t, view, alice); row.DollarPnL != "+0.00" || row.PercentReturn != nil {
			t.Fatalf("member board Alice = %+v, want +0.00 and nil percent", row)
		}
		if row := findViewMember(t, view, bob); row.DollarPnL != "+10.00" {
			t.Fatalf("member board Bob = %+v, want +10.00", row)
		}
	}

	// People board: separate dollarPnl per user, identical from either token.
	for _, home := range []homeResponse{aliceHome, bobHome} {
		aliceRow := findPerson(t, home.People, alice)
		if aliceRow.DollarPnL != "+0.00" || aliceRow.PercentReturn != nil {
			t.Fatalf("people board Alice = %+v, want +0.00 and nil percent", aliceRow)
		}
		bobRow := findPerson(t, home.People, bob)
		if bobRow.DollarPnL != "+10.00" {
			t.Fatalf("people board Bob = %+v, want +10.00", bobRow)
		}
		requirePercent(t, "people board Bob", bobRow.PercentReturn, "0.4")
	}

	// Dashboard: Alice's net worth and leaderboard row stay flat; Bob's carry the gain.
	if aliceDash.NetWorthUsd != "0.00" || aliceDash.NetWorthDollarPnl != "+0.00" {
		t.Fatalf("Alice net worth = %s (%s), want 0.00 (+0.00)", aliceDash.NetWorthUsd, aliceDash.NetWorthDollarPnl)
	}
	requirePercent(t, "Alice net worth", aliceDash.NetWorthPercentReturn, "")
	if row := findMyGroup(t, aliceDash.MyGroups, c); row.EquityUsd != "0.00" || row.DollarPnL != "+0.00" || row.SlicePercent != "0" {
		t.Fatalf("Alice myGroups row = %+v, want 0.00 equity, +0.00, 0 slice", row)
	}
	if bobDash.NetWorthUsd != "35.00" || bobDash.NetWorthDollarPnl != "+10.00" {
		t.Fatalf("Bob net worth = %s (%s), want 35.00 (+10.00)", bobDash.NetWorthUsd, bobDash.NetWorthDollarPnl)
	}
	for _, dash := range []homeDashboardResponse{aliceDash, bobDash} {
		if row := findPerson(t, dash.Leaderboard.People, alice); row.DollarPnL != "+0.00" || row.PercentReturn != nil {
			t.Fatalf("dashboard leaderboard Alice = %+v, want +0.00 and nil percent", row)
		}
		if row := findPerson(t, dash.Leaderboard.People, bob); row.DollarPnL != "+10.00" {
			t.Fatalf("dashboard leaderboard Bob = %+v, want +10.00", row)
		}
	}
}

func TestMultiUserFlow_peopleBoard_excludesMembersOtherClubs(t *testing.T) {
	t.Parallel()

	// Arrange: Alice and Bob share one gaining club; Bob also funds a solo club.
	a := newMultiUserApp(t)
	alice := a.signIn(t, "cross-alice", "Alice")
	bob := a.signIn(t, "cross-bob", "Bob")
	shared := a.createClub(t, alice, "Shared Club")
	a.join(t, bob, shared)
	a.deposit(t, alice, shared, usdc10)
	a.deposit(t, bob, shared, usdc30)
	a.buyAAPLxAtMark(t, shared, usdc20, 10, 3_000_000)
	solo := a.createClub(t, bob, "Bob Solo Club")
	a.deposit(t, bob, solo, usdc5)

	// Act
	aliceHome, bobHome := a.home(t, alice), a.home(t, bob)

	// Assert: Bob's own board aggregates both clubs ($42.50 equity on $35 in) ...
	bobSelf := findPerson(t, bobHome.People, bob)
	if bobSelf.DollarPnL != "+7.50" {
		t.Fatalf("Bob's own people row = %+v, want +7.50 across both clubs", bobSelf)
	}
	requirePercent(t, "Bob's own people row", bobSelf.PercentReturn, "0.214286")

	// ... while Alice only sees Bob's slice of the club they share, never his solo club.
	bobSeenByAlice := findPerson(t, aliceHome.People, bob)
	if bobSeenByAlice.DollarPnL != "+7.50" {
		t.Fatalf("Alice's view of Bob = %+v, want +7.50", bobSeenByAlice)
	}
	requirePercent(t, "Alice's view of Bob", bobSeenByAlice.PercentReturn, "0.25")
	if got := findPerson(t, aliceHome.People, alice).DollarPnL; got != "+2.50" {
		t.Fatalf("Alice people row dollarPnl = %s, want +2.50", got)
	}
	for _, row := range aliceHome.Groups {
		if row.GroupID == solo.ID && row.IsJoined {
			t.Fatal("Bob's solo club must not be joined for Alice")
		}
	}
}

func TestMultiUserFlow_pendingJoinerDeposit_isNotCreditedToFundedMembers(t *testing.T) {
	t.Parallel()

	// Arrange: Alice is funded; Bob's sweep lands in the treasury but is not yet confirmed.
	a := newMultiUserApp(t)
	alice := a.signIn(t, "pending-alice", "Alice")
	bob := a.signIn(t, "pending-bob", "Bob")
	c := a.createClub(t, alice, "Pending Sweep Club")
	a.join(t, bob, c)
	a.deposit(t, alice, c, usdc10)
	bobIntent := a.openFundIntent(t, bob, c, usdc30)
	a.landSweepInTreasury(t, bob, c, usdc30)

	// Act: every read path runs the on-read treasury reconcile.
	aliceView := a.groupView(t, alice, c)
	_ = a.home(t, alice)
	_ = a.dashboard(t, bob)

	// Assert: Bob's in-flight USDC is not minted to Alice as surplus.
	if aliceView.You.EquityUsd != "10.00" || aliceView.You.DollarPnL != "+0.00" {
		t.Fatalf("Alice you while Bob pending = %+v, want 10.00 and +0.00", aliceView.You)
	}
	position, found, err := a.Store.GetPosition(context.Background(), alice.UserID, c.ID)
	if err != nil || !found {
		t.Fatalf("GetPosition(Alice): found=%v err=%v", found, err)
	}
	if position.ShareUnits != usdc10 || position.AmountDeposited != usdc10 {
		t.Fatalf("Alice position = %+v, want %d shares and deposited", position, usdc10)
	}

	// Once Bob's sweep confirms, the credit lands on Bob only.
	a.confirmSweep(t, bob, c, bobIntent)
	aliceYou, bobYou := a.groupView(t, alice, c).You, a.groupView(t, bob, c).You
	if aliceYou.EquityUsd != "10.00" || aliceYou.SlicePercent != "0.25" {
		t.Fatalf("Alice you after Bob confirm = %+v, want 10.00 at 0.25", aliceYou)
	}
	if bobYou.EquityUsd != "30.00" || bobYou.SlicePercent != "0.75" {
		t.Fatalf("Bob you after confirm = %+v, want 30.00 at 0.75", bobYou)
	}
}
