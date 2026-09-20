package domain

import "testing"

func TestSharesForDeposit_usdcOnlyPot_creditsOneToOneWithSweptUsdc(t *testing.T) {
	// Arrange
	nav := PotNAV{
		TotalUsdc:    50_000_000,
		PerShareUsdc: BootstrapSharePriceMicros,
	}
	totalSharesMicros := int64(50_000_000)
	deposited := USDCMicros(4_000_000)

	// Act
	shares, err := SharesForDeposit(deposited, totalSharesMicros, nav.TotalUsdc)
	micros, errMicros := ShareUnitsMicrosForDeposit(deposited, totalSharesMicros, nav.TotalUsdc)

	// Assert
	if err != nil {
		t.Fatalf("SharesForDeposit: %v", err)
	}
	if errMicros != nil {
		t.Fatalf("ShareUnitsMicrosForDeposit: %v", errMicros)
	}
	if shares != ShareUnits("4") {
		t.Fatalf("shares = %q, want %q", shares, "4")
	}
	if micros != 4_000_000 {
		t.Fatalf("share micros = %d, want %d", micros, 4_000_000)
	}
}

func TestSharesForDeposit_markedPot_mintsSharesFromPotNav(t *testing.T) {
	// Arrange — pot $110, 100 shares outstanding, share price $1.10
	nav := PotNAV{
		TotalUsdc:    110_000_000,
		PerShareUsdc: 1_100_000,
	}
	totalSharesMicros := int64(100_000_000)
	deposited := USDCMicros(110_000_000)

	// Act
	shares, err := SharesForDeposit(deposited, totalSharesMicros, nav.TotalUsdc)
	micros, errMicros := ShareUnitsMicrosForDeposit(deposited, totalSharesMicros, nav.TotalUsdc)

	// Assert
	if err != nil {
		t.Fatalf("SharesForDeposit: %v", err)
	}
	if errMicros != nil {
		t.Fatalf("ShareUnitsMicrosForDeposit: %v", errMicros)
	}
	if shares != ShareUnits("100") {
		t.Fatalf("shares = %q, want %q", shares, "100")
	}
	if micros != 100_000_000 {
		t.Fatalf("share micros = %d, want %d", micros, 100_000_000)
	}
}

type alexBlairLiterals struct {
	alexDepositUSDC   USDCMicros
	blairDepositUSDC  USDCMicros
	postBuyPotUSDC    USDCMicros
	alexShares        ShareUnits
	blairShares       ShareUnits
	alexShareMicros   int64
	blairShareMicros  int64
	postAlexPerShare  USDCMicros
	postBuyPerShare   USDCMicros
	postBlairTotalPot USDCMicros
	totalSharesAfter  ShareUnits
}

func readmeAlexBlairScenario() alexBlairLiterals {
	return alexBlairLiterals{
		alexDepositUSDC:   100_000_000,
		blairDepositUSDC:  110_000_000,
		postBuyPotUSDC:    110_000_000,
		alexShares:        ShareUnits("100"),
		blairShares:       ShareUnits("100"),
		alexShareMicros:   100_000_000,
		blairShareMicros:  100_000_000,
		postAlexPerShare:  BootstrapSharePriceMicros,
		postBuyPerShare:   1_100_000,
		postBlairTotalPot: 220_000_000,
		totalSharesAfter:  ShareUnits("200"),
	}
}

