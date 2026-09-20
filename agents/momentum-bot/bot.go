package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const (
	maxBackoff = time.Minute
	// How long to stand down after a 403 before asking again. Resuming takes a cabal
	// vote, so there is no point asking faster.
	pausedWait = time.Minute
	// An intent with no clear answer (timeout, dropped connection, 5xx, still executing) is
	// resent under its idempotency key, which the server answers with the first outcome.
	maxResends = 2
	resendWait = 5 * time.Second
)

// Config is everything the run loop needs. The agent key is deliberately not here.
type Config struct {
	Interval time.Duration
	Lookback time.Duration
	Rule     Rule
	Live     bool
	Once     bool
}

type intentSubmitter interface {
	SubmitIntent(ctx context.Context, intent Intent) (IntentResult, error)
}

type priceSource interface {
	Prices(ctx context.Context, mints []string) (map[string]float64, error)
}

// Bot watches a fixed set of assets and places at most one trade per tick.
type Bot struct {
	cfg    Config
	assets []Asset
	monaco intentSubmitter
	prices priceSource
	budget *Budget
	out    *printer
	now    func() time.Time
	sleep  func(ctx context.Context, d time.Duration) error

	windows   map[string]*Window
	positions map[string]int64     // estimated token atoms bought this run, by symbol
	cooldown  map[string]time.Time // no new trade in a symbol until this time
	blocked   time.Time            // no trades at all until this time (paused / throttled)
	failures  int                  // consecutive price fetch failures
}

// NewBot wires a bot over already-discovered assets.
func NewBot(cfg Config, assets []Asset, monaco intentSubmitter, prices priceSource, budget *Budget, out io.Writer, color bool) *Bot {
	b := &Bot{
		cfg:       cfg,
		assets:    assets,
		monaco:    monaco,
		prices:    prices,
		budget:    budget,
		out:       &printer{w: out, color: color},
		now:       time.Now,
		sleep:     sleepCtx,
		windows:   make(map[string]*Window, len(assets)),
		positions: make(map[string]int64),
		cooldown:  make(map[string]time.Time),
	}
	for _, asset := range assets {
		b.windows[asset.Symbol] = NewWindow(cfg.Lookback, cfg.Interval/2)
	}
	return b
}

// Run ticks until the context ends, the key is rejected, or (with Once) the first
// tick that had a full lookback to decide on.
func (b *Bot) Run(ctx context.Context) error {
	for {
		decided, err := b.Tick(ctx)
		if err != nil {
			return err
		}
		if decided && b.cfg.Once {
			return nil
		}
		wait := b.cfg.Interval
		if b.failures > 0 {
			wait = backoff(b.failures)
		}
		if err := b.sleep(ctx, wait); err != nil {
			return nil
		}
	}
}

type reading struct {
	asset    Asset
	price    float64
	momentum float64
	action   Action
}

// Tick samples prices, logs one line per symbol, and acts on the best signal.
// decided is true when at least one symbol had a full lookback behind it.
func (b *Bot) Tick(ctx context.Context) (decided bool, err error) {
	mints := make([]string, len(b.assets))
	for i, asset := range b.assets {
		mints[i] = asset.SolanaMint
	}
	prices, err := b.prices.Prices(ctx, mints)
	if err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		b.failures++
		b.out.warn(b.now(), "price feed unavailable (%v), retrying in %s", err, backoff(b.failures))
		return false, nil
	}
	b.failures = 0

	now := b.now()
	var signals []reading
	for _, asset := range b.assets {
		price, ok := prices[asset.SolanaMint]
		if !ok {
			b.out.symbol(now, asset.Symbol, 0, "no price from Jupiter, skipping")
			continue
		}
		window := b.windows[asset.Symbol]
		window.Add(now, price)
		momentum, covered, ready := window.Momentum()
		if !ready {
			b.out.symbol(now, asset.Symbol, price, fmt.Sprintf("warming up (%s of %s)", short(covered), short(b.cfg.Lookback)))
			continue
		}
		decided = true
		action := b.cfg.Rule.Decide(momentum, b.positions[asset.Symbol] > 0)
		note := map[Action]string{Hold: "hold", Buy: "buy signal", Sell: "sell signal"}[action]
		if action != Hold && now.Before(b.cooldown[asset.Symbol]) {
			action, note = Hold, note+", cooling down after last trade"
		}
		b.out.symbol(now, asset.Symbol, price, fmt.Sprintf("%+.2f%% over %s  %s", momentum, short(b.cfg.Lookback), note))
		if action != Hold {
			signals = append(signals, reading{asset: asset, price: price, momentum: momentum, action: action})
		}
	}

	pick, ok := b.choose(signals)
	if !ok {
		return decided, nil
	}
	if now.Before(b.blocked) {
		b.out.warn(now, "trading stood down for another %s", short(b.blocked.Sub(now)))
		return decided, nil
	}
	return decided, b.trade(ctx, pick)
}

// choose picks one signal per tick: sells before buys (get out first), then the
// largest move. Buys drop out once the budget cannot fit another full trade.
func (b *Bot) choose(signals []reading) (reading, bool) {
	var candidates []reading
	for _, s := range signals {
		if s.action == Buy && b.budget.NextBuy() == 0 {
			continue
		}
		candidates = append(candidates, s)
	}
	if len(candidates) == 0 {
		if len(signals) > 0 {
			b.out.warn(b.now(), "spend cap reached (%s of %s), not buying", usd(b.budget.Spent()), usd(b.budget.Max()))
		}
		return reading{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].action != candidates[j].action {
			return candidates[i].action == Sell
		}
		return abs(candidates[i].momentum) > abs(candidates[j].momentum)
	})
	return candidates[0], true
}

