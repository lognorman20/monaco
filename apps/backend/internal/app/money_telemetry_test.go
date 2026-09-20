package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
	"github.com/monaco/monaco/apps/backend/internal/telemetry/telemetrytest"
	"github.com/monaco/monaco/packages/domain"
)

const (
	seriesIntentOK       = `monaco_money_events_total{event="agent_intent",outcome="ok"}`
	seriesIntentRejected = `monaco_money_events_total{event="agent_intent",outcome="rejected"}`
	seriesIntentError    = `monaco_money_events_total{event="agent_intent",outcome="error"}`
	seriesBuyOK          = `monaco_money_events_total{event="swap_buy",outcome="ok"}`
	seriesBuyVolume      = `monaco_money_volume_usdc_micros_total{event="swap_buy"}`
)

func TestMoneyOutcome_classification(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, "ok"},
		{context.Canceled, "canceled"},
		{fmt.Errorf("%w: over budget", ErrAgentIntentRejected), "rejected"},
		{ErrAgentPaused, "rejected"},
		{ErrInvalidAgentAPIKey, "rejected"},
		{fmt.Errorf("%w: below dust minimum", ErrInvalidRedeemRequest), "rejected"},
		{ErrInvalidPayoutProof, "rejected"},
		// Upstream and infrastructure failures are ours to page on.
		{errors.New("jupiter: status 429"), "error"},
		{context.DeadlineExceeded, "error"},
		{ErrRedeemPotIlliquid, "error"},
	}
	for _, tc := range cases {
		if got := moneyOutcome(tc.err); got != tc.want {
			t.Errorf("moneyOutcome(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// No t.Parallel: this test compares process-global counters around the calls.
func TestAgentIntent_metricsAndAuditLog_coverExecutedRejectedAndBadKey(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	proposer := openTestSession(t, h.ISO, h.Sessions, h.Privy, "intent-metrics", "Intent Metrics")
	token := h.ISO.UniqueToken("intent-metrics")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "intent-metrics"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 10_000_000)
	registerAgentBuyFill(t, h.Jupiter, h.XStocks, h.ISO.Suffix(), "AAPLx", 3_000_000)
	_, key := addAgentAndReveal(t, h, created.GroupID, proposer.UserID, 5_000_000)
	intents := NewAgentIntentService(h.Store, h.Swap, h.Symbols)

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	before := map[string]float64{}
	for _, series := range []string{seriesIntentOK, seriesIntentRejected, seriesIntentError, seriesBuyOK, seriesBuyVolume} {
		before[series] = telemetrytest.Value(t, series)
	}
	buy := SubmitAgentIntentInput{GroupID: created.GroupID, AgentKey: key, Side: domain.AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 3_000_000}

	// Act: one fill, one over-budget rejection, one bad key.
	if _, err := intents.SubmitAgentIntent(context.Background(), buy); err != nil {
		t.Fatalf("first buy: %v", err)
	}
	if _, err := intents.SubmitAgentIntent(context.Background(), buy); !errors.Is(err, ErrAgentIntentRejected) {
		t.Fatalf("second buy err = %v, want over-budget rejection", err)
	}
	badKey := buy
	badKey.AgentKey = "WRONG"
	if _, err := intents.SubmitAgentIntent(context.Background(), badKey); !errors.Is(err, ErrInvalidAgentAPIKey) {
		t.Fatalf("bad key err = %v", err)
	}

	// Assert: counters.
	want := map[string]float64{
		seriesIntentOK:       1,
		seriesIntentRejected: 2,
		seriesIntentError:    0,
		seriesBuyOK:          1,
		seriesBuyVolume:      3_000_000,
	}
	for series, delta := range want {
		if got := telemetrytest.Value(t, series) - before[series]; got != delta {
			t.Errorf("%s moved by %v, want %v", series, got, delta)
		}
	}

	// Assert: the service-layer audit lines exist and never carry the key.
	out := logs.String()
	for _, wantLine := range []string{`"msg":"agent intent executed"`, `"msg":"agent intent rejected"`} {
		if !strings.Contains(out, wantLine) {
			t.Errorf("log output missing %s", wantLine)
		}
	}
	if strings.Contains(out, key) || strings.Contains(out, "WRONG") {
		t.Fatal("an agent API key appeared in the logs")
	}
}

// The replay of a failed intent answers the bot without an error (200, status "failed").
// It used to count as outcome "ok" and log "agent intent executed".
func TestAgentIntent_replayOfFailedIntent_countsAndLogsAsFailure(t *testing.T) {
	// Arrange: an intent under an idempotency key whose swap failed.
	f := newAgentLimitsFixture(t, "replay-failed", 10_000_000, 5_000_000)
	var agentID string
	if err := f.DB.QueryRowContext(context.Background(),
		`SELECT id FROM group_agents WHERE group_id = $1`, f.GroupID).Scan(&agentID); err != nil {
		t.Fatalf("agent id: %v", err)
	}
	failed, err := f.Store.InsertAgentIntent(context.Background(), postgres.AgentIntentRow{
		GroupAgentID:   agentID,
		GroupID:        f.GroupID,
		Side:           domain.AgentIntentBuy,
		Symbol:         "AAPLx",
		UsdcMicros:     nullInt64(1_000_000),
		Status:         "failed",
		RejectReason:   nullString("jupiter execute: upstream 502"),
		IdempotencyKey: nullString("tick-failed"),
	})
	if err != nil {
		t.Fatalf("insert failed intent: %v", err)
	}

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	okBefore := telemetrytest.Value(t, seriesIntentOK)
	errorBefore := telemetrytest.Value(t, seriesIntentError)

	// Act
	replay, err := f.submit(domain.AgentIntentBuy, "AAPLx", 1_000_000, "tick-failed")

	// Assert: the bot's answer is unchanged...
	if err != nil || replay.IntentID != failed.ID || replay.Status != "failed" {
		t.Fatalf("replay = %+v, err = %v; want the failed intent %s and no error", replay, err, failed.ID)
	}
	// ...and the books say failure.
	if got := telemetrytest.Value(t, seriesIntentOK) - okBefore; got != 0 {
		t.Errorf("outcome=ok moved by %v for a replayed failed intent, want 0", got)
	}
	if got := telemetrytest.Value(t, seriesIntentError) - errorBefore; got != 1 {
		t.Errorf("outcome=error moved by %v, want 1", got)
	}
	out := logs.String()
	if strings.Contains(out, `"msg":"agent intent executed"`) {
		t.Errorf("replayed failed intent logged as executed: %s", out)
	}
	if !strings.Contains(out, `"msg":"agent intent failed"`) || !strings.Contains(out, `"outcome":"error"`) {
		t.Errorf("log output missing the failed line with outcome=error: %s", out)
	}
	if strings.Contains(out, "upstream 502") {
		t.Errorf("the stored internal error leaked into the replay log: %s", out)
	}
}

func TestAgentIntentOutcome_classification(t *testing.T) {
	cases := []struct {
		name   string
		result SubmitAgentIntentResult
		err    error
		want   string
	}{
		{name: "fill", result: SubmitAgentIntentResult{Status: "executed"}, want: telemetry.OutcomeOK},
		{name: "replayed failed intent", result: SubmitAgentIntentResult{Status: "failed"}, want: telemetry.OutcomeError},
		{name: "failed with its error", result: SubmitAgentIntentResult{Status: "failed"}, err: errors.New("boom"), want: telemetry.OutcomeError},
		{name: "rejected", result: SubmitAgentIntentResult{Status: "rejected"}, err: ErrAgentIntentRejected, want: telemetry.OutcomeRejected},
		{name: "replay still executing", result: SubmitAgentIntentResult{Status: "accepted"}, err: ErrAgentIntentInFlight, want: telemetry.OutcomeRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentIntentOutcome(tc.result, tc.err); got != tc.want {
				t.Fatalf("outcome = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRecordSwap_replayedFill_doesNotDoubleCountVolume(t *testing.T) {
	const event = "swap_buy"
	volume := telemetrytest.Value(t, seriesBuyVolume)
	replayed := telemetrytest.Value(t, `monaco_money_events_total{event="swap_buy",outcome="replayed"}`)

	recordSwap(event, 7_000_000, false, nil)

	if got := telemetrytest.Value(t, seriesBuyVolume); got != volume {
		t.Fatalf("volume moved %v -> %v on an idempotent replay", volume, got)
	}
	if got := telemetrytest.Value(t, `monaco_money_events_total{event="swap_buy",outcome="replayed"}`); got != replayed+1 {
		t.Fatalf("replayed = %v, want %v", got, replayed+1)
	}
}