func TestDepositCredit_readmeAlexBlairWorkedExample_matchesLiterals(t *testing.T) {
	// Arrange
	scenario := readmeAlexBlairScenario()
	emptyNav, err := ComputePotNAV(NavInput{
		Mode:         NavUSDCOnly,
		TreasuryUsdc: 0,
		TotalShares:  ShareUnits("0"),
	})
	if err != nil {
		t.Fatalf("empty ComputePotNAV: %v", err)
	}

	// Act — step 2: Alex deposits $100 at $1/share
	alexShares, err := SharesForDeposit(scenario.alexDepositUSDC, 0, emptyNav.TotalUsdc)
	if err != nil {
		t.Fatalf("Alex SharesForDeposit: %v", err)
	}
	alexMicros, err := ShareUnitsMicrosForDeposit(scenario.alexDepositUSDC, 0, emptyNav.TotalUsdc)
	if err != nil {
		t.Fatalf("Alex ShareUnitsMicrosForDeposit: %v", err)
	}
	postAlexNav, err := ComputePotNAV(NavInput{
		Mode:         NavUSDCOnly,
		TreasuryUsdc: scenario.alexDepositUSDC,
		TotalShares:  alexShares,
	})
	if err != nil {
		t.Fatalf("post-Alex ComputePotNAV: %v", err)
	}

	// Act — step 4: AAPLx +10%, pot $110, share price $1.10
	postBuyNav, err := ComputePotNAV(NavInput{
		Mode:         NavMarked,
		TreasuryUsdc: 0,
		TotalShares:  scenario.alexShares,
		Holdings: []MarkedHolding{{
			Symbol:    "AAPLx",
			Units:     "1",
			MarkUsdc:  scenario.postBuyPotUSDC,
			CostBasis: 100_000_000,
		}},
	})
	if err != nil {
		t.Fatalf("post-buy ComputePotNAV: %v", err)
	}

	// Act — step 5: Blair deposits $110
	blairShares, err := SharesForDeposit(scenario.blairDepositUSDC, scenario.alexShareMicros, postBuyNav.TotalUsdc)
	if err != nil {
		t.Fatalf("Blair SharesForDeposit: %v", err)
	}
	blairMicros, err := ShareUnitsMicrosForDeposit(scenario.blairDepositUSDC, scenario.alexShareMicros, postBuyNav.TotalUsdc)
	if err != nil {
		t.Fatalf("Blair ShareUnitsMicrosForDeposit: %v", err)
	}
	postBlairNav, err := ComputePotNAV(NavInput{
		Mode:         NavMarked,
		TreasuryUsdc: scenario.blairDepositUSDC,
		TotalShares:  ShareUnits("200"),
		Holdings: []MarkedHolding{{
			Symbol:    "AAPLx",
			Units:     "1",
			MarkUsdc:  scenario.postBuyPotUSDC,
			CostBasis: 100_000_000,
		}},
	})
	if err != nil {
		t.Fatalf("post-Blair ComputePotNAV: %v", err)
	}

	// Act — step 6: Blair redeems 50 shares = 50/200 of pot = $55 USDC
	blairRedeemShares := int64(50_000_000)
	totalSharesMicros := int64(200_000_000)
	redeemSlice, err := ComputeRedeemSlice(RedeemSliceInput{
		SharesRedeemedMicros: blairRedeemShares,
		TotalSharesMicros:    totalSharesMicros,
		PotNav:               scenario.postBlairTotalPot,
	})
	if err != nil {
		t.Fatalf("Blair redeem ComputeRedeemSlice: %v", err)
	}

	// Assert — README literals steps 1–6
	if emptyNav.PerShareUsdc != scenario.postAlexPerShare {
		t.Fatalf("empty per-share = %d, want %d", emptyNav.PerShareUsdc, scenario.postAlexPerShare)
	}
	if alexShares != scenario.alexShares {
		t.Fatalf("Alex shares = %q, want %q", alexShares, scenario.alexShares)
	}
	if alexMicros != scenario.alexShareMicros {
		t.Fatalf("Alex share micros = %d, want %d", alexMicros, scenario.alexShareMicros)
	}
	if postAlexNav.TotalUsdc != scenario.alexDepositUSDC {
		t.Fatalf("post-Alex pot = %d, want %d", postAlexNav.TotalUsdc, scenario.alexDepositUSDC)
	}
	if postBuyNav.TotalUsdc != scenario.postBuyPotUSDC {
		t.Fatalf("post-buy pot = %d, want %d", postBuyNav.TotalUsdc, scenario.postBuyPotUSDC)
	}
	if postBuyNav.PerShareUsdc != scenario.postBuyPerShare {
		t.Fatalf("post-buy per-share = %d, want %d", postBuyNav.PerShareUsdc, scenario.postBuyPerShare)
	}
	if blairShares != scenario.blairShares {
		t.Fatalf("Blair shares = %q, want %q", blairShares, scenario.blairShares)
	}
	if blairMicros != scenario.blairShareMicros {
		t.Fatalf("Blair share micros = %d, want %d", blairMicros, scenario.blairShareMicros)
	}
	if postBlairNav.TotalUsdc != scenario.postBlairTotalPot {
		t.Fatalf("post-Blair pot = %d, want %d", postBlairNav.TotalUsdc, scenario.postBlairTotalPot)
	}
	if postBlairNav.PerShareUsdc != scenario.postBuyPerShare {
		t.Fatalf("post-Blair per-share = %d, want %d", postBlairNav.PerShareUsdc, scenario.postBuyPerShare)
	}
	if redeemSlice.UsdcOwed != 55_000_000 {
		t.Fatalf("Blair redeem slice = %d, want 55000000 ($55)", redeemSlice.UsdcOwed)
	}
	if scenario.totalSharesAfter != ShareUnits("200") {
		t.Fatalf("total shares after Blair deposit = %q, want 200", scenario.totalSharesAfter)
	}
}
