package app

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// Every event Monaco tells members about, and who hears it. Each method looks up the names it
// needs, builds the copy (notify_copy.go) and calls Notify. None of them returns an error: the
// event already happened, and a notification that could not be written is logged, not raised.

// ProposalCreated tells every voter but the proposer that a vote is open.
func (n *Notifier) ProposalCreated(ctx context.Context, row postgres.ProposalRow, voterIDs []string) {
	if n == nil {
		return
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	recipients := withoutID(voterIDs, row.ProposerID)
	if len(recipients) == 0 {
		return
	}
	cabal := n.cabalName(ctx, row.GroupID)
	title, body := proposalCreatedCopy(
		n.personName(ctx, row.ProposerID), cabal, proposalRowSubject(row), untilClose(row.ExpiresAt, n.now()),
	)
	n.notifyOnce(ctx, recipients, Notification{
		Kind: NotifyProposalCreated, Title: title, Body: body,
		GroupID: row.GroupID, ProposalID: row.ID, Symbol: row.Symbol,
	})
}

// ProposalClosed tells every member how a vote ended: voted down, ran out of time, or (for the
// bot votes, which trade nothing) passed. A passed buy or sell is told when it fills instead.
func (n *Notifier) ProposalClosed(ctx context.Context, proposalID string) {
	if n == nil {
		return
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	row, found, err := n.store.GetProposalByID(ctx, proposalID)
	if err != nil || !found {
		n.logLookupFailed(ctx, "proposal", proposalID, err)
		return
	}
	if n.isReadOnlyProposal(ctx, row) {
		return
	}
	cabal := n.cabalName(ctx, row.GroupID)
	proposer := n.personName(ctx, row.ProposerID)
	note := Notification{GroupID: row.GroupID, ProposalID: row.ID, Symbol: row.Symbol}
	switch row.Status {
	case domain.ProposalFailed:
		note.Kind = NotifyProposalFailed
		note.Title, note.Body = proposalFailedCopy(cabal, proposer, proposalRowSubject(row))
	case domain.ProposalExpired:
		note.Kind = NotifyProposalExpired
		note.Title, note.Body = proposalExpiredCopy(cabal, proposalRowSubject(row))
	case domain.ProposalPassed:
		if !domain.IsAgentGovernanceKind(row.Kind) {
			return
		}
		note.Kind = NotifyProposalPassed
		note.Symbol = ""
		note.Title, note.Body = proposalPassedCopy(row.Kind, cabal, proposer, row.AgentDisplayName, row.AllocationUsdcMicros)
	default:
		return
	}
	n.notifyOnce(ctx, n.memberIDs(ctx, row.GroupID), note)
}

// TradeFilled tells every member that a voted buy or sell went through.
func (n *Notifier) TradeFilled(ctx context.Context, proposal Proposal, tx postgres.TransactionRow) {
	if n == nil {
		return
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	cabal := n.cabalName(ctx, proposal.GroupID)
	note := Notification{GroupID: proposal.GroupID, ProposalID: proposal.ID, TransactionID: tx.ID, Symbol: proposal.Symbol}
	if tx.Action == postgres.TransactionActionSell {
		note.Kind = NotifyTradeSold
		note.Title, note.Body = tradeSoldCopy(cabal, proposal.Symbol, sellProceeds(tx))
	} else {
		note.Kind = NotifyTradeBought
		note.Title, note.Body = tradeBoughtCopy(cabal, proposal.Symbol, tx.Amount)
	}
	n.notifyOnce(ctx, n.memberIDs(ctx, proposal.GroupID), note)
}

// BotTrade tells every member what the cabal's bot just did with its budget.
func (n *Notifier) BotTrade(ctx context.Context, groupID, agentName, symbol string, side domain.AgentIntentSide, transactionID string) {
	if n == nil || transactionID == "" {
		return
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	var amount int64
	sell := side == domain.AgentIntentSell
	if tx, found, err := n.store.GetTransactionByID(ctx, transactionID); err == nil && found {
		if sell {
			amount = sellProceeds(tx)
		} else {
			amount = tx.Amount
		}
	}
	title, body := botTradeCopy(agentName, n.cabalName(ctx, groupID), symbol, sell, amount)
	n.notifyOnce(ctx, n.memberIDs(ctx, groupID), Notification{
		Kind: NotifyBotTrade, Title: title, Body: body,
		GroupID: groupID, TransactionID: transactionID, Symbol: symbol,
	})
}

// MemberJoined tells the cabal's creator that someone joined.
func (n *Notifier) MemberJoined(ctx context.Context, groupID, userID string) {
	if n == nil {
		return
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	group, found, err := n.store.GetGroupByID(ctx, groupID)
	if err != nil || !found {
		n.logLookupFailed(ctx, "group", groupID, err)
		return
	}
	if group.IsFaker || group.CreatorUserID == userID {
		return
	}
	count, err := n.store.CountGroupMembers(ctx, groupID)
	if err != nil {
		count = 0
	}
	title, body := memberJoinedCopy(n.personName(ctx, userID), group.Name, count)
	n.Notify(ctx, []string{group.CreatorUserID}, Notification{Kind: NotifyMemberJoined, Title: title, Body: body, GroupID: groupID})
}

// JoinRequested tells the creator of a by-request cabal that someone is waiting at the door.
func (n *Notifier) JoinRequested(ctx context.Context, groupID, userID string) {
	if n == nil {
		return
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	group, found, err := n.store.GetGroupByID(ctx, groupID)
	if err != nil || !found {
		n.logLookupFailed(ctx, "group", groupID, err)
		return
	}
	if group.IsFaker || group.CreatorUserID == userID {
		return
	}
	title, body := joinRequestCopy(n.personName(ctx, userID), group.Name)
	n.Notify(ctx, []string{group.CreatorUserID}, Notification{Kind: NotifyJoinRequest, Title: title, Body: body, GroupID: groupID})
}

// JoinApproved tells the member who asked that they are in.
func (n *Notifier) JoinApproved(ctx context.Context, groupID, userID string) {
	if n == nil {
		return
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	title, body := joinApprovedCopy(n.cabalName(ctx, groupID))
	n.Notify(ctx, []string{userID}, Notification{Kind: NotifyJoinApproved, Title: title, Body: body, GroupID: groupID})
}

// ChatMessage tells every member but the author, at most once per cabal per ChatNotifyWindow.
func (n *Notifier) ChatMessage(ctx context.Context, groupID, authorID, message string) {
	if n == nil {
		return
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	recipients := withoutID(n.memberIDs(ctx, groupID), authorID)
	if len(recipients) == 0 {
		return
	}
	title, body := chatMessageCopy(n.personName(ctx, authorID), n.cabalName(ctx, groupID), message)
	n.notifyThrottled(ctx, recipients, Notification{Kind: NotifyChatMessage, Title: title, Body: body, GroupID: groupID},
		n.now().Add(-ChatNotifyWindow))
}

// FundCredited tells a member their money reached the cabal's pot.
func (n *Notifier) FundCredited(ctx context.Context, userID, groupID string, amountMicros int64) {
	if n == nil || amountMicros <= 0 {
		return
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	title, body := fundCreditedCopy(n.cabalName(ctx, groupID), amountMicros)
	n.Notify(ctx, []string{userID}, Notification{Kind: NotifyFundCredited, Title: title, Body: body, GroupID: groupID})
}

// CashOutSettled tells a member their cash out landed, in their balance or at their address.
func (n *Notifier) CashOutSettled(ctx context.Context, userID, groupID string, amountMicros int64, toAddress string) {
	if n == nil || amountMicros <= 0 {
		return
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	toBalance := false
	if wallet, found, err := n.store.GetMemberWalletByUserID(ctx, userID); err == nil && found {
		toBalance = wallet.SolanaAddress == toAddress
	}
	title, body := cashOutSettledCopy(n.cabalName(ctx, groupID), amountMicros, toBalance)
	n.Notify(ctx, []string{userID}, Notification{Kind: NotifyCashOutSettled, Title: title, Body: body, GroupID: groupID})
}

// MinFundsArrivedMicros is the smallest arrival worth telling someone about. Below it is dust.
const MinFundsArrivedMicros = 100_000

// ObserveMemberBalance compares what the member wallet holds with the running total of money
// that reached it from outside Monaco, and tells the member when that total rises.
//
// outside inflow = chain balance + confirmed sweeps into cabals + confirmed withdrawals
//
//	− cash-outs paid to the balance (pending ones too)
//
// The first reading only sets the mark. The mark never goes down, so a dip while a sweep or
// withdrawal is in flight cannot come back later as a false arrival. Returns the arrival it
// reported, zero when there was none.
func (n *Notifier) ObserveMemberBalance(ctx context.Context, userID, walletAddress string, chainBalanceMicros int64) int64 {
	if n == nil || userID == "" || walletAddress == "" || chainBalanceMicros < 0 {
		return 0
	}
	ctx, cancel := n.detached(ctx)
	defer cancel()
	flows, err := n.store.GetMemberMoneyFlows(ctx, userID, walletAddress)
	if err != nil {
		slog.WarnContext(ctx, "balance watch flows failed", "user_id", userID, "err", err)
		return 0
	}
	inflow := chainBalanceMicros + flows.SweepsOut + flows.WithdrawalsOut - flows.PayoutsIn
	now := n.now()
	mark, found, err := n.store.GetMemberBalanceMark(ctx, userID)
	if err != nil {
		slog.WarnContext(ctx, "balance watch mark read failed", "user_id", userID, "err", err)
		return 0
	}
	if !found {
		if _, err := n.store.InsertMemberBalanceMark(ctx, userID, inflow, now); err != nil {
			slog.WarnContext(ctx, "balance watch mark write failed", "user_id", userID, "err", err)
		}
		return 0
	}
	if inflow <= mark {
		if err := n.store.TouchMemberBalanceMark(ctx, userID, now); err != nil {
			slog.WarnContext(ctx, "balance watch touch failed", "user_id", userID, "err", err)
		}
		return 0
	}
	moved, err := n.store.AdvanceMemberBalanceMark(ctx, userID, mark, inflow, now)
	if err != nil || !moved {
		// Another reader saw the same rise first and reports it.
		return 0
	}
	arrived := inflow - mark
	if arrived < MinFundsArrivedMicros {
		return 0
	}
	title, body := fundsArrivedCopy(arrived)
	n.Notify(ctx, []string{userID}, Notification{Kind: NotifyFundsArrived, Title: title, Body: body})
	return arrived
}

// detached lets an event finish reporting after the request that caused it has returned.
func (n *Notifier) detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
}

func (n *Notifier) cabalName(ctx context.Context, groupID string) string {
	if n == nil || groupID == "" {
		return ""
	}
	group, found, err := n.store.GetGroupByID(ctx, groupID)
	if err != nil || !found {
		n.logLookupFailed(ctx, "group", groupID, err)
		return ""
	}
	return group.Name
}

func (n *Notifier) personName(ctx context.Context, userID string) string {
	if n == nil || userID == "" {
		return ""
	}
	names, err := n.store.ListUserDisplayNamesByIDs(ctx, []string{userID})
	if err != nil {
		n.logLookupFailed(ctx, "user", userID, err)
		return ""
	}
	return names[userID]
}

// memberIDs is every member who counts in a real cabal: ghosts never get notifications.
func (n *Notifier) memberIDs(ctx context.Context, groupID string) []string {
	ids, err := n.store.ListLiveGroupMemberIDs(ctx, groupID)
	if err != nil {
		n.logLookupFailed(ctx, "group members", groupID, err)
		return nil
	}
	return ids
}

// isReadOnlyProposal is true for a seeded demo cabal or a ghost's proposal: nobody votes on
// those, so nobody hears about them.
func (n *Notifier) isReadOnlyProposal(ctx context.Context, row postgres.ProposalRow) bool {
	if fakerGroup, err := n.store.IsFakerGroup(ctx, row.GroupID); err != nil || fakerGroup {
		return true
	}
	fakerUser, err := n.store.IsFakerUser(ctx, row.ProposerID)
	return err != nil || fakerUser
}

func (n *Notifier) logLookupFailed(ctx context.Context, what, id string, err error) {
	if err == nil {
		err = sql.ErrNoRows
	}
	slog.WarnContext(ctx, "notification lookup failed", "what", what, "id", id, "err", err)
}

func proposalRowSubject(row postgres.ProposalRow) string {
	return notifyProposalSubject(row.Kind, row.Symbol, row.UsdcMicros, row.AgentDisplayName, row.AllocationUsdcMicros)
}

// sellProceeds is the USDC a confirmed sell raised; the ledger keeps it in cost_basis_price.
func sellProceeds(tx postgres.TransactionRow) int64 {
	if tx.CostBasisPrice.Valid {
		return tx.CostBasisPrice.Int64
	}
	return 0
}

func withoutID(ids []string, drop string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != drop && id != "" {
			out = append(out, id)
		}
	}
	return out
}

// untilClose is how long a proposal has left, never negative.
func untilClose(expiresAt, now time.Time) time.Duration {
	if d := expiresAt.Sub(now); d > 0 {
		return d
	}
	return 0
}
