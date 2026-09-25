package app

// Server-side limits on a trading agent: it may only sell what it bought itself, concurrent
// intents cannot overshoot the voted allocation, a resend under an idempotency key never
// trades twice, the intent audit trail is repaired from the ledger, and keys minted before
// the long format keep authenticating.

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

type agentLimitsFixture struct {
	governanceHarness
	GroupID string
	Key     string
	Intents *AgentIntentService
}

func newAgentLimitsFixture(t *testing.T, label string, treasuryUSDC, allocation int64) agentLimitsFixture {
	t.Helper()
	h := integrationGovernanceApp(t)
	proposer := openTestSession(t, h.ISO, h.Sessions, h.Privy, label, "Agent Limits")
	token := h.ISO.UniqueToken(label)
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, label), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, treasuryUSDC)
	_, key := addAgentAndReveal(t, h, created.GroupID, proposer.UserID, allocation)
	return agentLimitsFixture{
		governanceHarness: h,
		GroupID:           created.GroupID,
		Key:               key,
		Intents:           NewAgentIntentService(h.Store, h.Swap, h.Symbols),
	}
}

func (f agentLimitsFixture) submit(side domain.AgentIntentSide, symbol string, amount int64, idempotencyKey string) (SubmitAgentIntentResult, error) {
	in := SubmitAgentIntentInput{
		GroupID:        f.GroupID,
		AgentKey:       f.Key,
		Side:           side,
		Symbol:         symbol,
		IdempotencyKey: idempotencyKey,
	}
	if side == domain.AgentIntentBuy {
		in.UsdcMicros = amount
	} else {
		in.TokenAmount = amount
	}
	return f.Intents.SubmitAgentIntent(context.Background(), in)
}

// seedMemberBoughtPosition records a confirmed buy the cabal voted for, not the agent.
func (f agentLimitsFixture) seedMemberBoughtPosition(t *testing.T, mint string, tokens int64) {
	t.Helper()
	if _, _, err := f.Store.ConfirmBuyTransaction(context.Background(), postgres.ConfirmBuyTransactionParams{
		GroupID:          f.GroupID,
		Amount:           tokens,
		InputMint:        jupiter.USDCMint,
		OutputMint:       mint,
		TxSignature:      testTxSignature(f.ISO, "member-buy"),
		ExecuteRequestID: testRequestID(f.ISO, "member-buy"),
		CostBasisPrice:   tokens,
		CostBasisAmount:  tokens,
	}); err != nil {
		t.Fatalf("seed member-bought position: %v", err)
	}
}

func (f agentLimitsFixture) countTransactions(t *testing.T, where string) int {
	t.Helper()
	var n int
	if err := f.DB.QueryRowContext(context.Background(),
		`SELECT count(*) FROM transactions WHERE group_id = $1 AND `+where, f.GroupID).Scan(&n); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	return n
}

func registerAgentSellFill(f agentLimitsFixture, mint string, tokens int64) {
	registerAgentSellFillWithProceeds(f, mint, tokens, tokens)
}

func registerAgentSellFillWithProceeds(f agentLimitsFixture, mint string, tokens, proceeds int64) {
	requestID := fmt.Sprintf("agent-sell-%s-%d", f.ISO.Suffix(), tokens)
	jupiter.RegisterSellQuote(f.Jupiter, mint, tokens, jupiter.SellQuote{
		Routable:    true,
		InputMint:   mint,
		OutputMint:  jupiter.USDCMint,
		InAmount:    strconv.FormatInt(tokens, 10),
		OutAmount:   strconv.FormatInt(proceeds, 10),
		RequestID:   requestID,
		Transaction: "unsigned-sell-tx",
	})
	jupiter.RegisterExecutePoll(f.Jupiter, requestID, []jupiter.ExecuteResult{{
		Status:             jupiter.ExecuteStatusSuccess,
		Code:               0,
		Signature:          "sig-" + requestID,
		InputAmountResult:  strconv.FormatInt(tokens, 10),
		OutputAmountResult: strconv.FormatInt(proceeds, 10),
	}})
}

