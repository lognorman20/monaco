package domain

import (
	"math"
	"math/big"
	"testing"
)

// Fuzz targets for the numeric entry points. Each asserts the same contract:
// the call never panics, and it either errors or returns a value that matches
// an independent arbitrary-precision computation. Run one with
//
//	go test -run='^$' -fuzz=FuzzComputeRedeemSlice -fuzztime=20s

// halfUp reports whether got is num/den rounded to nearest, halves up,
// i.e. 2·got·den − den ≤ 2·num < 2·got·den + den.
func halfUp(got int64, num, den *big.Int) bool {
	twiceNum := new(big.Int).Lsh(num, 1)
	centre := new(big.Int).Mul(big.NewInt(got), den)
	centre.Lsh(centre, 1)
	low := new(big.Int).Sub(centre, den)
	high := new(big.Int).Add(centre, den)
	return twiceNum.Cmp(low) >= 0 && twiceNum.Cmp(high) < 0
}

func FuzzShareUnitsMicrosForDeposit(f *testing.F) {
	f.Add(int64(4_000_000), int64(BootstrapSharePriceMicros)) // $4 at bootstrap
	f.Add(int64(110_000_000), int64(1_100_000))               // README Alex/Blair
	f.Add(int64(1), int64(3))                                 // rounds to a fraction of a share
	f.Add(int64(1), int64(2_000_001))                         // rounds to zero share micros
	f.Add(int64(math.MaxInt64), int64(1))                     // overflows int64
	f.Add(int64(math.MaxInt64), int64(BootstrapSharePriceMicros))
	f.Add(int64(math.MaxInt64), int64(math.MaxInt64))
	f.Add(int64(0), int64(1_000_000))
	f.Add(int64(-5), int64(1_000_000))
	f.Add(int64(1_000_000), int64(0))
	f.Add(int64(1_000_000), int64(math.MinInt64))

	f.Fuzz(func(t *testing.T, deposited, perShare int64) {
		nav := PotNAV{PerShareUsdc: USDCMicros(perShare)}
		micros, err := ShareUnitsMicrosForDeposit(USDCMicros(deposited), nav)
		shares, errShares := SharesForDeposit(USDCMicros(deposited), nav)

		if (err == nil) != (errShares == nil) {
			t.Fatalf("micros err = %v but decimal err = %v", err, errShares)
		}
		if deposited <= 0 || perShare <= 0 {
			if err == nil {
				t.Fatalf("accepted deposited=%d perShare=%d", deposited, perShare)
			}
			return
		}
		num := new(big.Int).Mul(big.NewInt(deposited), big.NewInt(1_000_000))
		den := big.NewInt(perShare)
		if err != nil {
			// The only legitimate failure for positive inputs is an int64 overflow.
			if q := new(big.Int).Quo(num, den); q.IsInt64() && q.Int64() < math.MaxInt64 {
				t.Fatalf("rejected representable mint %s: %v", q, err)
			}
			return
		}
		if micros < 0 {
			t.Fatalf("minted negative share micros %d", micros)
		}
		if !halfUp(micros, num, den) {
			t.Fatalf("minted %d for %d micros at %d/share", micros, deposited, perShare)
		}
		// The decimal form must round-trip to the same ledger integer.
		parsed, ok := new(big.Rat).SetString(string(shares))
		if !ok || parsed.Cmp(big.NewRat(micros, 1_000_000)) != 0 {
			t.Fatalf("SharesForDeposit = %q, micros = %d", shares, micros)
		}
	})
}

