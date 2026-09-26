package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/apns"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// Notification categories. Each is a member preference under
// users.preferences.notifications.<category>; a missing key means on.
const (
	NotifyCategoryProposals = "proposals"
	NotifyCategoryResults   = "results"
	NotifyCategoryChat      = "chat"
	NotifyCategoryMoney     = "money"
)

// Notification kinds, as stored and as the app reads them.
const (
	// proposals: something is waiting on you.
	NotifyProposalCreated  = "proposal_created"
	NotifyProposalExpiring = "proposal_expiring"
	NotifyProposalNudge    = "proposal_nudge"
	NotifyJoinRequest      = "join_request"

	// results: what the cabal decided and did.
	NotifyProposalPassed  = "proposal_passed"
	NotifyProposalFailed  = "proposal_failed"
	NotifyProposalExpired = "proposal_expired"
	NotifyTradeBought     = "trade_bought"
	NotifyTradeSold       = "trade_sold"
	NotifyBotTrade        = "bot_trade"
	NotifyMemberJoined    = "member_joined"
	NotifyJoinApproved    = "join_approved"

	// chat
	NotifyChatMessage = "chat_message"

	// money: your own dollars moved.
	NotifyFundsArrived   = "funds_arrived"
	NotifyFundCredited   = "fund_credited"
	NotifyCashOutSettled = "cash_out_settled"
)

// ChatNotifyWindow is how long one cabal's chat stays quiet for a member after it notified them.
const ChatNotifyWindow = 10 * time.Minute

// NotificationCategory is the preference that switches kind off.
func NotificationCategory(kind string) string {
	switch kind {
	case NotifyProposalCreated, NotifyProposalExpiring, NotifyProposalNudge, NotifyJoinRequest:
		return NotifyCategoryProposals
	case NotifyChatMessage:
		return NotifyCategoryChat
	case NotifyFundsArrived, NotifyFundCredited, NotifyCashOutSettled:
		return NotifyCategoryMoney
	default:
		return NotifyCategoryResults
	}
}

// Notification is one event, addressed to many members.
type Notification struct {
	Kind          string
	Title         string
	Body          string
	GroupID       string
	ProposalID    string
	TransactionID string
	// Symbol is the stock a buy or sell was in, so the inbox can draw its mark.
	Symbol string
}

// PushMessage is one member's copy of a notification, ready for their devices.
type PushMessage struct {
	UserID         string
	NotificationID string
	Kind           string
	Title          string
	Body           string
	GroupID        string
	ProposalID     string
	// Badge is the member's unread count after this notification.
	Badge int
}

// PushSender delivers push messages. Implementations log their own failures: a push that does
// not arrive must never undo the event that caused it.
type PushSender interface {
	Send(ctx context.Context, msgs []PushMessage)
}

// LogPushSender is the sender when APNs is not configured: it says what it would have sent.
type LogPushSender struct{}

// Send logs each message.
func (LogPushSender) Send(ctx context.Context, msgs []PushMessage) {
	for _, m := range msgs {
		slog.InfoContext(ctx, "push skipped (APNs not configured)",
			"user_id", m.UserID, "notification_id", m.NotificationID, "kind", m.Kind, "badge", m.Badge)
	}
}

// pushConcurrency bounds how many batches go to Apple at once. Past it a batch is dropped with a
// warning: the inbox row is already written, so nothing is lost but the buzz.
const pushConcurrency = 8

// pushTimeout bounds one batch's delivery.
const pushTimeout = 30 * time.Second

// Notifier writes inbox rows and hands them to a PushSender. A nil *Notifier is valid and does
// nothing, so services work unchanged when notifications are not wired.
type Notifier struct {
	store *postgres.Store
	push  PushSender
	now   func() time.Time
	// sem is nil when pushes are sent inline (tests).
	sem chan struct{}
}