// Buy $100 with a $100 allocation, sell it for $110: the agent may now spend $110.
func TestAgentIntent_sellProceedsRefillBudget(t *testing.T) {
	f := newAgentLimitsFixture(t, "refill", 1_000_000_000, 100_000_000)
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 100_000_000)
	if result, err := f.submit(domain.AgentIntentBuy, "AAPLx", 100_000_000, ""); err != nil || result.Status != "executed" {
		t.Fatalf("agent buy: result = %+v, err = %v", result, err)
	}
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 1_000_000)
	if _, err := f.submit(domain.AgentIntentBuy, "AAPLx", 1_000_000, ""); !errors.Is(err, ErrAgentIntentRejected) {
		t.Fatalf("buy with the allocation spent: err = %v, want ErrAgentIntentRejected", err)
	}

	registerAgentSellFillWithProceeds(f, "MintAAPLx", 100_000_000, 110_000_000)
	if result, err := f.submit(domain.AgentIntentSell, "AAPLx", 100_000_000, ""); err != nil || result.Status != "executed" {
		t.Fatalf("agent sell: result = %+v, err = %v", result, err)
	}

	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 110_000_001)
	if _, err := f.submit(domain.AgentIntentBuy, "AAPLx", 110_000_001, ""); !errors.Is(err, ErrAgentIntentRejected) {
		t.Fatalf("buy one micro past the refilled $110: err = %v, want ErrAgentIntentRejected", err)
	}
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 110_000_000)
	if result, err := f.submit(domain.AgentIntentBuy, "AAPLx", 110_000_000, ""); err != nil || result.Status != "executed" {
		t.Fatalf("buy of the refilled $110: result = %+v, err = %v", result, err)
	}
}

func TestAgentIntent_sellOfMemberBoughtPositionRejected(t *testing.T) {
	f := newAgentLimitsFixture(t, "sell-member", 10_000_000, 5_000_000)
	// The agent never bought AAPLx; this registers the symbol and nothing more.
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 1_000_000)
	f.seedMemberBoughtPosition(t, "MintAAPLx", 8_000_000)
	registerAgentSellFill(f, "MintAAPLx", 8_000_000)

	for _, tokens := range []int64{8_000_000, 1} {
		result, err := f.submit(domain.AgentIntentSell, "AAPLx", tokens, "")
		if !errors.Is(err, ErrAgentIntentRejected) {
			t.Fatalf("sell %d of a member-bought position: err = %v, want ErrAgentIntentRejected", tokens, err)
		}
		if result.Status != "rejected" || !strings.Contains(result.RejectReason, "agent position") {
			t.Fatalf("sell %d: result = %+v, want a rejection naming the agent position", tokens, result)
		}
	}
	if n := f.countTransactions(t, `action = 'sell'`); n != 0 {
		t.Fatalf("%d sell transaction(s) recorded, want none: the swap path must not be reached", n)
	}
}

func TestAgentIntent_sellOwnPositionAllowedUpToNetQuantity(t *testing.T) {
	f := newAgentLimitsFixture(t, "sell-own", 10_000_000, 5_000_000)
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 3_000_000)
	// Members hold more of the same stock; the agent's limit is its own 3.0, not the 8.0 total.
	f.seedMemberBoughtPosition(t, "MintAAPLx", 5_000_000)

	if result, err := f.submit(domain.AgentIntentBuy, "AAPLx", 3_000_000, ""); err != nil || result.Status != "executed" {
		t.Fatalf("agent buy: result = %+v, err = %v", result, err)
	}

	registerAgentSellFill(f, "MintAAPLx", 3_000_001)
	if _, err := f.submit(domain.AgentIntentSell, "AAPLx", 3_000_001, ""); !errors.Is(err, ErrAgentIntentRejected) {
		t.Fatalf("sell one atomic past the agent's buys: err = %v, want ErrAgentIntentRejected", err)
	}

	registerAgentSellFill(f, "MintAAPLx", 2_000_000)
	if result, err := f.submit(domain.AgentIntentSell, "AAPLx", 2_000_000, ""); err != nil || result.Status != "executed" {
		t.Fatalf("sell part of own position: result = %+v, err = %v", result, err)
	}

	// 1.0 left: its own earlier sell counts against what it bought.
	registerAgentSellFill(f, "MintAAPLx", 1_000_001)
	if _, err := f.submit(domain.AgentIntentSell, "AAPLx", 1_000_001, ""); !errors.Is(err, ErrAgentIntentRejected) {
		t.Fatalf("sell past the net position: err = %v, want ErrAgentIntentRejected", err)
	}
	registerAgentSellFill(f, "MintAAPLx", 1_000_000)
	if result, err := f.submit(domain.AgentIntentSell, "AAPLx", 1_000_000, ""); err != nil || result.Status != "executed" {
		t.Fatalf("sell the rest of own position: result = %+v, err = %v", result, err)
	}

	if n := f.countTransactions(t, `action = 'sell' AND status = 'confirmed'`); n != 2 {
		t.Fatalf("confirmed sells = %d, want 2", n)
	}
}

