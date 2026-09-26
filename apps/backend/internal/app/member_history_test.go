package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// historyFixture is Ada's money over ten days, one row of every kind, plus rows that are
// not hers or not yet hers.
//
// Sunday Investors: Ada 60%, Ben 40%. Semis or bust: Ada alone.
//
//	day -10  Ben funds Sunday $400                         not Ada's
//	day -9   Sunday buys Tesla for $100                    before Ada had a stake
//	day -8.5 Ada funds Semis $100                          fund, done
//	day -8   Ada funds Sunday $600                         fund, done
//	day -7   Sunday buys 4 Apple for $800                  buy: $480, 2.4 shares
//	day -6.5 Ada cashes $30 out of Sunday (paid)           cash_out, done
//	day -6   Sunday's agent sells 1 Apple for $260         bot_sell: $156, 0.6 shares
//	day -5   Sunday buys Apple for $50 (not filled yet)    buy, pending: $30, no shares
//	day -4   Ada sends $25 out of her account              withdrawal, done
//	day -3   Ada cashing $70 out of Semis                  cash_out, pending
//	day -2   Ada's $40 cash out of Semis failed            cash_out, failed
//	day -1   Ada funds Sunday $50 (sweep not landed)       fund, pending
type historyFixture struct {
	fx  portfolioFixture
	ids map[string]string
}

var historyNewestFirst = []string{
	"fund-pending", "cashout-failed", "cashout-pending", "withdrawal", "buy-pending",
	"bot-sell", "cashout-paid", "buy-apple", "fund-sunday", "fund-semis",
}

func newHistoryFixture(t *testing.T) historyFixture {
	t.Helper()
	base := newPortfolioFixture(t)
	h := base.h
	// Start from clean books: only the rows below exist for these cabals.
	for _, table := range []string{"transactions", "deposits", "positions"} {
		execSQL(t, h.DB, `DELETE FROM `+table+` WHERE group_id = ANY($1::uuid[])`, []string{base.sunday, base.semis})
	}
	hf := historyFixture{fx: base, ids: map[string]string{}}
	day := func(d float64) time.Time { return time.Now().UTC().Add(time.Duration(d * float64(24*time.Hour))) }

	execSQL(t, h.DB, `INSERT INTO positions (user_id, group_id, share_units, amount_deposited, amount_withdrawn) VALUES ($1, $2, 600000000, 630000000, 30000000)`, base.adaID, base.sunday)
	execSQL(t, h.DB, `INSERT INTO positions (user_id, group_id, share_units, amount_deposited) VALUES ($1, $2, 400000000, 400000000)`, base.benID, base.sunday)
	execSQL(t, h.DB, `INSERT INTO positions (user_id, group_id, share_units, amount_deposited) VALUES ($1, $2, 80000000, 100000000)`, base.adaID, base.semis)

	hf.deposit(t, "ben-sunday", base.benID, base.sunday, 400_000_000, "confirmed", day(-10))
	hf.trade(t, "tesla-before", base.sunday, "buy", "member_proposal", "confirmed", 100_000_000, 100_000_000, 100_000_000, jupiter.TSLAxMint, day(-9))
	hf.deposit(t, "fund-semis", base.adaID, base.semis, 100_000_000, "confirmed", day(-8.5))
	hf.deposit(t, "fund-sunday", base.adaID, base.sunday, 600_000_000, "confirmed", day(-8))
	hf.trade(t, "buy-apple", base.sunday, "buy", "member_proposal", "confirmed", 800_000_000, 800_000_000, 400_000_000, jupiter.AAPLxMint, day(-7))
	hf.ids["cashout-paid"] = queryID(t, h.DB,
		`INSERT INTO withdrawals (user_id, group_id, amount, to_address, status, tx_signature, created_at)
		 VALUES ($1, $2, 30000000, 'ada-wallet', 'confirmed', $3, $4) RETURNING id`,
		base.adaID, base.sunday, testTxSignature(h.ISO, "cashout-paid"), day(-6.5))
	hf.trade(t, "bot-sell", base.sunday, "sell", "agent", "confirmed", 100_000_000, 260_000_000, 260_000_000, jupiter.AAPLxMint, day(-6))
	hf.trade(t, "buy-pending", base.sunday, "buy", "member_proposal", "pending", 50_000_000, 0, 0, jupiter.AAPLxMint, day(-5))
	hf.ids["withdrawal"] = queryID(t, h.DB,
		`INSERT INTO platform_withdrawals (user_id, amount, to_address, status, tx_signature, created_at)
		 VALUES ($1, 25000000, 'outside-address', 'confirmed', $2, $3) RETURNING id`,
		base.adaID, testTxSignature(h.ISO, "withdrawal"), day(-4))
	hf.ids["cashout-pending"] = queryID(t, h.DB,
		`INSERT INTO redeem_jobs (group_id, user_id, share_units, slice_usdc, payout_address, status, created_at)
		 VALUES ($1, $2, 20000000, 70000000, 'ada-wallet', 'selling', $3) RETURNING id`,
		base.semis, base.adaID, day(-3))
	hf.ids["cashout-failed"] = queryID(t, h.DB,
		`INSERT INTO redeem_payouts (redeem_job_id, user_id, group_id, amount, to_address, tx_signature,
		   signed_tx, last_valid_block_height, status, failure_reason, created_at)
		 VALUES (gen_random_uuid(), $1, $2, 40000000, 'ada-wallet', $3, 'signed', 1, 'failed', 'blockhash expired', $4) RETURNING id`,
		base.adaID, base.semis, testTxSignature(h.ISO, "cashout-failed"), day(-2))
	hf.deposit(t, "fund-pending", base.adaID, base.sunday, 50_000_000, "pending", day(-1))
	return hf
}

