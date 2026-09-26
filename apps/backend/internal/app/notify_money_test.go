package app

import (
	"context"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

func (c *notifyCabal) walletAddress(t *testing.T, member int) string {
	t.Helper()
	wallet, found, err := c.h.Store.GetMemberWalletByUserID(context.Background(), c.members[member])
	if err != nil || !found {
		t.Fatalf("member wallet: %v found=%v", err, found)
	}
	return wallet.SolanaAddress
}

func TestObserveMemberBalance_reportsMoneyFromOutsideOnly(t *testing.T) {
	// Arrange
	c := newNotifyCabal(t, "Jordan")
	ctx := context.Background()
	user := c.members[0]
	address := c.walletAddress(t, 0)
	observe := func(chain int64) int64 {
		t.Helper()
		return c.notifier.ObserveMemberBalance(ctx, user, address, chain)
	}

	// Act and assert, step by step as the chain balance moves.

	// The first reading only sets the mark, whatever the wallet already held.
	if got := observe(40_000_000); got != 0 {
		t.Fatalf("first reading reported %d", got)
	}
	// $500 arrives from outside.
	if got := observe(540_000_000); got != 500_000_000 {
		t.Fatalf("arrival reported %d, want $500", got)
	}
	// The same balance read again reports nothing.
	if got := observe(540_000_000); got != 0 {
		t.Fatalf("repeat reading reported %d", got)
	}

	// $200 is swept into the cabal: the balance falls, but that was Monaco, not an arrival or a
	// loss the member needs to hear about.
	// Written confirmed in one statement: a pending deposit, even for a moment, is counted by
	// the ops backlog test running in another package against the same database.
	if _, err := c.h.DB.ExecContext(ctx, `
INSERT INTO deposits (user_id, group_id, amount, from_address, status, tx_signature)
VALUES ($1, $2, 200000000, $3, 'confirmed', $4)`, user, c.groupID, address, testTxSignature(c.h.ISO, "sweep")); err != nil {
		t.Fatalf("insert confirmed deposit: %v", err)
	}
	if got := observe(340_000_000); got != 0 {
		t.Fatalf("sweep reported %d", got)
	}

	// A withdrawal lands on chain before the ledger confirms it: a dip, then back to the mark.
	withdrawal, err := c.h.Store.InsertPlatformWithdrawal(ctx, user, 50_000_000, "ExternalAddress1111111111111111111111111111")
	if err != nil {
		t.Fatalf("insert withdrawal: %v", err)
	}
	if got := observe(290_000_000); got != 0 {
		t.Fatalf("in-flight withdrawal reported %d", got)
	}
	if _, _, err := c.h.Store.ConfirmPlatformWithdrawal(ctx, withdrawal.ID, testTxSignature(c.h.ISO, "withdraw")); err != nil {
		t.Fatalf("confirm withdrawal: %v", err)
	}
	if got := observe(290_000_000); got != 0 {
		t.Fatalf("confirmed withdrawal reported %d", got)
	}

	// A cabal cash out paid to the balance is the member's own money coming home: it has its
	// own notification, so it is not "arrived" as well.
	if _, err := c.h.DB.ExecContext(ctx, `
INSERT INTO redeem_payouts (redeem_job_id, user_id, group_id, amount, to_address, tx_signature, signed_tx, last_valid_block_height, status)
VALUES (gen_random_uuid(), $1, $2, 120000000, $3, $4, 'signed', 1, 'pending')`,
		user, c.groupID, address, testTxSignature(c.h.ISO, "payout")); err != nil {
		t.Fatalf("insert payout: %v", err)
	}
	if got := observe(410_000_000); got != 0 {
		t.Fatalf("cash out reported %d", got)
	}

	// Dust is not worth a buzz, and it still moves the mark.
	if got := observe(410_050_000); got != 0 {
		t.Fatalf("dust reported %d", got)
	}
	// $25 more from outside, measured from the dust-inclusive mark.
	c.now = c.now.Add(time.Minute)
	if got := observe(435_050_000); got != 25_000_000 {
		t.Fatalf("second arrival reported %d, want $25", got)
	}

	arrivals := 0
	for _, row := range c.inbox(t, 0) {
		if row.Kind == NotifyFundsArrived {
			arrivals++
		}
	}
	if arrivals != 2 {
		t.Fatalf("funds_arrived rows = %d, want 2", arrivals)
	}
	if row := firstOfKind(t, c.inbox(t, 0), NotifyFundsArrived); row.Title != "$25 arrived in your balance" {
		t.Fatalf("newest arrival title = %q", row.Title)
	}
}

func TestObserveMemberBalance_moneyPreferenceOffStillMovesTheMark(t *testing.T) {
	c := newNotifyCabal(t, "Jordan")
	setNotificationPreference(t, c, 0, NotifyCategoryMoney, false)
	address := c.walletAddress(t, 0)
	c.notifier.ObserveMemberBalance(context.Background(), c.members[0], address, 0)

	c.notifier.ObserveMemberBalance(context.Background(), c.members[0], address, 10_000_000)

	if countKind(c.inbox(t, 0), NotifyFundsArrived) != 0 {
		t.Fatal("money switched off, yet a row was written")
	}
	mark, _, _ := c.h.Store.GetMemberBalanceMark(context.Background(), c.members[0])
	if mark != 10_000_000 {
		t.Fatalf("mark = %d; switching the category off must not replay the arrival later", mark)
	}
}

func TestMoneyEvents_copyAndRecipients(t *testing.T) {
	c := newNotifyCabal(t, "Jordan", "Priya")
	ctx := context.Background()
	cabal := c.cabalName(t)

	c.notifier.FundCredited(ctx, c.members[1], c.groupID, 250_000_000)
	c.notifier.CashOutSettled(ctx, c.members[1], c.groupID, 120_500_000, c.walletAddress(t, 1))
	c.now = c.now.Add(time.Second)
	c.notifier.CashOutSettled(ctx, c.members[1], c.groupID, 80_000_000, "SomeoneElsesAddress11111111111111111111111")

	rows := c.inbox(t, 1)
	fund := firstOfKind(t, rows, NotifyFundCredited)
	if fund.Title != "$250 is in "+cabal || fund.GroupID.String != c.groupID {
		t.Fatalf("fund row = %+v", fund)
	}
	var toBalance, toAddress postgres.NotificationRow
	for _, r := range rows {
		if r.Kind != NotifyCashOutSettled {
			continue
		}
		if r.Body == "Your cash out went through." {
			toBalance = r
		} else {
			toAddress = r
		}
	}
	if toBalance.Title != "$120.50 from "+cabal+" is in your balance" {
		t.Fatalf("cash out to balance title = %q", toBalance.Title)
	}
	if toAddress.Title != "You cashed out $80 from "+cabal || toAddress.Body != "Sent to your payout address." {
		t.Fatalf("cash out to address = %q / %q", toAddress.Title, toAddress.Body)
	}
	if countKind(c.inbox(t, 0), NotifyFundCredited)+countKind(c.inbox(t, 0), NotifyCashOutSettled) != 0 {
		t.Fatal("money notifications reached another member")
	}
}

func TestTradeEvents_everyMemberHearsOnceWithTheStock(t *testing.T) {
	// Arrange
	c := newNotifyCabal(t, "Jordan", "Priya")
	ctx := context.Background()
	p := c.propose(t, 0, 250_000_000)
	buy, err := c.h.Store.InsertFailedTransaction(ctx, c.groupID, postgres.TransactionActionBuy, jupiter.USDCMint, jupiter.AAPLxMint, 250_000_000, testRequestID(c.h.ISO, "buy"))
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	// Act: the fill is reported twice (a retried execute), and the bot trades too.
	c.notifier.TradeFilled(ctx, p, buy)
	c.notifier.TradeFilled(ctx, p, buy)
	botTx, err := c.h.Store.InsertFailedTransaction(ctx, c.groupID, postgres.TransactionActionBuy, jupiter.USDCMint, jupiter.AAPLxMint, 40_000_000, testRequestID(c.h.ISO, "bot"))
	if err != nil {
		t.Fatalf("insert bot transaction: %v", err)
	}
	c.notifier.BotTrade(ctx, c.groupID, "Scout", "NVDAx", domain.AgentIntentBuy, botTx.ID)

	// Assert
	for i := range c.members {
		rows := c.inbox(t, i)
		if countKind(rows, NotifyTradeBought) != 1 {
			t.Fatalf("member %d kinds = %v, want one trade_bought", i, kindsOf(rows))
		}
		bought := firstOfKind(t, rows, NotifyTradeBought)
		if bought.Title != c.cabalName(t)+" bought Apple" || bought.Body != "$250 from the pot, as voted." {
			t.Fatalf("bought copy = %q / %q", bought.Title, bought.Body)
		}
		if bought.Symbol.String != "AAPLx" || bought.TransactionID.String != buy.ID || bought.ProposalID.String != p.ID {
			t.Fatalf("bought row = %+v", bought)
		}
		bot := firstOfKind(t, rows, NotifyBotTrade)
		if bot.Title != "Scout bought $40 of Nvidia" || bot.Symbol.String != "NVDAx" {
			t.Fatalf("bot row = %q %q", bot.Title, bot.Symbol.String)
		}
	}
}
