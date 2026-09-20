package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var watchlist = []Asset{
	{Symbol: "GOOGLx", SolanaMint: "mintG", Routable: true},
	{Symbol: "AAPLx", SolanaMint: "mintA", Routable: true},
}

// scriptedPrices replays one price map (or error) per tick.
type scriptedPrices struct {
	ticks []map[string]float64
	errs  map[int]error
	n     int
}

func (s *scriptedPrices) Prices(context.Context, []string) (map[string]float64, error) {
	i := s.n
	s.n++
	if err := s.errs[i]; err != nil {
		return nil, err
	}
	return s.ticks[i], nil
}

// intentServer records every intent the bot posts and answers with respond.
type intentServer struct {
	mu      sync.Mutex
	intents []Intent
	respond func(w http.ResponseWriter, n int)
}

func (s *intentServer) handler(w http.ResponseWriter, r *http.Request) {
	var intent Intent
	_ = json.NewDecoder(r.Body).Decode(&intent)
	s.mu.Lock()
	s.intents = append(s.intents, intent)
	n := len(s.intents)
	s.mu.Unlock()
	if s.respond == nil {
		_, _ = w.Write([]byte(`{"intentId":"intent-1","status":"executed","transactionId":"tx-1"}`))
		return
	}
	s.respond(w, n)
}

type harness struct {
	bot    *Bot
	server *intentServer
	log    *bytes.Buffer
	clock  time.Time
	slept  []time.Duration
}

// newHarness builds a bot on a fake one-minute clock with a 2-minute lookback, so the
// third tick is the first one that can decide.
func newHarness(t *testing.T, live bool, prices *scriptedPrices, budget *Budget) *harness {
	t.Helper()
	h := &harness{server: &intentServer{}, log: &bytes.Buffer{}, clock: t0}
	srv := httptest.NewServer(http.HandlerFunc(h.server.handler))
	t.Cleanup(srv.Close)
	cfg := Config{Interval: time.Minute, Lookback: 2 * time.Minute, Rule: Rule{BuyPct: 0.5, SellPct: 0.5}, Live: live}
	h.bot = NewBot(cfg, watchlist, NewMonacoClient(srv.URL, "group-1", testKey, srv.Client()), prices, budget, h.log, false)
	h.bot.now = func() time.Time { return h.clock }
	h.bot.sleep = func(_ context.Context, d time.Duration) error {
		h.slept = append(h.slept, d)
		h.clock = h.clock.Add(d)
		return nil
	}
	return h
}

func (h *harness) ticks(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := h.bot.Tick(context.Background()); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		h.clock = h.clock.Add(time.Minute)
	}
}

func flat(g, a float64) map[string]float64 { return map[string]float64{"mintG": g, "mintA": a} }

