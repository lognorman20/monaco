package app

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// recordingPushSender keeps every push it was handed.
type recordingPushSender struct {
	mu   sync.Mutex
	msgs []PushMessage
}

func (r *recordingPushSender) Send(_ context.Context, msgs []PushMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msgs...)
}

func (r *recordingPushSender) forUser(userID string) []PushMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []PushMessage
	for _, m := range r.msgs {
		if m.UserID == userID {
			out = append(out, m)
		}
	}
	return out
}

// notifyCabal is a cabal with a creator and members, a notifier that pushes inline, and the
// governance service wired to it.
type notifyCabal struct {
	h        governanceHarness
	notifier *Notifier
	push     *recordingPushSender
	groupID  string
	// members[0] is the creator.
	members []string
	tokens  []string
	now     time.Time
}

func newNotifyCabal(t *testing.T, names ...string) *notifyCabal {
	t.Helper()
	h := integrationGovernanceApp(t)
	push := &recordingPushSender{}
	notifier := NewNotifier(h.Store, push).SendPushInline()
	c := &notifyCabal{h: h, notifier: notifier, push: push, now: time.Now().UTC().Truncate(time.Second)}
	// Both clocks read c.now, so a test moves time by assigning it.
	notifier.SetClock(func() time.Time { return c.now })
	h.Governance.SetNotifier(notifier)
	h.Governance.SetClock(func() time.Time { return c.now })
	for i, name := range names {
		label := "member" + string(rune('a'+i))
		session := openTestSession(t, h.ISO, h.Sessions, h.Privy, label, name)
		c.members = append(c.members, session.UserID)
		c.tokens = append(c.tokens, h.ISO.UniqueToken(label))
	}
	created, err := h.Governance.CreateGroupWithRules(context.Background(), c.tokens[0], testGroupName(h.ISO, "notify"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	c.groupID = created.GroupID
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 1_000_000_000)
	for _, token := range c.tokens[1:] {
		// A second apart, so the inbox's newest-first order is the join order reversed.
		c.now = c.now.Add(time.Second)
		if _, err := h.Governance.JoinGroup(context.Background(), token, c.groupID); err != nil {
			t.Fatalf("join: %v", err)
		}
	}
	c.now = c.now.Add(time.Second)
	return c
}

func (c *notifyCabal) propose(t *testing.T, proposer int, usdc int64) Proposal {
	t.Helper()
	registerRoutableQuote(t, c.h.Jupiter, c.h.XStocks, "AAPLx", usdc)
	p, err := c.h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID: c.groupID, ProposerID: c.members[proposer], Symbol: "AAPLx", UsdcMicros: usdc,
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	return p
}

func (c *notifyCabal) inbox(t *testing.T, member int) []postgres.NotificationRow {
	t.Helper()
	rows, err := c.h.Store.ListNotifications(context.Background(), c.members[member], nil, 100)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	return rows
}

func kindsOf(rows []postgres.NotificationRow) []string {
	kinds := make([]string, 0, len(rows))
	for _, r := range rows {
		kinds = append(kinds, r.Kind)
	}
	sort.Strings(kinds)
	return kinds
}

func countKind(rows []postgres.NotificationRow, kind string) int {
	n := 0
	for _, r := range rows {
		if r.Kind == kind {
			n++
		}
	}
	return n
}

// setNotificationPreference writes preferences.notifications.<category> the way the settings
// screen does: a JSON boolean, merged into whatever else the member has set.
func setNotificationPreference(t *testing.T, c *notifyCabal, member int, category string, on bool) {
	t.Helper()
	if _, err := c.h.DB.ExecContext(context.Background(), `
UPDATE users
SET preferences = preferences || jsonb_build_object(
  'notifications', COALESCE(preferences->'notifications', '{}'::jsonb) || jsonb_build_object($2::text, $3::boolean))
WHERE id = $1`, c.members[member], category, on); err != nil {
		t.Fatalf("set preference: %v", err)
	}
}

func firstOfKind(t *testing.T, rows []postgres.NotificationRow, kind string) postgres.NotificationRow {
	t.Helper()
	for _, r := range rows {
		if r.Kind == kind {
			return r
		}
	}
	t.Fatalf("no %s in %v", kind, kindsOf(rows))
	return postgres.NotificationRow{}
}

func TestProposalCreated_notifiesEveryVoterButTheProposer(t *testing.T) {
	// Arrange
	c := newNotifyCabal(t, "Jordan", "Priya", "Sam")

	// Act
	p := c.propose(t, 0, 250_000_000)

	// Assert
	if got := countKind(c.inbox(t, 0), NotifyProposalCreated); got != 0 {
		t.Fatalf("proposer got %d proposal notifications, want 0", got)
	}
	for _, member := range []int{1, 2} {
		rows := c.inbox(t, member)
		if countKind(rows, NotifyProposalCreated) != 1 {
			t.Fatalf("member %d kinds = %v, want one proposal_created", member, kindsOf(rows))
		}
		row := firstOfKind(t, rows, NotifyProposalCreated)
		if row.Title != "Jordan proposed $250 of Apple in "+c.cabalName(t) {
			t.Fatalf("title = %q", row.Title)
		}
		if row.Body != "Voting closes in 1 day." {
			t.Fatalf("body = %q", row.Body)
		}
		if row.ProposalID.String != p.ID || row.GroupID.String != c.groupID || row.Symbol.String != "AAPLx" {
			t.Fatalf("row points at %+v", row)
		}
		pushes := c.push.forUser(c.members[member])
		if len(pushes) != 1 || pushes[0].ProposalID != p.ID || pushes[0].GroupID != c.groupID {
			t.Fatalf("pushes = %+v", pushes)
		}
	}
}

func (c *notifyCabal) cabalName(t *testing.T) string {
	t.Helper()
	group, _, err := c.h.Store.GetGroupByID(context.Background(), c.groupID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	return group.Name
}

func TestNotify_respectsEachMembersCategoryPreference(t *testing.T) {
	cases := []struct {
		name     string
		category string
		on       bool
		want     int
	}{
		{name: "proposals switched off", category: NotifyCategoryProposals, on: false, want: 0},
		{name: "proposals switched on explicitly", category: NotifyCategoryProposals, on: true, want: 1},
		{name: "another category off leaves proposals on", category: NotifyCategoryChat, on: false, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			c := newNotifyCabal(t, "Jordan", "Priya")
			setNotificationPreference(t, c, 1, tc.category, tc.on)

			// Act
			c.propose(t, 0, 10_000_000)

			// Assert
			if got := countKind(c.inbox(t, 1), NotifyProposalCreated); got != tc.want {
				t.Fatalf("proposal notifications = %d, want %d", got, tc.want)
			}
			if got := len(c.push.forUser(c.members[1])); got != tc.want {
				t.Fatalf("pushes = %d, want %d: an opted-out member gets neither row nor push", got, tc.want)
			}
		})
	}
}

func TestNotify_missingPreferenceMeansOn(t *testing.T) {
	c := newNotifyCabal(t, "Jordan", "Priya")
	if _, err := c.h.DB.ExecContext(context.Background(), `UPDATE users SET preferences = '{"theme":"dark"}' WHERE id = $1`, c.members[1]); err != nil {
		t.Fatalf("seed preferences: %v", err)
	}

	c.propose(t, 0, 10_000_000)

	if got := countKind(c.inbox(t, 1), NotifyProposalCreated); got != 1 {
		t.Fatalf("got %d, want 1", got)
	}
}

func TestChatMessage_notifiesOncePerCabalPerMemberPerTenMinutes(t *testing.T) {
	// Arrange
	c := newNotifyCabal(t, "Jordan", "Priya", "Sam")
	chat := NewGroupChatService(c.h.Store, c.h.Privy)
	chat.SetNotifier(c.notifier)
	post := func(member int, body string) {
		t.Helper()
		if _, err := chat.PostMessage(context.Background(), c.tokens[member], c.groupID, body); err != nil {
			t.Fatalf("post: %v", err)
		}
	}

	// Act: three messages inside the window, then one after it.
	post(0, "first")
	post(0, "second")
	c.now = c.now.Add(9 * time.Minute)
	post(2, "third, from someone else")
	afterWindow := c.now.Add(ChatNotifyWindow + time.Second)
	c.now = afterWindow

	// Assert: inside the window each member heard once.
	if got := countKind(c.inbox(t, 1), NotifyChatMessage); got != 1 {
		t.Fatalf("Priya got %d chat notifications in the window, want 1", got)
	}
	if got := countKind(c.inbox(t, 0), NotifyChatMessage); got != 1 {
		t.Fatalf("Jordan got %d, want 1 (for Sam's message; never for his own)", got)
	}
	if got := countKind(c.inbox(t, 2), NotifyChatMessage); got != 1 {
		t.Fatalf("Sam got %d, want 1 (for Jordan's first message)", got)
	}
	first := firstOfKind(t, c.inbox(t, 1), NotifyChatMessage)
	if first.Title != "Jordan in "+c.cabalName(t) || first.Body != "first" {
		t.Fatalf("chat row = %q / %q", first.Title, first.Body)
	}

	// Act: past the window the next message notifies again.
	post(0, "fourth")

	// Assert
	if got := countKind(c.inbox(t, 1), NotifyChatMessage); got != 2 {
		t.Fatalf("Priya got %d after the window, want 2", got)
	}
}

func TestChatMessage_authorNeverNotified(t *testing.T) {
	c := newNotifyCabal(t, "Jordan", "Priya")
	c.notifier.ChatMessage(context.Background(), c.groupID, c.members[0], "hello")
	if got := countKind(c.inbox(t, 0), NotifyChatMessage); got != 0 {
		t.Fatalf("author got %d", got)
	}
}

func TestProposalClosed_votedDownNotifiesEveryMemberOnce(t *testing.T) {
	// Arrange: two members, majority; one no from each fails it.
	c := newNotifyCabal(t, "Jordan", "Priya")
	p := c.propose(t, 0, 10_000_000)

	// Act
	for i := range c.members {
		if _, err := c.h.Governance.CastVote(context.Background(), CastVoteInput{ProposalID: p.ID, VoterID: c.members[i], Choice: domain.VoteNo}); err != nil && !errors.Is(err, ErrProposalNotOpen) {
			t.Fatalf("vote: %v", err)
		}
	}
	// A second report of the same close (the poller, a detail read) must not repeat it.
	c.notifier.ProposalClosed(context.Background(), p.ID)

	// Assert
	for i := range c.members {
		rows := c.inbox(t, i)
		if got := countKind(rows, NotifyProposalFailed); got != 1 {
			t.Fatalf("member %d kinds = %v, want one proposal_failed", i, kindsOf(rows))
		}
	}
	row := firstOfKind(t, c.inbox(t, 1), NotifyProposalFailed)
	if row.Title != c.cabalName(t)+" voted down $10 of Apple" || row.Body != "Proposed by Jordan." {
		t.Fatalf("failed copy = %q / %q", row.Title, row.Body)
	}
}

func TestProposalClosed_expiredNotifiesEveryMember(t *testing.T) {
	c := newNotifyCabal(t, "Jordan", "Priya")
	p := c.propose(t, 0, 10_000_000)
	later := c.now.Add(25 * time.Hour)
	c.h.Governance.SetClock(func() time.Time { return later })

	if _, err := c.h.Governance.FinalizeExpiredProposal(context.Background(), p.ID); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	for i := range c.members {
		if got := countKind(c.inbox(t, i), NotifyProposalExpired); got != 1 {
			t.Fatalf("member %d expired notifications = %d, want 1", i, got)
		}
	}
	if got := firstOfKind(t, c.inbox(t, 0), NotifyProposalExpired); got.Title != "The vote on $10 of Apple ran out of time" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestMemberJoined_tellsTheCreatorOnly(t *testing.T) {
	c := newNotifyCabal(t, "Jordan", "Priya", "Sam")

	creator := c.inbox(t, 0)
	if got := countKind(creator, NotifyMemberJoined); got != 2 {
		t.Fatalf("creator got %d member_joined, want 2", got)
	}
	if countKind(c.inbox(t, 1), NotifyMemberJoined) != 0 {
		t.Fatal("a member was told about a join")
	}
	if creator[0].Title != "Sam joined "+c.cabalName(t) || creator[0].Body != "3 members now." {
		t.Fatalf("newest join row = %q / %q", creator[0].Title, creator[0].Body)
	}
}

func TestNotify_ghostMembersNeverGetRows(t *testing.T) {
	c := newNotifyCabal(t, "Jordan", "Priya")
	if _, err := c.h.DB.ExecContext(context.Background(), `UPDATE users SET is_faker = true WHERE id = $1`, c.members[1]); err != nil {
		t.Skipf("cannot flag a ghost here: %v", err)
	}
	t.Cleanup(func() {
		_, _ = c.h.DB.ExecContext(context.Background(), `UPDATE users SET is_faker = false WHERE id = $1`, c.members[1])
	})

	written := c.notifier.Notify(context.Background(), c.members, Notification{Kind: NotifyMemberJoined, Title: "x", GroupID: c.groupID})

	if written != 1 {
		t.Fatalf("written = %d, want only the real member", written)
	}
}

func TestNotify_pushCarriesTheUnreadBadge(t *testing.T) {
	c := newNotifyCabal(t, "Jordan", "Priya")
	before := len(c.inbox(t, 0))

	c.notifier.Notify(context.Background(), []string{c.members[0]}, Notification{Kind: NotifyFundCredited, Title: "$5 is in the pot"})

	pushes := c.push.forUser(c.members[0])
	last := pushes[len(pushes)-1]
	if last.Badge != before+1 {
		t.Fatalf("badge = %d, want %d unread", last.Badge, before+1)
	}
}

func TestNotify_nilNotifierIsSafe(t *testing.T) {
	var n *Notifier
	if n.Notify(context.Background(), []string{"x"}, Notification{Kind: "k", Title: "t"}) != 0 {
		t.Fatal("nil notifier wrote")
	}
	n.ProposalClosed(context.Background(), "x")
	n.ChatMessage(context.Background(), "g", "u", "m")
	if n.ObserveMemberBalance(context.Background(), "u", "a", 1) != 0 {
		t.Fatal("nil notifier observed")
	}
}
