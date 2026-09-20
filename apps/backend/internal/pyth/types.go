package pyth

import "time"

// TreasuryRef identifies a group treasury for pot valuation.
type TreasuryRef struct {
	GroupID      string
	Address      string
	TreasuryUsdc int64
}

// CostBasis is fill-derived holding metadata from confirmed buy transactions.
type CostBasis struct {
	Symbol string
	Mint   string
	Units  int64
	Price  int64
	Amount int64
}

// MarkSource names the price source a mark came from, so a valuation is auditable.
type MarkSource string

const (
	MarkSourcePyth      MarkSource = "pyth"
	MarkSourceJupiter   MarkSource = "jupiter"
	MarkSourceCostBasis MarkSource = "cost_basis"
)

// EquityMark is one Hermes equity mark plus the freshness metadata around it.
type EquityMark struct {
	PriceUsdcMicros int64
	// ConfUsdcMicros is Pyth's confidence interval around the mark, in USDC micros.
	// Zero means Hermes did not publish one. Display only — valuation does not
	// widen or narrow a mark by it.
	ConfUsdcMicros int64
	// PublishedAt is Hermes publish_time in UTC; zero when Hermes omits it.
	PublishedAt time.Time
	MarketOpen  bool
	AfterHours  bool
}

// MarkedHolding is a treasury xStock position with a live mark. Source records
// which price source produced MarkUsdc; empty means the producer did not say.
type MarkedHolding struct {
	Symbol     string
	Mint       string
	Units      int64
	MarkUsdc   int64
	CostBasis  int64
	AfterHours bool
	Source     MarkSource
}

// NavInput is the marked-pot valuation input for domain NAV callers (M4-T5).
type NavInput struct {
	TreasuryUsdc int64
	Holdings     []MarkedHolding
	AfterHours   bool
}

// PotAfterHours reports whether any holding uses a frozen equity mark.
func PotAfterHours(holdings []MarkedHolding) bool {
	for _, holding := range holdings {
		if holding.AfterHours {
			return true
		}
	}
	return false
}
