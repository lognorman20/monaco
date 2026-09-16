package pyth

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

// MarkedHolding is a treasury xStock position with a live Pyth mark.
type MarkedHolding struct {
	Symbol     string
	Mint       string
	Units      int64
	MarkUsdc   int64
	CostBasis  int64
	AfterHours bool
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