func FuzzComputeRedeemSlice(f *testing.F) {
	f.Add(int64(500_000), int64(2_000_000), int64(4_000_000))   // quarter of a $4 pot
	f.Add(int64(3_000_000), int64(3_000_000), int64(3_000_000)) // full redeem
	f.Add(int64(1), int64(3), int64(1))                         // ⅓ micro: dust, refused
	f.Add(int64(1), int64(2), int64(1))                         // exactly ½ micro rounds up to the whole pot
	f.Add(int64(1), int64(2), int64(3))                         // 1.5 rounds to 2
	f.Add(int64(1), int64(math.MaxInt64), int64(math.MaxInt64))
	f.Add(int64(math.MaxInt64), int64(math.MaxInt64), int64(math.MaxInt64))
	f.Add(int64(math.MaxInt64-1), int64(math.MaxInt64), int64(math.MaxInt64))
	f.Add(int64(5), int64(4), int64(100)) // more than outstanding
	f.Add(int64(0), int64(4), int64(100))
	f.Add(int64(1), int64(4), int64(-100))
	f.Add(int64(math.MinInt64), int64(math.MinInt64), int64(math.MinInt64))

	f.Fuzz(func(t *testing.T, redeemed, total, pot int64) {
		slice, err := ComputeRedeemSlice(RedeemSliceInput{
			SharesRedeemedMicros: redeemed,
			TotalSharesMicros:    total,
			PotNav:               USDCMicros(pot),
		})

		if redeemed <= 0 || total <= 0 || redeemed > total || pot < 0 {
			if err == nil {
				t.Fatalf("accepted redeemed=%d total=%d pot=%d", redeemed, total, pot)
			}
			return
		}
		num := new(big.Int).Mul(big.NewInt(redeemed), big.NewInt(pot))
		den := big.NewInt(total)
		if err != nil {
			// Valid inputs are only refused when the slice rounds to zero micros.
			if !halfUp(0, num, den) {
				t.Fatalf("refused payable slice redeemed=%d total=%d pot=%d: %v", redeemed, total, pot, err)
			}
			return
		}
		if slice.UsdcOwed <= 0 || int64(slice.UsdcOwed) > pot {
			t.Fatalf("owed %d outside (0, pot %d]", slice.UsdcOwed, pot)
		}
		if !halfUp(int64(slice.UsdcOwed), num, den) {
			t.Fatalf("owed %d for %d/%d of %d: more than half a micro from exact", slice.UsdcOwed, redeemed, total, pot)
		}
		if redeemed == total && int64(slice.UsdcOwed) != pot {
			t.Fatalf("full redeem owed %d, pot %d", slice.UsdcOwed, pot)
		}
		if slice.SharesRedeemedMicros != redeemed || slice.TotalSharesMicros != total {
			t.Fatalf("slice echoed shares %d/%d, want %d/%d", slice.SharesRedeemedMicros, slice.TotalSharesMicros, redeemed, total)
		}
	})
}

