package main

import (
	"math"
	"time"
)

const (
	usdcMicrosPerUSD   = 1_000_000
	tokenAtomsPerShare = 100_000_000 // xStocks are 8-decimal tokens
	// The bot never sees its fills, so it sizes sells from an estimate of what each
	// buy returned, shaved by this much to stay under the real holding after slippage.
	fillHaircut = 0.02
)

// Sample is one observed price.
type Sample struct {
	At    time.Time
	Price float64
}

// Window keeps just enough price history to compare now against one lookback ago.
type Window struct {
	span      time.Duration
	tolerance time.Duration
	samples   []Sample
}

// NewWindow returns a lookback window. tolerance absorbs ticker jitter: a sample
// that is span-tolerance old already counts as "one lookback ago".
func NewWindow(span, tolerance time.Duration) *Window {
	return &Window{span: span, tolerance: tolerance}
}

// Add records a price and drops history older than the reference sample.
func (w *Window) Add(at time.Time, price float64) {
	w.samples = append(w.samples, Sample{At: at, Price: price})
	for len(w.samples) > 1 && at.Sub(w.samples[1].At) >= w.span-w.tolerance {
		w.samples = w.samples[1:]
	}
}

// Momentum is the percent change from the reference sample to the latest one.
// ready is false until the window holds a full lookback; covered says how far along it is.
func (w *Window) Momentum() (pct float64, covered time.Duration, ready bool) {
	if len(w.samples) == 0 {
		return 0, 0, false
	}
	first, last := w.samples[0], w.samples[len(w.samples)-1]
	covered = last.At.Sub(first.At)
	if covered < w.span-w.tolerance || first.Price <= 0 {
		return 0, covered, false
	}
	return (last.Price - first.Price) / first.Price * 100, covered, true
}

// Action is what the rule says to do with one symbol right now.
type Action int

const (
	Hold Action = iota
	Buy
	Sell
)

// Rule is the whole strategy: buy what rose at least BuyPct over the lookback, sell
// what the bot bought once it has fallen at least SellPct over the lookback.
type Rule struct {
	BuyPct  float64
	SellPct float64
}

// Decide applies the rule. holding is whether the bot has an open position in the symbol.
func (r Rule) Decide(momentumPct float64, holding bool) Action {
	switch {
	case momentumPct >= r.BuyPct:
		return Buy
	case holding && momentumPct <= -r.SellPct:
		return Sell
	default:
		return Hold
	}
}

// Budget is the bot's own spend limit. The server enforces the cabal's voted
// allocation regardless; this keeps a misconfigured bot well inside it.
type Budget struct {
	perTradeMicros int64
	maxMicros      int64
	spentMicros    int64
}

// NewBudget returns a budget with a per-trade cap and a total cap for this run.
func NewBudget(perTradeMicros, maxMicros int64) *Budget {
	return &Budget{perTradeMicros: perTradeMicros, maxMicros: maxMicros}
}

// NextBuy is the size of the next buy, or 0 when a full-size trade no longer fits.
func (b *Budget) NextBuy() int64 {
	if b.perTradeMicros <= 0 || b.maxMicros-b.spentMicros < b.perTradeMicros {
		return 0
	}
	return b.perTradeMicros
}

// Record counts a buy against the total cap.
func (b *Budget) Record(micros int64) { b.spentMicros += micros }

// Exhaust stops all further buys, used when the server says the allocation is gone.
func (b *Budget) Exhaust() { b.spentMicros = b.maxMicros }

func (b *Budget) Spent() int64 { return b.spentMicros }
func (b *Budget) Max() int64   { return b.maxMicros }

// estimateFill is the conservative token amount a buy of usdcMicros at price returned.
func estimateFill(usdcMicros int64, price float64) int64 {
	if price <= 0 {
		return 0
	}
	shares := float64(usdcMicros) / usdcMicrosPerUSD / price
	return int64(math.Floor(shares * (1 - fillHaircut) * tokenAtomsPerShare))
}

func usdToMicros(usd float64) int64 { return int64(math.Round(usd * usdcMicrosPerUSD)) }
