package domain

import (
	"fmt"
	"math/big"
	"math/rand"
	"testing"
)

// Rounding rules these properties are derived from (money.go ratRoundToInt64):
// every amount is rounded to the NEAREST micro, halves up. Nothing floors, so a
// single operation can land up to half a micro on either side of the exact value.
//
//   - Redeem: |UsdcOwed − exact slice| ≤ ½ micro, and UsdcOwed ≤ pot NAV.
//   - Deposit: share price P is itself round(NAV/shares), then minted share micros
//     are round(D×1e6/P). Value moved between the depositor and existing members
//     is at most D/(2P) + p/(2×1e6) micros, where p is the exact share price.
//
// depositDustBound and redeemDustBound state those bounds; the simulation asserts
// them after every operation and asserts their running sum at the end.

// redeemDustBound is the most value (in micros) one redeem can shift between the
// redeemer and everyone else.
var redeemDustBound = big.NewRat(1, 2)

// depositDustBound returns D/(2P) + p/(2×1e6): the most value one deposit can
// shift between the depositor and existing members.
func depositDustBound(deposited USDCMicros, nav PotNAV, totalShareMicros int64) *big.Rat {
	bound := big.NewRat(int64(deposited), 2*int64(nav.PerShareUsdc))
	if totalShareMicros > 0 {
		// p/(2×1e6) with p = NAV / (shareMicros/1e6) simplifies to NAV/(2×shareMicros).
		bound.Add(bound, big.NewRat(int64(nav.TotalUsdc), 2*totalShareMicros))
	}
	return bound
}

func ratCeil(r *big.Rat) int64 {
	q, m := new(big.Int).DivMod(r.Num(), r.Denom(), new(big.Int))
	if m.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q.Int64()
}

// simPot is a reference ledger driven only through the public domain API.
type simPot struct {
	t            *testing.T
	treasury     USDCMicros
	holdingUnits int64 // micro-units of a single xStock line
	mark         USDCMicros
	shareMicros  map[string]int64
	totalShares  int64
	deposited    map[string]USDCMicros
	paid         map[string]USDCMicros
	marketPnL    int64    // NAV change caused by marks and trades, not by members
	dust         *big.Rat // running sum of per-operation dust bounds
	members      []string
}

func newSimPot(t *testing.T, members []string) *simPot {
	return &simPot{
		t:           t,
		mark:        100_000_000,
		shareMicros: map[string]int64{},
		deposited:   map[string]USDCMicros{},
		paid:        map[string]USDCMicros{},
		dust:        new(big.Rat),
		members:     members,
	}
}

func (p *simPot) shares(micros int64) ShareUnits {
	p.t.Helper()
	units, err := ShareUnitsMicrosToDomain(micros)
	if err != nil {
		p.t.Fatalf("ShareUnitsMicrosToDomain(%d): %v", micros, err)
	}
	return units
}

func (p *simPot) nav() PotNAV {
	p.t.Helper()
	nav, err := ComputePotNAV(NavInput{
		Mode:         NavMarked,
		TreasuryUsdc: p.treasury,
		TotalShares:  p.shares(p.totalShares),
		Holdings: []MarkedHolding{{
			Symbol:   "AAPLx",
			Units:    string(p.shares(p.holdingUnits)),
			MarkUsdc: p.mark,
		}},
	})
	if err != nil {
		p.t.Fatalf("ComputePotNAV: %v", err)
	}
	return nav
}

func (p *simPot) equity(member string, nav PotNAV) USDCMicros {
	p.t.Helper()
	equity, err := MemberEquity(p.shares(p.shareMicros[member]), p.shares(p.totalShares), nav.TotalUsdc)
	if err != nil {
		p.t.Fatalf("MemberEquity(%s): %v", member, err)
	}
	return equity
}

func (p *simPot) equities(nav PotNAV) map[string]USDCMicros {
	out := make(map[string]USDCMicros, len(p.members))
	for _, member := range p.members {
		out[member] = p.equity(member, nav)
	}
	return out
}

// assertOthersNotDiluted fails when any member other than actor lost more equity
// than bound (plus 1 micro for the two MemberEquity roundings being compared).
func (p *simPot) assertOthersNotDiluted(op, actor string, before map[string]USDCMicros, bound *big.Rat) {
	p.t.Helper()
	after := p.equities(p.nav())
	allowed := USDCMicros(ratCeil(bound) + 1)
	for _, member := range p.members {
		if member == actor {
			continue
		}
		if loss := before[member] - after[member]; loss > allowed {
			p.t.Fatalf("%s by %s diluted %s by %d micros (allowed %d): before=%d after=%d",
				op, actor, member, loss, allowed, before[member], after[member])
		}
	}
}