func FuzzComputePotNAV(f *testing.F) {
	f.Add(int64(50_000_000), "50", "0", int64(0), true)              // USDC-only, $1/share
	f.Add(int64(10_000_000), "100", "0.5", int64(200_000_000), true) // $10 + half a $200 stock
	f.Add(int64(0), "0", "0", int64(0), false)                       // bootstrap
	f.Add(int64(0), "", "1.25", int64(180_500_000), true)            // empty shares string
	f.Add(int64(math.MaxInt64), "1", "1", int64(1), true)            // sum overflows
	f.Add(int64(0), "1", "9223372036854775807", int64(math.MaxInt64), true)
	f.Add(int64(math.MaxInt64), "0.000001", "0", int64(0), false) // per-share overflows
	f.Add(int64(1), "9223372036854775807", "0", int64(0), false)  // per-share rounds to zero
	f.Add(int64(0), "1", "1e400", int64(1), true)                 // exponent units
	f.Add(int64(0), "1", "1/3", int64(3_000_000), true)           // big.Rat fraction syntax
	f.Add(int64(5), "1", "-1", int64(1_000_000), true)            // negative units
	f.Add(int64(5), "1", "1", int64(-1_000_000), true)            // negative mark
	f.Add(int64(-1), "1", "1", int64(1_000_000), true)            // negative treasury
	f.Add(int64(5), "abc", "xyz", int64(1), true)
	f.Add(int64(5), "-3", "1", int64(1), true)

	f.Fuzz(func(t *testing.T, treasury int64, totalShares, units string, mark int64, marked bool) {
		if len(totalShares) > 64 || len(units) > 64 {
			t.Skip("decimal strings this long are not ledger values")
		}
		in := NavInput{
			Mode:         NavUSDCOnly,
			TreasuryUsdc: USDCMicros(treasury),
			TotalShares:  ShareUnits(totalShares),
			Holdings:     []MarkedHolding{{Symbol: "AAPLx", Units: units, MarkUsdc: USDCMicros(mark)}},
		}
		if marked {
			in.Mode = NavMarked
		}

		nav, err := ComputePotNAV(in)

		if err != nil {
			if nav != (PotNAV{}) {
				t.Fatalf("error %v returned alongside non-zero nav %+v", err, nav)
			}
			return
		}
		if treasury < 0 {
			t.Fatalf("accepted negative treasury %d", treasury)
		}
		if nav.TotalUsdc < 0 || nav.PerShareUsdc < 0 {
			t.Fatalf("negative nav %+v from %+v", nav, in)
		}
		if nav.TotalUsdc < in.TreasuryUsdc {
			t.Fatalf("pot %d is worth less than its treasury %d", nav.TotalUsdc, treasury)
		}
		if !marked && nav.TotalUsdc != in.TreasuryUsdc {
			t.Fatalf("USDC-only pot %d != treasury %d", nav.TotalUsdc, treasury)
		}
		if marked {
			if mark < 0 {
				t.Fatalf("accepted negative mark %d", mark)
			}
			u, ok := new(big.Rat).SetString(units)
			if !ok || u.Sign() < 0 {
				t.Fatalf("accepted units %q", units)
			}
			value := new(big.Rat).Mul(u, big.NewRat(mark, 1))
			if !halfUp(int64(nav.TotalUsdc)-treasury, value.Num(), value.Denom()) {
				t.Fatalf("holding %q × %d valued at %d", units, mark, int64(nav.TotalUsdc)-treasury)
			}
		}
		if in.TotalShares.IsZero() {
			if nav.PerShareUsdc != BootstrapSharePriceMicros {
				t.Fatalf("no shares outstanding but per-share = %d", nav.PerShareUsdc)
			}
			return
		}
		shares, ok := new(big.Rat).SetString(totalShares)
		if !ok || shares.Sign() <= 0 {
			t.Fatalf("accepted total shares %q", totalShares)
		}
		perShare := new(big.Rat).Quo(big.NewRat(int64(nav.TotalUsdc), 1), shares)
		if !halfUp(int64(nav.PerShareUsdc), perShare.Num(), perShare.Denom()) {
			t.Fatalf("per-share %d for pot %d over %q shares", nav.PerShareUsdc, nav.TotalUsdc, totalShares)
		}
	})
}

func FuzzValidateIntent(f *testing.F) {
	const (
		buy  = true
		sell = false
	)
	f.Add("active", buy, "AAPLx", int64(400_000), int64(500_000), int64(0), int64(0), int64(1_000_000), int64(0))
	f.Add("active", buy, "AAPLx", int64(600_000), int64(500_000), int64(0), int64(0), int64(1_000_000), int64(0))             // over allocation
	f.Add("active", buy, "AAPLx", int64(200_000), int64(500_000), int64(200_000), int64(100_000), int64(1_000_000), int64(0)) // exact headroom
	f.Add("active", buy, "AAPLx", int64(200_001), int64(500_000), int64(200_000), int64(100_000), int64(1_000_000), int64(0))
	f.Add("active", buy, "AAPLx", int64(400_000), int64(500_000), int64(0), int64(0), int64(399_999), int64(0))                   // treasury short
	f.Add("active", buy, "AAPLx", int64(2), int64(0), int64(math.MaxInt64), int64(math.MaxInt64), int64(math.MaxInt64), int64(0)) // wraps to +2 unchecked
	f.Add("active", buy, "AAPLx", int64(1), int64(-3), int64(math.MaxInt64), int64(0), int64(math.MaxInt64), int64(0))
	f.Add("active", buy, "AAPLx", int64(1), int64(500_000), int64(math.MinInt64), int64(0), int64(1_000_000), int64(0))
	f.Add("active", sell, "AAPLx", int64(1_000_000), int64(0), int64(0), int64(0), int64(0), int64(1_000_000))
	f.Add("active", sell, "AAPLx", int64(1_000_001), int64(0), int64(0), int64(0), int64(0), int64(1_000_000))
	f.Add("active", sell, "", int64(1), int64(0), int64(0), int64(0), int64(0), int64(1_000_000))
	f.Add("paused", buy, "AAPLx", int64(1), int64(500_000), int64(0), int64(0), int64(1_000_000), int64(0))
	f.Add("revoked", sell, "AAPLx", int64(1), int64(0), int64(0), int64(0), int64(0), int64(1_000_000))
	f.Add("pending", buy, "AAPLx", int64(1), int64(500_000), int64(0), int64(0), int64(1_000_000), int64(0))

	f.Fuzz(func(t *testing.T, status string, isBuy bool, symbol string, amount, allocation, spent, pending, treasury, held int64) {
		agent := buildActiveAgent(func(a *GroupAgent) {
			a.Status = AgentStatus(status)
			a.AllocationUsdcMicros = allocation
		})
		snap := AgentTreasurySnapshot{
			TreasuryUsdcMicros:     treasury,
			AgentSpentUsdcMicros:   spent,
			PendingAgentUsdcMicros: pending,
			TokenHoldingsBySymbol:  map[string]int64{symbol: held},
		}
		intent := AgentIntentRequest{Side: AgentIntentSell, Symbol: symbol, TokenAmount: amount}
		if isBuy {
			intent = AgentIntentRequest{Side: AgentIntentBuy, Symbol: symbol, UsdcMicros: amount}
		}

		err := ValidateIntent(agent, intent, snap)

		if err == nil {
			assertIntentAcceptanceIsSafe(t, agent, intent, snap)
			return
		}
		// Completeness: a well-formed intent inside every limit must not be refused.
		if status != string(AgentStatusActive) || symbol == "" || amount <= 0 {
			return
		}
		if isBuy {
			if allocation < 0 || spent < 0 || pending < 0 {
				return
			}
			available := new(big.Int).Sub(big.NewInt(allocation), big.NewInt(spent))
			available.Sub(available, big.NewInt(pending))
			if big.NewInt(amount).Cmp(available) <= 0 && amount <= treasury {
				t.Fatalf("refused buy %d within headroom %s and treasury %d: %v", amount, available, treasury, err)
			}
		} else if amount <= held {
			t.Fatalf("refused sell %d with %d held: %v", amount, held, err)
		}
	})
}