func TestBot_dryRunNeverPosts(t *testing.T) {
	prices := &scriptedPrices{ticks: []map[string]float64{flat(100, 200), flat(100, 200), flat(101, 200)}}
	h := newHarness(t, false, prices, NewBudget(1_000_000, 5_000_000))
	h.ticks(t, 3)

	if len(h.server.intents) != 0 {
		t.Fatalf("dry run posted %d intents", len(h.server.intents))
	}
	out := h.log.String()
	for _, want := range []string{"warming up (1m of 2m)", "GOOGLx  $ 101.00  +1.00% over 2m  buy signal", "→ would buy $1.00 of GOOGLx  (dry run, nothing sent)"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
	if h.bot.budget.Spent() != 1_000_000 {
		t.Fatalf("dry run should still walk the cap, spent %d", h.bot.budget.Spent())
	}
}

func TestBot_liveBuysStrongestSignalOncePerTick(t *testing.T) {
	prices := &scriptedPrices{ticks: []map[string]float64{flat(100, 200), flat(100, 200), flat(101, 206)}}
	h := newHarness(t, true, prices, NewBudget(1_000_000, 5_000_000))
	h.ticks(t, 3)

	if len(h.server.intents) != 1 {
		t.Fatalf("posted %d intents, want 1", len(h.server.intents))
	}
	got := h.server.intents[0]
	if got.Side != "buy" || got.Symbol != "AAPLx" || got.UsdcMicros != 1_000_000 || got.TokenAmount != 0 {
		t.Fatalf("intent %+v, want $1 of AAPLx (+3%% beats +1%%)", got)
	}
	if !strings.Contains(h.log.String(), "✓ filled  tx tx-1  intent intent-1  ($1.00 of $5.00 spent)") {
		t.Fatalf("log:\n%s", h.log.String())
	}
}

func TestBot_cooldownStopsRepeatBuysOnTheSameSignal(t *testing.T) {
	prices := &scriptedPrices{ticks: []map[string]float64{flat(100, 200), flat(100, 200), flat(101, 200), flat(102, 200)}}
	h := newHarness(t, true, prices, NewBudget(1_000_000, 5_000_000))
	h.ticks(t, 4)
	if len(h.server.intents) != 1 {
		t.Fatalf("posted %d intents, want 1 within one lookback", len(h.server.intents))
	}
	if !strings.Contains(h.log.String(), "cooling down after last trade") {
		t.Fatalf("log:\n%s", h.log.String())
	}
}

func TestBot_totalCapStopsBuying(t *testing.T) {
	prices := &scriptedPrices{ticks: []map[string]float64{flat(100, 200), flat(100, 200), flat(101, 200), flat(101, 200), flat(101, 204)}}
	h := newHarness(t, true, prices, NewBudget(1_000_000, 1_500_000))
	h.ticks(t, 5)
	if len(h.server.intents) != 1 {
		t.Fatalf("posted %d intents, want 1 under a $1.50 cap", len(h.server.intents))
	}
	if !strings.Contains(h.log.String(), "spend cap reached ($1.00 of $1.50), not buying") {
		t.Fatalf("log:\n%s", h.log.String())
	}
}

func TestBot_sellsItsOwnEstimatedPositionOnTheWayDown(t *testing.T) {
	prices := &scriptedPrices{ticks: []map[string]float64{
		flat(100, 200), flat(100, 200), flat(101, 200), // buy GOOGLx at 101
		flat(101, 200), flat(101, 200), flat(100, 200), // -0.99% over 2m: sell
	}}
	h := newHarness(t, true, prices, NewBudget(1_000_000, 5_000_000))
	h.ticks(t, 6)
	if len(h.server.intents) != 2 {
		t.Fatalf("posted %d intents, want buy then sell", len(h.server.intents))
	}
	sell := h.server.intents[1]
	if sell.Side != "sell" || sell.Symbol != "GOOGLx" || sell.UsdcMicros != 0 || sell.TokenAmount != estimateFill(1_000_000, 101) {
		t.Fatalf("sell %+v", sell)
	}
	if h.bot.positions["GOOGLx"] != 0 {
		t.Fatal("position should be flat after the sell")
	}
}

func TestBot_allocationRejectionStopsFurtherBuys(t *testing.T) {
	prices := &scriptedPrices{ticks: []map[string]float64{flat(100, 200), flat(100, 200), flat(101, 200), flat(101, 204)}}
	h := newHarness(t, true, prices, NewBudget(1_000_000, 5_000_000))
	h.server.respond = func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"agent intent rejected: trade exceeds agent allocation"}`))
	}
	h.ticks(t, 4)
	if len(h.server.intents) != 1 {
		t.Fatalf("posted %d intents, want 1: the server said the budget is gone", len(h.server.intents))
	}
	if !strings.Contains(h.log.String(), "✗ rejected by Monaco: trade exceeds agent allocation") {
		t.Fatalf("log:\n%s", h.log.String())
	}
	if h.bot.positions["GOOGLx"] != 0 {
		t.Fatal("a rejected buy must not open a position")
	}
}

func TestBot_badKeyStopsTheBot(t *testing.T) {
	prices := &scriptedPrices{ticks: []map[string]float64{flat(100, 200), flat(100, 200), flat(101, 200)}}
	h := newHarness(t, true, prices, NewBudget(1_000_000, 5_000_000))
	h.server.respond = func(w http.ResponseWriter, _ int) { w.WriteHeader(http.StatusUnauthorized) }
	h.ticks(t, 2)
	_, err := h.bot.Tick(context.Background())
	if !errors.Is(err, ErrBadKey) {
		t.Fatalf("got %v, want ErrBadKey", err)
	}
	if strings.Contains(h.log.String()+err.Error(), testKey) {
		t.Fatal("the key must never be printed")
	}
}

func TestBot_throttleAndPauseStandDownThenResume(t *testing.T) {
	for name, tc := range map[string]struct {
		status  int
		header  string
		want    string
		intents int // the refused buy plus the fills after the stand-down ends
	}{
		// Retry-After 2m swallows the AAPLx signal at +1m; only the +3m one goes out.
		"429": {http.StatusTooManyRequests, "120", "throttled by Monaco, standing down for 2m", 2},
		// A pause is re-checked after 1m, so both later AAPLx signals go out.
		"403": {http.StatusForbidden, "", "the cabal has paused this agent", 3},
	} {
		t.Run(name, func(t *testing.T) {
			prices := &scriptedPrices{ticks: []map[string]float64{
				flat(100, 200), flat(100, 200), flat(101, 200), // GOOGLx buy -> refused
				flat(101, 204), // AAPLx buy signal one minute later
				flat(101, 204), flat(101, 209),
			}}
			h := newHarness(t, true, prices, NewBudget(1_000_000, 5_000_000))
			h.server.respond = func(w http.ResponseWriter, n int) {
				if n == 1 {
					if tc.header != "" {
						w.Header().Set("Retry-After", tc.header)
					}
					w.WriteHeader(tc.status)
					return
				}
				_, _ = w.Write([]byte(`{"intentId":"i","status":"executed","transactionId":"tx"}`))
			}
			h.ticks(t, 6)
			if !strings.Contains(h.log.String(), tc.want) {
				t.Fatalf("log:\n%s", h.log.String())
			}
			if len(h.server.intents) != tc.intents {
				t.Fatalf("posted %d intents, want %d", len(h.server.intents), tc.intents)
			}
			if fills := int64(tc.intents - 1); h.bot.budget.Spent() != fills*1_000_000 {
				t.Fatalf("spent %d, want only the %d fills counted", h.bot.budget.Spent(), fills)
			}
		})
	}
}

func TestBot_unknownOutcomeCountsAgainstTheCapAndIsNotRetried(t *testing.T) {
	prices := &scriptedPrices{ticks: []map[string]float64{flat(100, 200), flat(100, 200), flat(101, 200), flat(102, 200)}}
	h := newHarness(t, true, prices, NewBudget(1_000_000, 5_000_000))
	h.server.respond = func(w http.ResponseWriter, _ int) { w.WriteHeader(http.StatusBadGateway) }
	h.ticks(t, 4)
	if len(h.server.intents) != 1 {
		t.Fatalf("posted %d intents, want no resend", len(h.server.intents))
	}
	if h.bot.budget.Spent() != 1_000_000 {
		t.Fatalf("spent %d, want the unconfirmed buy counted", h.bot.budget.Spent())
	}
	if h.bot.positions["GOOGLx"] != 0 {
		t.Fatal("an unconfirmed buy must not become a sellable position")
	}
}

func TestBot_priceFeedFailureBacksOffAndRecovers(t *testing.T) {
	prices := &scriptedPrices{
		ticks: []map[string]float64{nil, nil, flat(100, 200), {"mintG": 100}},
		errs:  map[int]error{0: errors.New("connection refused"), 1: errors.New("connection refused")},
	}
	h := newHarness(t, true, prices, NewBudget(1_000_000, 5_000_000))
	h.bot.cfg.Once = true
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	sleep := h.bot.sleep
	h.bot.sleep = func(c context.Context, d time.Duration) error {
		if calls++; calls == 4 {
			cancel()
			return c.Err()
		}
		return sleep(c, d)
	}
	if err := h.bot.Run(ctx); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second, time.Minute}
	if len(h.slept) != len(want) {
		t.Fatalf("slept %v, want %v", h.slept, want)
	}
	for i := range want {
		if h.slept[i] != want[i] {
			t.Fatalf("slept %v, want %v", h.slept, want)
		}
	}
	if !strings.Contains(h.log.String(), "AAPLx          —  no price from Jupiter, skipping") {
		t.Fatalf("log:\n%s", h.log.String())
	}
}

func TestBot_onceExitsAfterTheFirstRealDecision(t *testing.T) {
	prices := &scriptedPrices{ticks: []map[string]float64{flat(100, 200), flat(100, 200), flat(100.1, 200), flat(105, 200)}}
	h := newHarness(t, true, prices, NewBudget(1_000_000, 5_000_000))
	h.bot.cfg.Once = true
	if err := h.bot.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prices.n != 3 || len(h.server.intents) != 0 {
		t.Fatalf("ticks=%d intents=%d, want to stop at the third tick with a hold", prices.n, len(h.server.intents))
	}
	if !strings.Contains(h.log.String(), "+0.10% over 2m  hold") {
		t.Fatalf("log:\n%s", h.log.String())
	}
}