func (hf historyFixture) deposit(t *testing.T, label, userID, groupID string, micros int64, status string, at time.Time) {
	t.Helper()
	var signature any
	if status == "confirmed" {
		signature = testTxSignature(hf.fx.h.ISO, "hist-"+label)
	}
	hf.ids[label] = queryID(t, hf.fx.h.DB,
		`INSERT INTO deposits (user_id, group_id, amount, from_address, status, tx_signature, created_at)
		 VALUES ($1, $2, $3, 'wallet', $4, $5, $6) RETURNING id`,
		userID, groupID, micros, status, signature, at)
}

// trade writes a cabal swap. For a buy, amount is USDC in and fill is tokens out; for a sell,
// amount is tokens in and fill is USDC out. A zero fill leaves the row unfilled.
func (hf historyFixture) trade(t *testing.T, label, groupID, action, initiatedBy, status string, amount, price, fill int64, mint string, at time.Time) {
	t.Helper()
	input, output := jupiter.USDCMint, mint
	if action == "sell" {
		input, output = mint, jupiter.USDCMint
	}
	var costPrice, costAmount any
	if fill > 0 {
		costPrice, costAmount = price, fill
	}
	var signature any
	if status == "confirmed" {
		signature = testTxSignature(hf.fx.h.ISO, "hist-"+label)
	}
	hf.ids[label] = queryID(t, hf.fx.h.DB,
		`INSERT INTO transactions (group_id, amount, action, input_mint, output_mint, status, tx_signature,
		   execute_request_id, cost_basis_price, cost_basis_amount, initiated_by, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
		groupID, amount, action, input, output, status, signature, testRequestID(hf.fx.h.ISO, "hist-"+label),
		costPrice, costAmount, initiatedBy, at)
}

func (hf historyFixture) list(t *testing.T, filter HistoryFilter, cursor string, limit int) HistoryPage {
	t.Helper()
	page, err := hf.fx.portfolio.ListHistory(context.Background(), hf.fx.token, filter, cursor, limit)
	if err != nil {
		t.Fatalf("ListHistory(%s, %q, %d): %v", filter, cursor, limit, err)
	}
	return page
}

// labels maps each item back to the fixture row it came from.
func (hf historyFixture) labels(items []HistoryItem) []string {
	byID := make(map[string]string, len(hf.ids))
	for label, id := range hf.ids {
		byID[id] = label
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		label, ok := byID[item.ID]
		if !ok {
			label = "unknown:" + item.Kind
		}
		out = append(out, label)
	}
	return out
}

func (hf historyFixture) item(t *testing.T, items []HistoryItem, label string) HistoryItem {
	t.Helper()
	for _, item := range items {
		if item.ID == hf.ids[label] {
			return item
		}
	}
	t.Fatalf("%s not in history", label)
	return HistoryItem{}
}

func TestListHistory_everyKindNewestFirstAndOnlyAdasOwn(t *testing.T) {
	// Arrange
	hf := newHistoryFixture(t)

	// Act
	page := hf.list(t, HistoryFilterAll, "", HistoryMaxLimit)

	// Assert
	if got, want := strings.Join(hf.labels(page.Items), ","), strings.Join(historyNewestFirst, ","); got != want {
		t.Fatalf("history =\n  %s\nwant\n  %s", got, want)
	}
	if page.NextCursor != "" {
		t.Errorf("nextCursor = %q, want none on the only page", page.NextCursor)
	}
}

// Ada holds 60% of Sunday, so she owns 60% of the trade: $480 of the $800 and 2.4 of the
// 4 shares.
func TestListHistory_attributesACabalBuyBySlice(t *testing.T) {
	// Arrange
	hf := newHistoryFixture(t)

	// Act
	buy := hf.item(t, hf.list(t, HistoryFilterAll, "", HistoryMaxLimit).Items, "buy-apple")

	// Assert
	if buy.Kind != "buy" || buy.Status != HistoryStatusDone {
		t.Errorf("kind/status = %s/%s, want buy/done", buy.Kind, buy.Status)
	}
	if buy.AmountMicros == nil || *buy.AmountMicros != 480_000_000 {
		t.Errorf("amount = %v, want 480000000", buy.AmountMicros)
	}
	if buy.Quantity != "2.4" || buy.Symbol != "AAPLx" || buy.Name != "Apple" {
		t.Errorf("quantity/symbol/name = %q/%q/%q, want 2.4/AAPLx/Apple", buy.Quantity, buy.Symbol, buy.Name)
	}
	if buy.GroupID != hf.fx.sunday || buy.TransactionID != hf.ids["buy-apple"] {
		t.Errorf("group/transaction = %s/%s, want Sunday and the swap id", buy.GroupID, buy.TransactionID)
	}
}

func TestListHistory_eachRowSaysWhatHappenedInTheMembersTerms(t *testing.T) {
	// Arrange
	hf := newHistoryFixture(t)
	items := hf.list(t, HistoryFilterAll, "", HistoryMaxLimit).Items

	cases := []struct {
		label, kind, status string
		amount              int64
		quantity            string
		hasAmount           bool
	}{
		{"bot-sell", "bot_sell", HistoryStatusDone, 156_000_000, "0.6", true},
		{"buy-pending", "buy", HistoryStatusPending, 30_000_000, "", true},
		{"fund-pending", "fund", HistoryStatusPending, 50_000_000, "", true},
		{"fund-sunday", "fund", HistoryStatusDone, 600_000_000, "", true},
		{"cashout-paid", "cash_out", HistoryStatusDone, 30_000_000, "", true},
		{"cashout-pending", "cash_out", HistoryStatusPending, 70_000_000, "", true},
		{"cashout-failed", "cash_out", HistoryStatusFailed, 40_000_000, "", true},
		{"withdrawal", "withdrawal", HistoryStatusDone, 25_000_000, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			// Act
			item := hf.item(t, items, tc.label)

			// Assert
			if item.Kind != tc.kind || item.Status != tc.status {
				t.Errorf("kind/status = %s/%s, want %s/%s", item.Kind, item.Status, tc.kind, tc.status)
			}
			if item.AmountMicros == nil || *item.AmountMicros != tc.amount {
				t.Errorf("amount = %v, want %d", item.AmountMicros, tc.amount)
			}
			if item.Quantity != tc.quantity {
				t.Errorf("quantity = %q, want %q", item.Quantity, tc.quantity)
			}
		})
	}
	if withdrawal := hf.item(t, items, "withdrawal"); withdrawal.GroupID != "" || withdrawal.GroupName != "" {
		t.Errorf("withdrawal cabal = %q/%q, want none: it left the account, not a cabal", withdrawal.GroupID, withdrawal.GroupName)
	}
}

func TestListHistory_pagesWithoutRepeatingOrDroppingARow(t *testing.T) {
	// Arrange
	hf := newHistoryFixture(t)

	// Act
	var got []HistoryItem
	cursor, pages := "", 0
	for {
		page := hf.list(t, HistoryFilterAll, cursor, 4)
		got = append(got, page.Items...)
		pages++
		if page.NextCursor == "" {
			break
		}
		if pages > 5 {
			t.Fatal("pagination did not end")
		}
		cursor = page.NextCursor
	}

	// Assert
	if pages != 3 {
		t.Errorf("pages = %d, want 3 (4 + 4 + 2)", pages)
	}
	if strings.Join(hf.labels(got), ",") != strings.Join(historyNewestFirst, ",") {
		t.Errorf("paged history = %v, want %v", hf.labels(got), historyNewestFirst)
	}
}

// Two rows written in the same instant still page cleanly: the id breaks the tie.
func TestListHistory_pagesAcrossRowsThatShareATimestamp(t *testing.T) {
	// Arrange
	hf := newHistoryFixture(t)
	execSQL(t, hf.fx.h.DB, `UPDATE deposits SET created_at = '2026-09-01T12:00:00Z' WHERE user_id = $1`, hf.fx.adaID)

	// Act
	first := hf.list(t, HistoryFilterMoneyIn, "", 1)
	second := hf.list(t, HistoryFilterMoneyIn, first.NextCursor, 1)
	third := hf.list(t, HistoryFilterMoneyIn, second.NextCursor, 1)

	// Assert
	seen := map[string]bool{}
	for _, page := range []HistoryPage{first, second, third} {
		if len(page.Items) != 1 {
			t.Fatalf("page = %d items, want 1", len(page.Items))
		}
		if seen[page.Items[0].ID] {
			t.Fatalf("row %s came back twice", page.Items[0].ID)
		}
		seen[page.Items[0].ID] = true
	}
	if third.NextCursor != "" {
		t.Errorf("nextCursor after the last deposit = %q, want none", third.NextCursor)
	}
}

func TestListHistory_filtersByWhatTheMemberAskedFor(t *testing.T) {
	cases := []struct {
		filter HistoryFilter
		want   []string
	}{
		{HistoryFilterMoneyIn, []string{"fund-pending", "fund-sunday", "fund-semis"}},
		{HistoryFilterCashOut, []string{"cashout-failed", "cashout-pending", "withdrawal", "cashout-paid"}},
		{HistoryFilterBuy, []string{"buy-pending", "buy-apple"}},
		{HistoryFilterSell, []string{"bot-sell"}},
	}
	hf := newHistoryFixture(t)
	for _, tc := range cases {
		t.Run(string(tc.filter), func(t *testing.T) {
			got := hf.labels(hf.list(t, tc.filter, "", HistoryMaxLimit).Items)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("%s = %v, want %v", tc.filter, got, tc.want)
			}
		})
	}
}

// Once Ada has cashed all the way out of Sunday she holds no slice of it, so none of its
// trades are hers to list; her own money rows stay.
func TestListHistory_dropsTheTradesOfACabalTheMemberLeft(t *testing.T) {
	// Arrange
	hf := newHistoryFixture(t)
	execSQL(t, hf.fx.h.DB, `UPDATE positions SET share_units = 0 WHERE user_id = $1 AND group_id = $2`, hf.fx.adaID, hf.fx.sunday)

	// Act
	got := hf.labels(hf.list(t, HistoryFilterAll, "", HistoryMaxLimit).Items)

	// Assert
	for _, label := range got {
		if label == "buy-apple" || label == "bot-sell" || label == "buy-pending" {
			t.Errorf("history still lists %s from a cabal Ada has left: %v", label, got)
		}
	}
	if !strings.Contains(strings.Join(got, ","), "fund-sunday") {
		t.Errorf("history dropped Ada's own fund into Sunday: %v", got)
	}
}

func TestListHistory_refusesACursorItDidNotIssue(t *testing.T) {
	hf := newHistoryFixture(t)
	for _, cursor := range []string{"garbage", EncodeHistoryCursor(postgres.MemberHistoryCursor{At: time.Now(), ID: "not-a-uuid"})} {
		_, err := hf.fx.portfolio.ListHistory(context.Background(), hf.fx.token, HistoryFilterAll, cursor, 10)
		if !errors.Is(err, ErrInvalidHistoryQuery) {
			t.Errorf("cursor %q: err = %v, want ErrInvalidHistoryQuery", cursor, err)
		}
	}
}

func TestExportHistory_returnsEveryRowAcrossPages(t *testing.T) {
	// Arrange
	hf := newHistoryFixture(t)

	// Act
	items, truncated, err := hf.fx.portfolio.ExportHistory(context.Background(), hf.fx.token, HistoryFilterAll)

	// Assert
	if err != nil {
		t.Fatalf("ExportHistory: %v", err)
	}
	if truncated || len(items) != len(historyNewestFirst) {
		t.Errorf("export = %d rows (truncated %v), want %d", len(items), truncated, len(historyNewestFirst))
	}
}

func TestParseHistoryFilterAndLimit(t *testing.T) {
	filters := map[string]HistoryFilter{"": HistoryFilterAll, "ALL": HistoryFilterAll, "money_in": HistoryFilterMoneyIn, " sell ": HistoryFilterSell}
	for raw, want := range filters {
		if got, err := ParseHistoryFilter(raw); err != nil || got != want {
			t.Errorf("ParseHistoryFilter(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := ParseHistoryFilter("deposits"); !errors.Is(err, ErrInvalidHistoryQuery) {
		t.Errorf("ParseHistoryFilter(deposits) err = %v, want ErrInvalidHistoryQuery", err)
	}
	limits := []struct {
		raw  string
		want int
		ok   bool
	}{
		{"", HistoryDefaultLimit, true},
		{"10", 10, true},
		{"500", HistoryMaxLimit, true},
		{"0", 0, false},
		{"-3", 0, false},
		{"ten", 0, false},
		{"10abc", 0, false},
	}
	for _, tc := range limits {
		t.Run(fmt.Sprintf("limit=%q", tc.raw), func(t *testing.T) {
			got, err := ParseHistoryLimit(tc.raw)
			if tc.ok && (err != nil || got != tc.want) {
				t.Errorf("ParseHistoryLimit(%q) = %d, %v; want %d", tc.raw, got, err, tc.want)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidHistoryQuery) {
				t.Errorf("ParseHistoryLimit(%q) err = %v, want ErrInvalidHistoryQuery", tc.raw, err)
			}
		})
	}
}

func TestHistoryStatus_foldsEveryTablesWords(t *testing.T) {
	cases := map[string]string{
		"confirmed":       HistoryStatusDone,
		"settled":         HistoryStatusDone,
		"pending":         HistoryStatusPending,
		"debited":         HistoryStatusPending,
		"selling":         HistoryStatusPending,
		"paying":          HistoryStatusPending,
		"failed":          HistoryStatusFailed,
		"failed: sweep":   HistoryStatusFailed,
		"dropped":         HistoryStatusFailed,
		" Confirmed ":     HistoryStatusDone,
		"something newer": HistoryStatusPending,
	}
	for raw, want := range cases {
		if got := historyStatus(raw); got != want {
			t.Errorf("historyStatus(%q) = %q, want %q", raw, got, want)
		}
	}
}