// assertConserved checks deposits + market P&L == payouts + pot NAV, and that
// member equities add back up to the pot within half a micro each.
func (p *simPot) assertConserved(op string) {
	p.t.Helper()
	nav := p.nav()
	if nav.TotalUsdc < 0 || p.treasury < 0 {
		p.t.Fatalf("after %s: negative pot: nav=%d treasury=%d", op, nav.TotalUsdc, p.treasury)
	}
	var in, out int64
	for _, member := range p.members {
		in += int64(p.deposited[member])
		out += int64(p.paid[member])
	}
	if in+p.marketPnL != out+int64(nav.TotalUsdc) {
		p.t.Fatalf("after %s: value in %d + market %d != paid %d + pot %d",
			op, in, p.marketPnL, out, nav.TotalUsdc)
	}
	if p.totalShares == 0 {
		return
	}
	var equitySum int64
	for _, equity := range p.equities(nav) {
		equitySum += int64(equity)
	}
	slack := int64(len(p.members)+1) / 2
	if diff := equitySum - int64(nav.TotalUsdc); diff > slack || diff < -slack {
		p.t.Fatalf("after %s: member equities sum to %d, pot is %d (slack %d)", op, equitySum, nav.TotalUsdc, slack)
	}
}

func (p *simPot) deposit(member string, amount USDCMicros) {
	p.t.Helper()
	nav := p.nav()
	before := p.equities(nav)
	minted, err := ShareUnitsMicrosForDeposit(amount, nav)
	if err != nil {
		if nav.PerShareUsdc == 0 {
			return // share price rounded to zero: deposits are refused, not mispriced
		}
		p.t.Fatalf("ShareUnitsMicrosForDeposit(%d, %+v): %v", amount, nav, err)
	}
	bound := depositDustBound(amount, nav, p.totalShares)
	p.dust.Add(p.dust, bound)

	p.treasury += amount
	p.deposited[member] += amount
	p.shareMicros[member] += minted
	p.totalShares += minted

	p.assertOthersNotDiluted("deposit", member, before, bound)
	// The depositor's own equity grows by the deposit, within the same bound.
	gained := p.equity(member, p.nav()) - before[member]
	allowed := USDCMicros(ratCeil(bound) + 1)
	if diff := gained - amount; diff > allowed || diff < -allowed {
		p.t.Fatalf("deposit of %d by %s changed own equity by %d (allowed ±%d)", amount, member, gained, allowed)
	}
	p.assertConserved("deposit")
}

// redeem burns shareMicros of member's claim and pays the slice out of the pot.
// It reports false when the slice is dust the domain refuses to pay.
func (p *simPot) redeem(member string, shareMicros int64) bool {
	p.t.Helper()
	nav := p.nav()
	before := p.equities(nav)
	slice, err := ComputeRedeemSlice(RedeemSliceInput{
		SharesRedeemedMicros: shareMicros,
		TotalSharesMicros:    p.totalShares,
		PotNav:               nav.TotalUsdc,
	})
	exact := new(big.Rat).Mul(big.NewRat(shareMicros, p.totalShares), big.NewRat(int64(nav.TotalUsdc), 1))
	if err != nil {
		if exact.Cmp(redeemDustBound) >= 0 {
			p.t.Fatalf("ComputeRedeemSlice refused a payable slice of %s micros: %v", exact.FloatString(3), err)
		}
		return false
	}
	if slice.UsdcOwed > nav.TotalUsdc {
		p.t.Fatalf("redeem pays %d from a pot worth %d", slice.UsdcOwed, nav.TotalUsdc)
	}
	drift := new(big.Rat).Sub(big.NewRat(int64(slice.UsdcOwed), 1), exact)
	if drift.Abs(drift).Cmp(redeemDustBound) > 0 {
		p.t.Fatalf("redeem pays %d, exact slice %s: off by more than half a micro", slice.UsdcOwed, exact.FloatString(6))
	}
	p.dust.Add(p.dust, redeemDustBound)

	if slice.UsdcOwed > p.treasury {
		p.liquidate()
	}
	p.treasury -= slice.UsdcOwed
	p.paid[member] += slice.UsdcOwed
	p.shareMicros[member] -= shareMicros
	p.totalShares -= shareMicros

	if p.totalShares > 0 {
		p.assertOthersNotDiluted("redeem", member, before, redeemDustBound)
	}
	p.assertConserved("redeem")
	return true
}