func FuzzMemberEquity(f *testing.F) {
	f.Add("100", "200", int64(220_000_000))
	f.Add("0", "0", int64(1_000_000))
	f.Add("", "10", int64(1_000_000))
	f.Add("1", "3", int64(1))
	f.Add("9223372036854.775807", "9223372036854.775807", int64(math.MaxInt64))
	f.Add("1000000000000", "0.000001", int64(math.MaxInt64)) // overflows
	f.Add("5", "0", int64(1_000_000))                        // shares without a total
	f.Add("-1", "10", int64(1_000_000))
	f.Add("1", "10", int64(-1))
	f.Add("1e30", "1e-30", int64(7))
	f.Add("abc", "10", int64(7))

	f.Fuzz(func(t *testing.T, memberShares, totalShares string, pot int64) {
		if len(memberShares) > 64 || len(totalShares) > 64 {
			t.Skip("decimal strings this long are not ledger values")
		}

		equity, err := MemberEquity(ShareUnits(memberShares), ShareUnits(totalShares), USDCMicros(pot))

		if err != nil {
			if equity != 0 {
				t.Fatalf("error %v returned alongside equity %d", err, equity)
			}
			return
		}
		if pot < 0 {
			t.Fatalf("accepted negative pot %d", pot)
		}
		if equity < 0 {
			t.Fatalf("negative equity %d", equity)
		}
		if ShareUnits(memberShares).IsZero() {
			if equity != 0 {
				t.Fatalf("no shares but equity %d", equity)
			}
			return
		}
		member, okMember := new(big.Rat).SetString(memberShares)
		total, okTotal := new(big.Rat).SetString(totalShares)
		if !okMember || !okTotal || member.Sign() < 0 || total.Sign() <= 0 {
			t.Fatalf("accepted member=%q total=%q", memberShares, totalShares)
		}
		exact := new(big.Rat).Mul(new(big.Rat).Quo(member, total), big.NewRat(pot, 1))
		if !halfUp(int64(equity), exact.Num(), exact.Denom()) {
			t.Fatalf("equity %d for %q of %q in pot %d", equity, memberShares, totalShares, pot)
		}
		if member.Cmp(total) <= 0 && int64(equity) > pot {
			t.Fatalf("equity %d exceeds pot %d", equity, pot)
		}
	})
}
