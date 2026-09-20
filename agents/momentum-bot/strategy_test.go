package main

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 19, 14, 0, 0, 0, time.UTC)

func TestWindow_notReadyUntilFullLookback(t *testing.T) {
	w := NewWindow(5*time.Minute, 15*time.Second)
	if _, _, ready := w.Momentum(); ready {
		t.Fatal("empty window must not be ready")
	}
	w.Add(t0, 100)
	w.Add(t0.Add(2*time.Minute), 110)
	_, covered, ready := w.Momentum()
	if ready || covered != 2*time.Minute {
		t.Fatalf("ready=%v covered=%s, want not ready at 2m", ready, covered)
	}
}

func TestWindow_momentumAgainstOneLookbackAgo(t *testing.T) {
	w := NewWindow(5*time.Minute, 15*time.Second)
	for i, price := range []float64{100, 90, 95, 99, 100, 100.8} {
		w.Add(t0.Add(time.Duration(i)*time.Minute), price)
	}
	pct, _, ready := w.Momentum()
	if !ready || pct < 0.79 || pct > 0.81 {
		t.Fatalf("pct=%.4f ready=%v, want +0.80%% vs the 14:00 price", pct, ready)
	}

	// One more minute: the reference slides to the 14:01 price of 90.
	w.Add(t0.Add(6*time.Minute), 99)
	pct, covered, _ := w.Momentum()
	if pct != 10 || covered != 5*time.Minute {
		t.Fatalf("pct=%.4f covered=%s, want +10%% over 5m", pct, covered)
	}
}

func TestWindow_toleratesTickerJitter(t *testing.T) {
	w := NewWindow(time.Minute, 5*time.Second)
	w.Add(t0, 100)
	w.Add(t0.Add(57*time.Second), 101)
	if _, _, ready := w.Momentum(); !ready {
		t.Fatal("57s of a 60s lookback with 5s tolerance should be ready")
	}
}

func TestWindow_zeroReferencePriceIsNotReady(t *testing.T) {
	w := NewWindow(time.Minute, 0)
	w.Add(t0, 0)
	w.Add(t0.Add(time.Minute), 10)
	if _, _, ready := w.Momentum(); ready {
		t.Fatal("a zero reference price must not produce a signal")
	}
}

func TestRule_decide(t *testing.T) {
	rule := Rule{BuyPct: 0.5, SellPct: 0.5}
	cases := []struct {
		name     string
		momentum float64
		holding  bool
		want     Action
	}{
		{"up past threshold buys", 0.8, false, Buy},
		{"exactly at threshold buys", 0.5, false, Buy},
		{"small move holds", 0.3, true, Hold},
		{"down past threshold sells a held position", -0.9, true, Sell},
		{"down past threshold with nothing held holds", -0.9, false, Hold},
		{"still adds to a winner", 1.2, true, Buy},
	}
	for _, tc := range cases {
		if got := rule.Decide(tc.momentum, tc.holding); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestBudget_capsPerTradeAndTotal(t *testing.T) {
	b := NewBudget(usdToMicros(2), usdToMicros(5))
	for i := 0; i < 2; i++ {
		size := b.NextBuy()
		if size != 2_000_000 {
			t.Fatalf("buy %d: size %d, want per-trade cap", i, size)
		}
		b.Record(size)
	}
	// $1 left: a full $2 trade no longer fits, so the bot stops rather than overshoot.
	if size := b.NextBuy(); size != 0 {
		t.Fatalf("size %d, want 0 with $1 of headroom", size)
	}
	if b.Spent() != 4_000_000 {
		t.Fatalf("spent %d", b.Spent())
	}
}

func TestBudget_exhaustStopsBuys(t *testing.T) {
	b := NewBudget(usdToMicros(1), usdToMicros(5))
	b.Exhaust()
	if b.NextBuy() != 0 {
		t.Fatal("exhausted budget must not size a buy")
	}
}

func TestEstimateFill_isBelowTheExactFill(t *testing.T) {
	// $1 at $200/share is 0.005 shares = 500000 atoms; the estimate keeps 98%.
	if got := estimateFill(1_000_000, 200); got != 490_000 {
		t.Fatalf("got %d want 490000", got)
	}
	if got := estimateFill(1_000_000, 0); got != 0 {
		t.Fatalf("zero price: got %d", got)
	}
}