// liquidate sells the whole xStock line at mark. NAV already values the line at
// the same rounded amount, so this moves value without changing it.
func (p *simPot) liquidate() {
	p.t.Helper()
	value, err := multiplyDecimalByMicros(string(p.shares(p.holdingUnits)), p.mark)
	if err != nil {
		p.t.Fatalf("multiplyDecimalByMicros: %v", err)
	}
	p.treasury += value
	p.holdingUnits = 0
}

// external applies a market-side change and books the NAV delta as market P&L.
func (p *simPot) external(op string, change func()) {
	p.t.Helper()
	before := p.nav().TotalUsdc
	change()
	p.marketPnL += int64(p.nav().TotalUsdc - before)
	p.assertConserved(op)
}

func (p *simPot) buy(spend USDCMicros) {
	p.t.Helper()
	units, err := MulDivFloor(int64(spend), 1_000_000, int64(p.mark))
	if err != nil {
		p.t.Fatalf("MulDivFloor: %v", err)
	}
	p.external("buy", func() {
		p.treasury -= spend
		p.holdingUnits += units
	})
}

func (p *simPot) redeemEverything() {
	p.t.Helper()
	for i, member := range p.members {
		held := p.shareMicros[member]
		if held == 0 {
			continue
		}
		last := p.totalShares == held
		potBefore := p.nav().TotalUsdc
		paidBefore := p.paid[member]
		if !p.redeem(member, held) {
			if last && potBefore > 0 {
				p.t.Fatalf("last holder %s could not redeem a pot worth %d", member, potBefore)
			}
			continue
		}
		if last {
			if got := p.paid[member] - paidBefore; got != potBefore {
				p.t.Fatalf("member %d redeemed all outstanding shares: paid %d, pot was %d", i, got, potBefore)
			}
		}
	}
}

func randomDeposit(rng *rand.Rand) USDCMicros {
	// Log-uniform from $0.01 to $1M so tiny and whale deposits both show up.
	magnitude := rng.Intn(9) // 1e4 .. 1e12 micros
	base := int64(10_000)
	for i := 0; i < magnitude; i++ {
		base *= 10
	}
	return USDCMicros(base + rng.Int63n(base*9))
}

func TestPotSimulation_randomDepositsMarksRedeems_conservesValueAndNeverDilutes(t *testing.T) {
	members := []string{"alex", "blair", "casey", "devon", "emery"}
	for seed := int64(1); seed <= 40; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			// Arrange
			rng := newSeededRand(t, seed)
			pot := newSimPot(t, members)

			// Act + Assert — every operation asserts conservation and no-dilution.
			for step := 0; step < 300; step++ {
				member := members[rng.Intn(len(members))]
				switch roll := rng.Intn(10); {
				case roll < 4:
					pot.deposit(member, randomDeposit(rng))
				case roll < 6:
					if held := pot.shareMicros[member]; held > 0 {
						pot.redeem(member, 1+rng.Int63n(held))
					}
				case roll < 8:
					// Mark moves between −50% and +100%, never below one micro.
					pot.external("mark", func() {
						next := int64(pot.mark) * int64(50+rng.Intn(151)) / 100
						if next < 1 {
							next = 1
						}
						pot.mark = USDCMicros(next)
					})
				default:
					if pot.treasury > 1 {
						pot.buy(USDCMicros(1 + rng.Int63n(int64(pot.treasury))))
					}
				}
			}
			pot.redeemEverything()

			// Assert — nothing is left behind and nothing extra was created.
			if pot.totalShares == 0 {
				if left := pot.nav().TotalUsdc; left != 0 {
					t.Fatalf("all shares redeemed but pot still holds %d micros", left)
				}
			}
		})
	}
}