func TestAgentIntent_unknownSellSymbolRejected(t *testing.T) {
	f := newAgentLimitsFixture(t, "sell-unknown", 10_000_000, 5_000_000)
	result, err := f.submit(domain.AgentIntentSell, "NOPEx", 1_000, "")
	if !errors.Is(err, ErrAgentIntentRejected) || result.Status != "rejected" {
		t.Fatalf("sell of an unknown symbol: result = %+v, err = %v, want a rejection", result, err)
	}
}

func TestAgentIntent_concurrentBuysCannotExceedAllocation(t *testing.T) {
	const allocation = 5_000_000
	const callers = 8
	f := newAgentLimitsFixture(t, "race-cap", 100_000_000, allocation)

	// Distinct amounts: the fake Jupiter keys quotes (and so execute request ids) by amount.
	amounts := make([]int64, callers)
	for i := range amounts {
		amounts[i] = 1_500_000 + int64(i)
		registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", amounts[i])
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, callers)
	for i := range amounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = f.submit(domain.AgentIntentBuy, "AAPLx", amounts[i], "")
		}()
	}
	close(start)
	wg.Wait()

	executed := 0
	for i, err := range errs {
		switch {
		case err == nil:
			executed++
		case errors.Is(err, ErrAgentIntentRejected) && strings.Contains(err.Error(), "allocation"):
		default:
			t.Fatalf("caller %d: unexpected error %v", i, err)
		}
	}
	// Three buys of ~$1.50 fit in $5; a fourth does not.
	if executed != 3 {
		t.Fatalf("executed buys = %d, want 3", executed)
	}
	var spent int64
	if err := f.DB.QueryRowContext(context.Background(), `
SELECT COALESCE(SUM(amount), 0) FROM transactions
WHERE group_id = $1 AND action = 'buy' AND initiated_by = 'agent' AND status IN ('pending', 'confirmed')`,
		f.GroupID).Scan(&spent); err != nil {
		t.Fatalf("sum agent buys: %v", err)
	}
	if spent > allocation {
		t.Fatalf("agent spent %d, over its %d allocation", spent, allocation)
	}
}

func TestAgentIntent_inFlightIntentReservesBudget(t *testing.T) {
	f := newAgentLimitsFixture(t, "reserve", 100_000_000, 5_000_000)
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 3_000_000)

	var agentID string
	if err := f.DB.QueryRowContext(context.Background(),
		`SELECT id FROM group_agents WHERE group_id = $1`, f.GroupID).Scan(&agentID); err != nil {
		t.Fatalf("agent id: %v", err)
	}
	// Another request's buy: accepted, its swap not yet on the ledger.
	inFlight, err := f.Store.InsertAgentIntent(context.Background(), postgres.AgentIntentRow{
		GroupAgentID: agentID,
		GroupID:      f.GroupID,
		Side:         domain.AgentIntentBuy,
		Symbol:       "AAPLx",
		UsdcMicros:   nullInt64(3_000_000),
		Status:       "accepted",
	})
	if err != nil {
		t.Fatalf("insert in-flight intent: %v", err)
	}

	if _, err := f.submit(domain.AgentIntentBuy, "AAPLx", 3_000_000, ""); !errors.Is(err, ErrAgentIntentRejected) {
		t.Fatalf("buy while $3 of $5 is reserved: err = %v, want ErrAgentIntentRejected", err)
	}

	// The process that accepted it died before the swap: past the abandon window the
	// reservation lapses and the row says so.
	if _, err := f.DB.ExecContext(context.Background(),
		`UPDATE agent_intents SET created_at = now() - interval '1 hour' WHERE id = $1`, inFlight.ID); err != nil {
		t.Fatalf("age in-flight intent: %v", err)
	}
	if result, err := f.submit(domain.AgentIntentBuy, "AAPLx", 3_000_000, ""); err != nil || result.Status != "executed" {
		t.Fatalf("buy after the reservation lapsed: result = %+v, err = %v", result, err)
	}
	abandoned, _, err := f.Store.GetAgentIntentByID(context.Background(), inFlight.ID)
	if err != nil {
		t.Fatalf("get abandoned intent: %v", err)
	}
	if abandoned.Status != "failed" || abandoned.RejectReason.String == "" {
		t.Fatalf("abandoned intent = %+v, want failed with a reason", abandoned)
	}
}