// NewNotifier sends pushes in the background, a few batches at a time.
func NewNotifier(store *postgres.Store, push PushSender) *Notifier {
	if push == nil {
		push = LogPushSender{}
	}
	return &Notifier{store: store, push: push, now: time.Now, sem: make(chan struct{}, pushConcurrency)}
}

// SetClock overrides time.Now. For tests.
func (n *Notifier) SetClock(now func() time.Time) {
	if n == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	n.now = now
}

// SendPushInline makes Notify deliver before it returns. For tests.
func (n *Notifier) SendPushInline() *Notifier {
	if n != nil {
		n.sem = nil
	}
	return n
}

// Notify writes one row per member in userIDs who has the kind's category switched on, then
// pushes them. It returns how many members it reached. Failures are logged, never returned to
// the path that caused the event: a missed notification must not fail a vote or a buy.
func (n *Notifier) Notify(ctx context.Context, userIDs []string, note Notification) int {
	return n.notify(ctx, userIDs, note, notifyOptions{})
}

// notifyOnce is Notify for an event that may be reported more than once (a proposal finalized
// from two places, a fill seen by a retry): a member gets it once per proposal and transaction.
func (n *Notifier) notifyOnce(ctx context.Context, userIDs []string, note Notification) int {
	return n.notify(ctx, userIDs, note, notifyOptions{once: true})
}

// notifyThrottled is Notify that skips members who got the same kind for the same cabal since.
func (n *Notifier) notifyThrottled(ctx context.Context, userIDs []string, note Notification, since time.Time) int {
	return n.notify(ctx, userIDs, note, notifyOptions{throttleSince: &since})
}

type notifyOptions struct {
	throttleSince *time.Time
	once          bool
}

func (n *Notifier) notify(ctx context.Context, userIDs []string, note Notification, opts notifyOptions) int {
	if n == nil || n.store == nil || len(userIDs) == 0 {
		return 0
	}
	note.Title = clampText(note.Title, 300)
	note.Body = clampText(note.Body, 600)
	if note.Kind == "" || note.Title == "" {
		slog.WarnContext(ctx, "notification dropped", "reason", "kind and title are required", "kind", note.Kind)
		return 0
	}
	// Detached from the request: the event already happened, and a client that hung up must
	// not leave its cabal un-notified.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	inserted, err := n.store.InsertNotifications(writeCtx, postgres.InsertNotificationsParams{
		UserIDs:       userIDs,
		Category:      NotificationCategory(note.Kind),
		Kind:          note.Kind,
		Title:         note.Title,
		Body:          note.Body,
		GroupID:       note.GroupID,
		ProposalID:    note.ProposalID,
		TransactionID: note.TransactionID,
		Symbol:        note.Symbol,
		ThrottleSince: opts.throttleSince,
		Once:          opts.once,
		CreatedAt:     n.now().UTC(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "notification write failed", "kind", note.Kind, "group_id", note.GroupID,
			"proposal_id", note.ProposalID, "recipients", len(userIDs), "err", err)
		return 0
	}
	slog.InfoContext(ctx, "notifications written", "kind", note.Kind, "group_id", note.GroupID,
		"proposal_id", note.ProposalID, "recipients", len(userIDs), "written", len(inserted))
	if len(inserted) == 0 {
		return 0
	}
	recipients := make([]string, 0, len(inserted))
	for _, row := range inserted {
		recipients = append(recipients, row.UserID)
	}
	badges, err := n.store.CountUnreadNotificationsByUser(writeCtx, recipients)
	if err != nil {
		slog.WarnContext(ctx, "notification badge count failed", "kind", note.Kind, "err", err)
		badges = map[string]int{}
	}
	msgs := make([]PushMessage, 0, len(inserted))
	for _, row := range inserted {
		msgs = append(msgs, PushMessage{
			UserID:         row.UserID,
			NotificationID: row.ID,
			Kind:           note.Kind,
			Title:          note.Title,
			Body:           note.Body,
			GroupID:        note.GroupID,
			ProposalID:     note.ProposalID,
			Badge:          badges[row.UserID],
		})
	}
	n.dispatch(ctx, msgs)
	return len(inserted)
}

