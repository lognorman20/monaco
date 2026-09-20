package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

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