func TestPotSimulation_flatUsdcPot_noMemberProfitsBeyondDustBound(t *testing.T) {
	members := []string{"alex", "blair", "casey"}
	for seed := int64(100); seed < 140; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			// Arrange — no marks, no trades: every micro in the pot is a member deposit.
			rng := newSeededRand(t, seed)
			pot := newSimPot(t, members)

			// Act
			for step := 0; step < 200; step++ {
				member := members[rng.Intn(len(members))]
				if rng.Intn(3) > 0 {
					pot.deposit(member, randomDeposit(rng))
				} else if held := pot.shareMicros[member]; held > 0 {
					pot.redeem(member, 1+rng.Int63n(held))
				}
			}
			pot.redeemEverything()

			// Assert — payouts never exceed deposits in aggregate, the pot is empty,
			// and no single member gained or lost more than the accumulated dust bound.
			if pot.marketPnL != 0 {
				t.Fatalf("flat pot recorded market P&L of %d", pot.marketPnL)
			}
			var in, out USDCMicros
			for _, member := range members {
				in += pot.deposited[member]
				out += pot.paid[member]
			}
			remaining := pot.nav().TotalUsdc
			if out+remaining != in {
				t.Fatalf("paid %d + remaining %d != deposited %d", out, remaining, in)
			}
			if pot.totalShares == 0 && remaining != 0 {
				t.Fatalf("all shares redeemed but %d micros remain", remaining)
			}
			allowed := USDCMicros(ratCeil(pot.dust))
			for _, member := range members {
				stillOwed := pot.equity(member, pot.nav())
				net := pot.paid[member] + stillOwed - pot.deposited[member]
				if net > allowed || net < -allowed {
					t.Fatalf("%s net %d micros on a flat pot; dust bound is %d", member, net, allowed)
				}
			}
		})
	}
}

func TestSharesForDeposit_firstDeposit_mintsAtOneDollarPerShare(t *testing.T) {
	rng := newSeededRand(t, 7)
	for i := 0; i < 500; i++ {
		// Arrange — an empty pot: no shares, any treasury balance.
		deposited := randomDeposit(rng)
		nav, err := ComputePotNAV(NavInput{Mode: NavUSDCOnly, TreasuryUsdc: USDCMicros(rng.Int63n(1_000_000_000)), TotalShares: "0"})
		if err != nil {
			t.Fatalf("ComputePotNAV: %v", err)
		}

		// Act
		micros, err := ShareUnitsMicrosForDeposit(deposited, nav)

		// Assert
		if err != nil {
			t.Fatalf("ShareUnitsMicrosForDeposit: %v", err)
		}
		if nav.PerShareUsdc != BootstrapSharePriceMicros {
			t.Fatalf("bootstrap per-share = %d, want %d", nav.PerShareUsdc, BootstrapSharePriceMicros)
		}
		if micros != int64(deposited) {
			t.Fatalf("first deposit of %d micros minted %d share micros, want 1:1", deposited, micros)
		}
	}
}

func TestShareUnitsMicrosForDeposit_largerDeposit_neverMintsFewerShares(t *testing.T) {
	rng := newSeededRand(t, 11)
	for i := 0; i < 2_000; i++ {
		// Arrange
		nav := PotNAV{PerShareUsdc: USDCMicros(1 + rng.Int63n(5_000_000_000))}
		small := USDCMicros(1 + rng.Int63n(1_000_000_000_000))
		large := small + USDCMicros(rng.Int63n(1_000_000_000_000))

		// Act
		smallMicros, errSmall := ShareUnitsMicrosForDeposit(small, nav)
		largeMicros, errLarge := ShareUnitsMicrosForDeposit(large, nav)

		// Assert
		if errSmall != nil || errLarge != nil {
			t.Fatalf("ShareUnitsMicrosForDeposit: %v / %v", errSmall, errLarge)
		}
		if largeMicros < smallMicros {
			t.Fatalf("deposit %d minted %d but smaller deposit %d minted %d at %d/share",
				large, largeMicros, small, smallMicros, nav.PerShareUsdc)
		}
	}
}

func TestShareUnitsMicrosForDeposit_higherSharePrice_neverMintsMoreShares(t *testing.T) {
	rng := newSeededRand(t, 13)
	for i := 0; i < 2_000; i++ {
		// Arrange
		deposited := randomDeposit(rng)
		cheap := USDCMicros(1 + rng.Int63n(5_000_000_000))
		dear := cheap + USDCMicros(rng.Int63n(5_000_000_000))

		// Act
		cheapMicros, errCheap := ShareUnitsMicrosForDeposit(deposited, PotNAV{PerShareUsdc: cheap})
		dearMicros, errDear := ShareUnitsMicrosForDeposit(deposited, PotNAV{PerShareUsdc: dear})

		// Assert
		if errCheap != nil || errDear != nil {
			t.Fatalf("ShareUnitsMicrosForDeposit: %v / %v", errCheap, errDear)
		}
		if dearMicros > cheapMicros {
			t.Fatalf("%d micros minted %d at %d/share but only %d at cheaper %d/share",
				deposited, dearMicros, dear, cheapMicros, cheap)
		}
	}
}