func (n *Notifier) dispatch(ctx context.Context, msgs []PushMessage) {
	if n.sem == nil {
		sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pushTimeout)
		defer cancel()
		n.push.Send(sendCtx, msgs)
		return
	}
	select {
	case n.sem <- struct{}{}:
	default:
		slog.WarnContext(ctx, "push dropped", "reason", "too many batches in flight", "kind", msgs[0].Kind, "recipients", len(msgs))
		return
	}
	go func() {
		defer func() { <-n.sem }()
		sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pushTimeout)
		defer cancel()
		n.push.Send(sendCtx, msgs)
	}()
}

// APNsPushSender delivers to every device a member registered, and forgets a device Apple
// says is gone.
type APNsPushSender struct {
	client *apns.Client
	store  *postgres.Store
}

// NewAPNsPushSender wires Apple push over the device table.
func NewAPNsPushSender(client *apns.Client, store *postgres.Store) *APNsPushSender {
	return &APNsPushSender{client: client, store: store}
}

// Send pushes each message to each of its member's devices.
func (s *APNsPushSender) Send(ctx context.Context, msgs []PushMessage) {
	if s == nil || s.client == nil || len(msgs) == 0 {
		return
	}
	userIDs := make([]string, 0, len(msgs))
	for _, m := range msgs {
		userIDs = append(userIDs, m.UserID)
	}
	devices, err := s.store.ListDeviceTokensForUsers(ctx, userIDs)
	if err != nil {
		slog.ErrorContext(ctx, "push device lookup failed", "recipients", len(userIDs), "err", err)
		return
	}
	byUser := make(map[string][]postgres.DeviceTokenRow, len(userIDs))
	for _, d := range devices {
		byUser[d.UserID] = append(byUser[d.UserID], d)
	}
	sent, dropped, failed := 0, 0, 0
	for _, m := range msgs {
		for _, d := range byUser[m.UserID] {
			err := s.client.Send(ctx, apns.Device{Token: d.Token, Env: deviceAPNsEnv(d.AppEnv, s.client.DefaultEnv())}, pushPayload(m))
			switch {
			case err == nil:
				sent++
			case errors.Is(err, apns.ErrUnregistered):
				dropped++
				if delErr := s.store.DeleteDeviceToken(ctx, d.Token); delErr != nil {
					slog.WarnContext(ctx, "push forget device failed", "user_id", d.UserID, "err", delErr)
				}
			default:
				failed++
				slog.WarnContext(ctx, "push failed", "user_id", d.UserID, "notification_id", m.NotificationID, "kind", m.Kind, "err", err)
			}
		}
	}
	slog.InfoContext(ctx, "push batch done", "kind", msgs[0].Kind, "sent", sent, "unregistered", dropped, "failed", failed)
}

// deviceAPNsEnv routes a device by the build that registered it: a debug build's token only
// works against the sandbox host and a TestFlight or App Store build's only against production.
func deviceAPNsEnv(appEnv string, fallback apns.Env) apns.Env {
	switch appEnv {
	case "debug":
		return apns.EnvSandbox
	case "production":
		return apns.EnvProduction
	default:
		return fallback
	}
}

func pushPayload(m PushMessage) apns.Payload {
	return apns.Payload{
		Alert:          apns.Alert{Title: m.Title, Body: m.Body},
		Badge:          m.Badge,
		Sound:          "default",
		ThreadID:       m.GroupID,
		GroupID:        m.GroupID,
		ProposalID:     m.ProposalID,
		NotificationID: m.NotificationID,
		Kind:           m.Kind,
	}
}

// clampText trims and cuts s to max runes, ending a cut line with an ellipsis.
func clampText(s string, max int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}