func (b *Bot) trade(ctx context.Context, s reading) error {
	symbol := s.asset.Symbol
	intent := Intent{Symbol: symbol}
	var what string
	if s.action == Buy {
		intent.Side, intent.UsdcMicros = "buy", b.budget.NextBuy()
		what = fmt.Sprintf("buy %s of %s", usd(intent.UsdcMicros), symbol)
	} else {
		intent.Side, intent.TokenAmount = "sell", b.positions[symbol]
		what = fmt.Sprintf("sell %s %s", shares(intent.TokenAmount), symbol)
	}
	// One trade per symbol per lookback, whatever happens next: the same signal
	// would otherwise fire again on every tick.
	b.cooldown[symbol] = b.now().Add(b.cfg.Lookback)

	if !b.cfg.Live {
		b.out.action(b.now(), "→ would %s  (dry run, nothing sent)", what)
		b.settle(s, intent)
		return nil
	}

	key, err := NewIdempotencyKey()
	if err != nil {
		return err
	}
	intent.IdempotencyKey = key

	b.out.action(b.now(), "→ %s", what)
	result, err := b.submit(ctx, intent)
	now := b.now()
	var rejected *RejectedError
	var throttled *ThrottledError
	switch {
	case err == nil:
		b.settle(s, intent)
		b.out.ok(now, "✓ filled  tx %s  intent %s  (%s of %s spent)", result.TransactionID, result.IntentID, usd(b.budget.Spent()), usd(b.budget.Max()))
	case errors.Is(err, ErrBadKey):
		return fmt.Errorf("%w: the key is wrong, revoked, or for another cabal; stopping so the server does not throttle this address", err)
	case errors.Is(err, ErrPaused):
		b.blocked = now.Add(pausedWait)
		b.out.warn(now, "✗ the cabal has paused this agent; still watching, will ask again in %s", short(pausedWait))
	case errors.As(err, &rejected):
		if rejected.IntentID != "" {
			b.out.warn(now, "✗ rejected by Monaco: %s  intent %s", rejected.Reason, rejected.IntentID)
		} else {
			b.out.warn(now, "✗ rejected by Monaco: %s", rejected.Reason)
		}
		if strings.Contains(rejected.Reason, "allocation") {
			// The voted budget is spent. Sells can still go through.
			b.budget.Exhaust()
		}
		if s.action == Sell {
			// The estimate was above what the cabal really holds; drop it rather than retry it.
			b.positions[symbol] = 0
		}
	case errors.As(err, &throttled):
		b.blocked = now.Add(throttled.RetryAfter)
		b.out.warn(now, "✗ throttled by Monaco, standing down for %s", short(throttled.RetryAfter))
	default:
		// Still no clear answer after the resends, or Monaco says the swap failed: the trade may
		// have gone through all the same. Assume it did, so the cap errs on the safe side.
		b.out.warn(now, "✗ no clear answer from Monaco (%v). Giving up on this one; check the cabal's activity feed.", err)
		if s.action == Buy {
			b.budget.Record(intent.UsdcMicros)
			b.out.warn(now, "  counting %s against the cap anyway (%s of %s)", usd(intent.UsdcMicros), usd(b.budget.Spent()), usd(b.budget.Max()))
		} else {
			b.positions[symbol] = 0
		}
	}
	return nil
}

// submit posts an intent and resends it, unchanged and under the same idempotency key,
// while the outcome is unclear. Any clear answer, good or bad, ends it.
func (b *Bot) submit(ctx context.Context, intent Intent) (IntentResult, error) {
	for resends := 0; ; resends++ {
		result, err := b.monaco.SubmitIntent(ctx, intent)
		if !unclearOutcome(err) || resends == maxResends {
			return result, err
		}
		b.out.warn(b.now(), "  no clear answer from Monaco (%v); asking again in %s under the same idempotency key", err, short(resendWait))
		if b.sleep(ctx, resendWait) != nil {
			return result, err
		}
	}
}

// unclearOutcome reports whether err leaves open whether the trade happened. Every answer
// the bot has a reaction for is clear; so is a 2xx that says the intent did not execute.
func unclearOutcome(err error) bool {
	if err == nil || errors.Is(err, ErrBadKey) || errors.Is(err, ErrPaused) {
		return false
	}
	var rejected *RejectedError
	var throttled *ThrottledError
	var unsettled *UnsettledError
	return !errors.As(err, &rejected) && !errors.As(err, &throttled) && !errors.As(err, &unsettled)
}

// settle updates the bot's books after a fill (or a simulated one in dry run).
func (b *Bot) settle(s reading, intent Intent) {
	if s.action == Buy {
		b.budget.Record(intent.UsdcMicros)
		b.positions[s.asset.Symbol] += estimateFill(intent.UsdcMicros, s.price)
		return
	}
	b.positions[s.asset.Symbol] = 0
}

func backoff(failures int) time.Duration {
	d := time.Second
	for i := 0; i < failures && d < maxBackoff; i++ {
		d *= 2
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