func TestAgentIntent_statusRepairedFromLedgerAfterLostWrite(t *testing.T) {
	f := newAgentLimitsFixture(t, "repair", 100_000_000, 5_000_000)
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 1_000_000)
	ctx := context.Background()

	var agentID string
	if err := f.DB.QueryRowContext(ctx, `SELECT id FROM group_agents WHERE group_id = $1`, f.GroupID).Scan(&agentID); err != nil {
		t.Fatalf("agent id: %v", err)
	}
	// A buy that filled, whose "executed" write never landed: the row still says accepted.
	stuck, err := f.Store.InsertAgentIntent(ctx, postgres.AgentIntentRow{
		GroupAgentID:   agentID,
		GroupID:        f.GroupID,
		Side:           domain.AgentIntentBuy,
		Symbol:         "AAPLx",
		UsdcMicros:     nullInt64(2_000_000),
		Status:         "accepted",
		IdempotencyKey: nullString("lost-write"),
	})
	if err != nil {
		t.Fatalf("insert stuck intent: %v", err)
	}
	requestID := testRequestID(f.ISO, "lost-write")
	if _, _, err := f.Store.InsertPendingTransaction(ctx, postgres.InsertPendingTransactionParams{
		GroupID:          f.GroupID,
		AgentIntentID:    stuck.ID,
		InitiatedBy:      "agent",
		Action:           postgres.TransactionActionBuy,
		InputMint:        jupiter.USDCMint,
		OutputMint:       "MintAAPLx",
		Amount:           2_000_000,
		ExecuteRequestID: requestID,
	}); err != nil {
		t.Fatalf("insert pending transaction: %v", err)
	}
	confirmed, _, err := f.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          f.GroupID,
		Amount:           2_000_000,
		InputMint:        jupiter.USDCMint,
		OutputMint:       "MintAAPLx",
		TxSignature:      testTxSignature(f.ISO, "lost-write"),
		ExecuteRequestID: requestID,
		CostBasisPrice:   2_000_000,
		CostBasisAmount:  2_000_000,
	})
	if err != nil {
		t.Fatalf("confirm transaction: %v", err)
	}

	// The bot's retry under the same key gets the fill, not "accepted" and not a second trade.
	replay, err := f.Intents.SubmitAgentIntent(ctx, SubmitAgentIntentInput{
		GroupID: f.GroupID, AgentKey: f.Key, Side: domain.AgentIntentBuy, Symbol: "AAPLx",
		UsdcMicros: 2_000_000, IdempotencyKey: "lost-write",
	})
	if err != nil {
		t.Fatalf("replay of a filled intent: %v", err)
	}
	// The replay read the row before the repair committed only if the order is wrong.
	repaired, _, err := f.Store.GetAgentIntentByID(ctx, stuck.ID)
	if err != nil {
		t.Fatalf("get repaired intent: %v", err)
	}
	if repaired.Status != "executed" || repaired.TransactionID.String != confirmed.ID {
		t.Fatalf("repaired intent = %+v, want executed with transaction %s", repaired, confirmed.ID)
	}
	if replay.IntentID != stuck.ID || replay.Status != "executed" || replay.TransactionID != confirmed.ID {
		t.Fatalf("replay = %+v, want the executed intent %s with transaction %s", replay, stuck.ID, confirmed.ID)
	}
	if n := f.countTransactions(t, `action = 'buy'`); n != 1 {
		t.Fatalf("buy transactions = %d, want 1", n)
	}
}