func TestSharesForDeposit_nonPositiveInputs_rejected(t *testing.T) {
	cases := []struct {
		name      string
		deposited USDCMicros
		perShare  USDCMicros
	}{
		{name: "zero deposit", deposited: 0, perShare: BootstrapSharePriceMicros},
		{name: "negative deposit", deposited: -1, perShare: BootstrapSharePriceMicros},
		{name: "most negative deposit", deposited: -1 << 63, perShare: BootstrapSharePriceMicros},
		{name: "zero share price", deposited: 1_000_000, perShare: 0},
		{name: "negative share price", deposited: 1_000_000, perShare: -1_000_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			nav := PotNAV{PerShareUsdc: tc.perShare}

			// Act
			shares, err := SharesForDeposit(tc.deposited, nav)
			micros, errMicros := ShareUnitsMicrosForDeposit(tc.deposited, nav)

			// Assert
			if err == nil || errMicros == nil {
				t.Fatalf("expected rejection, got shares=%q micros=%d", shares, micros)
			}
		})
	}
}

func TestComputeRedeemSlice_nonPositiveOrOversizedInputs_rejected(t *testing.T) {
	cases := []struct {
		name string
		in   RedeemSliceInput
	}{
		{name: "zero shares redeemed", in: RedeemSliceInput{SharesRedeemedMicros: 0, TotalSharesMicros: 10, PotNav: 10}},
		{name: "negative shares redeemed", in: RedeemSliceInput{SharesRedeemedMicros: -1, TotalSharesMicros: 10, PotNav: 10}},
		{name: "zero total shares", in: RedeemSliceInput{SharesRedeemedMicros: 1, TotalSharesMicros: 0, PotNav: 10}},
		{name: "negative total shares", in: RedeemSliceInput{SharesRedeemedMicros: 1, TotalSharesMicros: -10, PotNav: 10}},
		{name: "more than outstanding", in: RedeemSliceInput{SharesRedeemedMicros: 11, TotalSharesMicros: 10, PotNav: 10}},
		{name: "negative pot", in: RedeemSliceInput{SharesRedeemedMicros: 1, TotalSharesMicros: 10, PotNav: -1}},
		{name: "empty pot", in: RedeemSliceInput{SharesRedeemedMicros: 10, TotalSharesMicros: 10, PotNav: 0}},
		{name: "slice rounds to zero", in: RedeemSliceInput{SharesRedeemedMicros: 1, TotalSharesMicros: 1_000, PotNav: 499}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			// Act
			slice, err := ComputeRedeemSlice(tc.in)

			// Assert
			if err == nil {
				t.Fatalf("expected rejection, got %+v", slice)
			}
		})
	}
}

func TestComputeRedeemSlice_allOutstandingShares_returnsEntirePot(t *testing.T) {
	rng := newSeededRand(t, 17)
	for i := 0; i < 2_000; i++ {
		// Arrange
		total := 1 + rng.Int63()
		pot := USDCMicros(1 + rng.Int63())

		// Act
		slice, err := ComputeRedeemSlice(RedeemSliceInput{SharesRedeemedMicros: total, TotalSharesMicros: total, PotNav: pot})

		// Assert
		if err != nil {
			t.Fatalf("ComputeRedeemSlice(%d of %d, pot %d): %v", total, total, pot, err)
		}
		if slice.UsdcOwed != pot {
			t.Fatalf("full redeem paid %d, want entire pot %d", slice.UsdcOwed, pot)
		}
	}
}

func TestComputeRedeemSlice_moreSharesRedeemed_neverPaysLess(t *testing.T) {
	rng := newSeededRand(t, 19)
	for i := 0; i < 2_000; i++ {
		// Arrange
		total := 2 + rng.Int63n(1_000_000_000_000)
		pot := USDCMicros(total + rng.Int63n(1_000_000_000_000)) // ≥ $1/share so no slice is dust
		small := 1 + rng.Int63n(total-1)
		large := small + rng.Int63n(total-small+1)

		// Act
		smallSlice, errSmall := ComputeRedeemSlice(RedeemSliceInput{SharesRedeemedMicros: small, TotalSharesMicros: total, PotNav: pot})
		largeSlice, errLarge := ComputeRedeemSlice(RedeemSliceInput{SharesRedeemedMicros: large, TotalSharesMicros: total, PotNav: pot})

		// Assert
		if errSmall != nil || errLarge != nil {
			t.Fatalf("ComputeRedeemSlice: %v / %v", errSmall, errLarge)
		}
		if largeSlice.UsdcOwed < smallSlice.UsdcOwed {
			t.Fatalf("redeeming %d paid %d but %d paid %d (total %d, pot %d)",
				large, largeSlice.UsdcOwed, small, smallSlice.UsdcOwed, total, pot)
		}
	}
}