func TestAgentIntent_duplicateIdempotencyKeyReturnsOriginalResult(t *testing.T) {
	f := newAgentLimitsFixture(t, "idem", 100_000_000, 5_000_000)
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 3_000_000)

	first, err := f.submit(domain.AgentIntentBuy, "AAPLx", 3_000_000, "tick-1")
	if err != nil || first.Status != "executed" {
		t.Fatalf("first submit: result = %+v, err = %v", first, err)
	}
	quotesAfterFirst := jupiter.QuoteBuyCallCount(f.Jupiter)

	again, err := f.submit(domain.AgentIntentBuy, "AAPLx", 3_000_000, "tick-1")
	if err != nil {
		t.Fatalf("resend under the same key: %v", err)
	}
	if again != first {
		t.Fatalf("resend = %+v, want the original %+v", again, first)
	}
	if got := jupiter.QuoteBuyCallCount(f.Jupiter); got != quotesAfterFirst {
		t.Fatalf("resend reached Jupiter (%d quotes, was %d)", got, quotesAfterFirst)
	}
	if n := f.countTransactions(t, `action = 'buy'`); n != 1 {
		t.Fatalf("buy transactions = %d, want 1", n)
	}

	// The same key on a different trade is a bot bug, not a replay.
	if _, err := f.submit(domain.AgentIntentBuy, "AAPLx", 1_000_000, "tick-1"); !errors.Is(err, ErrAgentIntentRejected) || !strings.Contains(err.Error(), "different intent") {
		t.Fatalf("key reused for a different intent: err = %v", err)
	}

	// A rejection replays as the same rejection, on the same row.
	rejected, err := f.submit(domain.AgentIntentBuy, "AAPLx", 3_000_000, "tick-2")
	if !errors.Is(err, ErrAgentIntentRejected) {
		t.Fatalf("over-allocation buy: err = %v, want ErrAgentIntentRejected", err)
	}
	rejectedAgain, err := f.submit(domain.AgentIntentBuy, "AAPLx", 3_000_000, "tick-2")
	if !errors.Is(err, ErrAgentIntentRejected) || rejectedAgain != rejected {
		t.Fatalf("replayed rejection = %+v (err %v), want %+v", rejectedAgain, err, rejected)
	}

	// Without a key nothing is deduplicated.
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 1_000_000)
	if result, err := f.submit(domain.AgentIntentBuy, "AAPLx", 1_000_000, ""); err != nil || result.IntentID == first.IntentID {
		t.Fatalf("keyless buy: result = %+v, err = %v", result, err)
	}
}

func TestAgentIntent_concurrentDuplicateKeyTradesOnce(t *testing.T) {
	f := newAgentLimitsFixture(t, "idem-race", 100_000_000, 50_000_000)
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 2_000_000)

	const callers = 6
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = f.submit(domain.AgentIntentBuy, "AAPLx", 2_000_000, "same-tick")
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil && !errors.Is(err, ErrAgentIntentInFlight) {
			t.Fatalf("caller %d: unexpected error %v", i, err)
		}
	}
	var intents int
	if err := f.DB.QueryRowContext(context.Background(),
		`SELECT count(*) FROM agent_intents WHERE group_id = $1 AND idempotency_key = 'same-tick'`, f.GroupID).Scan(&intents); err != nil {
		t.Fatalf("count intents: %v", err)
	}
	if intents != 1 {
		t.Fatalf("intents under one key = %d, want 1", intents)
	}
	if n := f.countTransactions(t, `action = 'buy'`); n != 1 {
		t.Fatalf("buy transactions = %d, want 1", n)
	}
}

func TestAgentIntent_legacyShortKeyStillAuthenticates(t *testing.T) {
	f := newAgentLimitsFixture(t, "legacy-key", 100_000_000, 5_000_000)
	registerAgentBuyFill(t, f.Jupiter, f.XStocks, f.ISO.Suffix(), "AAPLx", 1_000_000)

	// An agent installed before the long format: five characters, no prefix.
	const legacyAlphabet = "23456789abcdefghjkmnpqrstuvwxyz"
	buf := make([]byte, 5)
	for i := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(legacyAlphabet))))
		if err != nil {
			t.Fatalf("legacy key: %v", err)
		}
		buf[i] = legacyAlphabet[n.Int64()]
	}
	legacyKey := string(buf)
	if IsCurrentAgentKeyFormat(legacyKey) {
		t.Fatalf("%q must not look like a current key", legacyKey)
	}
	if _, err := f.DB.ExecContext(context.Background(),
		`UPDATE group_agents SET api_key_hash = $2, api_key_prefix = $3, api_key = $3 WHERE group_id = $1`,
		f.GroupID, HashAgentAPIKey(legacyKey), legacyKey); err != nil {
		t.Fatalf("install legacy key: %v", err)
	}

	f.Key = legacyKey
	if result, err := f.submit(domain.AgentIntentBuy, "AAPLx", 1_000_000, ""); err != nil || result.Status != "executed" {
		t.Fatalf("intent under a legacy key: result = %+v, err = %v", result, err)
	}
}

func TestAgentAPIKey_hashIsUniqueAcrossAgents(t *testing.T) {
	a := newAgentLimitsFixture(t, "uniq-a", 10_000_000, 1_000_000)
	b := newAgentLimitsFixture(t, "uniq-b", 10_000_000, 1_000_000)
	_, err := a.DB.ExecContext(context.Background(),
		`UPDATE group_agents SET api_key_hash = $2 WHERE group_id = $1`, b.GroupID, HashAgentAPIKey(a.Key))
	if err == nil {
		t.Fatal("two live agents were given the same key hash; want a unique violation")
	}
}
