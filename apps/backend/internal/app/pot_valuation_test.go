package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

// valuationPot is a two-member group used by the shared-valuation scenarios.
type valuationPot struct {
	h        integrationHarness
	groupID  string
	treasury string
	alice    valuationMember
	bob      valuationMember
}

type valuationMember struct {
	userID string
	token  string
	wallet string
}

func newValuationPot(t *testing.T, h integrationHarness, label string) valuationPot {
	t.Helper()
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Privy)

	member := func(name string) valuationMember {
		session := openTestSession(t, h.ISO, sessions, h.Privy, label+"-"+name, name)
		wallet, found, err := h.Store.GetMemberWalletByUserID(ctx, session.UserID)
		if err != nil || !found {
			t.Fatalf("GetMemberWalletByUserID: found=%v err=%v", found, err)
		}
		return valuationMember{
			userID: session.UserID,
			token:  string(privy.AccessToken(h.ISO.UniqueToken(label + "-" + name))),
			wallet: wallet.SolanaAddress,
		}
	}
	alice, bob := member("alice"), member("bob")

	group, err := h.Groups.CreateGroup(ctx, alice.token, testGroupName(h.ISO, label))
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	ensureGroupMember(t, h.Store, group.GroupID, alice.userID)
	ensureGroupMember(t, h.Store, group.GroupID, bob.userID)

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, group.GroupID)
	if err != nil || !found {
		t.Fatalf("GetTreasuryByGroupID: found=%v err=%v", found, err)
	}
	return valuationPot{h: h, groupID: group.GroupID, treasury: treasury.SolanaAddress, alice: alice, bob: bob}
}

// credit books a deposit that is already on the ledger: shares minted 1:1 at par.
func (p valuationPot) credit(t *testing.T, member valuationMember, usdc int64) {
	t.Helper()
	ctx := context.Background()
	tx, err := p.h.Store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := p.h.Store.IncrementPositionTx(ctx, tx, member.userID, p.groupID, usdc, usdc); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func (p valuationPot) buy(t *testing.T, label string, usdc, atomics int64) {
	t.Helper()
	if _, _, err := p.h.Store.ConfirmBuyTransaction(context.Background(), postgres.ConfirmBuyTransactionParams{
		GroupID: p.groupID, Amount: usdc, InputMint: jupiter.USDCMint, OutputMint: jupiter.AAPLxMint,
		TxSignature: testTxSignature(p.h.ISO, label), ExecuteRequestID: testRequestID(p.h.ISO, label),
		CostBasisPrice: usdc, CostBasisAmount: atomics,
	}); err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}
}

func (p valuationPot) sell(t *testing.T, label string, atomics, proceeds int64) {
	t.Helper()
	if _, _, err := p.h.Store.ConfirmSellTransaction(context.Background(), postgres.ConfirmSellTransactionParams{
		GroupID: p.groupID, Amount: atomics, InputMint: jupiter.AAPLxMint, OutputMint: jupiter.USDCMint,
		TxSignature: testTxSignature(p.h.ISO, label), ExecuteRequestID: testRequestID(p.h.ISO, label),
		ProceedsUSDC: proceeds,
	}); err != nil {
		t.Fatalf("ConfirmSellTransaction: %v", err)
	}
}

// roundTrip books a $100 buy that was sold again for proceeds, leaving a cash-only pot.
func (p valuationPot) roundTrip(t *testing.T, label string, proceeds int64) {
	t.Helper()
	p.buy(t, label+"-buy", 100_000_000, 100_000_000)
	p.sell(t, label+"-sell", 100_000_000, proceeds)
}

func (p valuationPot) setTreasury(t *testing.T, usdc int64) {
	t.Helper()
	seedTestTreasuryUSDC(t, p.h.Privy, p.treasury, usdc)
}

func (p valuationPot) position(t *testing.T, member valuationMember) postgres.PositionRow {
	t.Helper()
	row, found, err := p.h.Store.GetPosition(context.Background(), member.userID, p.groupID)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if !found {
		return postgres.PositionRow{UserID: member.userID, GroupID: p.groupID}
	}
	return row
}

// sweep records a pending deposit and reports it as confirmed on chain.
func (p valuationPot) sweep(t *testing.T, member valuationMember, label string, usdc int64) (ObserveSweepResult, string, error) {
	t.Helper()
	ctx := context.Background()
	deposit, err := p.h.Store.InsertDeposit(ctx, member.userID, p.groupID, usdc, member.wallet)
	if err != nil {
		t.Fatalf("InsertDeposit: %v", err)
	}
	result, err := p.observe(member, deposit.ID, label, usdc)
	return result, deposit.ID, err
}

func (p valuationPot) observe(member valuationMember, depositID, label string, usdc int64) (ObserveSweepResult, error) {
	return p.h.Deposits.ObserveSweep(context.Background(), ObservedSweep{
		TxSignature: testTxSignature(p.h.ISO, label),
		FromAddress: member.wallet,
		ToAddress:   p.treasury,
		Amount:      usdc,
		DepositID:   depositID,
		UserID:      member.userID,
		GroupID:     p.groupID,
	})
}

const (
	// A $50 buy of half an AAPLx share: cost-basis mark $100.
	valuationBuyUSDC    = int64(50_000_000)
	valuationBuyAtomics = int64(50_000_000)
)

// Two members put in $100 each and $50 buys stock. What the first one out receives must track
// the live mark, so the member who stays is left with exactly the other half of the pot.
func TestWithdrawToBalance_paysSliceAtLiveMarks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		mark      func(h integrationHarness, groupID string)
		wantPaid  int64
		wantErr   error
		wantStays int64
	}{
		{
			name:      "stock halves: redeemer takes half of the loss",
			mark:      func(h integrationHarness, groupID string) { registerLiveAAPLxMark(h, groupID, 50_000_000) },
			wantPaid:  87_500_000,
			wantStays: 87_500_000,
		},
		{
			name:      "stock doubles: redeemer takes half of the gain",
			mark:      func(h integrationHarness, groupID string) { registerLiveAAPLxMark(h, groupID, 200_000_000) },
			wantPaid:  125_000_000,
			wantStays: 125_000_000,
		},
		{
			name: "oracle down: nothing is paid and the shares stay put",
			mark: func(h integrationHarness, groupID string) {
				pyth.RegisterMarkedPotError(h.Pyth, pyth.TreasuryRef{GroupID: groupID}, fmt.Errorf("hermes down"))
			},
			wantErr: ErrPotMarkUnavailable,
		},
		{
			name: "price chain only has cost basis: nothing is paid",
			mark: func(h integrationHarness, groupID string) {
				pyth.RegisterMarkedPot(h.Pyth, pyth.TreasuryRef{GroupID: groupID}, pyth.NavInput{
					Holdings: []pyth.MarkedHolding{{
						Symbol: "AAPLx", Mint: jupiter.AAPLxMint, MarkUsdc: 100_000_000, Source: pyth.MarkSourceCostBasis,
					}},
				})
			},
			wantErr: ErrPotMarkUnavailable,
		},
	}

	for i, tc := range cases {
		i, tc := i, tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := integrationApp(t)
			ctx := context.Background()
			pot := newValuationPot(t, h, fmt.Sprintf("nav-redeem-%d", i))
			pot.credit(t, pot.alice, 100_000_000)
			pot.credit(t, pot.bob, 100_000_000)
			pot.buy(t, fmt.Sprintf("nav-redeem-%d-buy", i), valuationBuyUSDC, valuationBuyAtomics)
			pot.setTreasury(t, 150_000_000)
			tc.mark(h, pot.groupID)

			job, err := h.Redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{AccessToken: pot.alice.token, GroupID: pot.groupID})

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("WithdrawToBalance err = %v, want %v", err, tc.wantErr)
				}
				if _, paid := privy.LastPayUSDCRequest(h.Privy); paid {
					t.Fatal("a payout was broadcast without a live mark")
				}
				if got := pot.position(t, pot.alice).ShareUnits; got != 100_000_000 {
					t.Fatalf("alice share_units = %d, want 100000000 untouched", got)
				}
				if active, err := h.Store.HasActiveRedeemJobForUser(ctx, pot.alice.userID, pot.groupID); err != nil || active {
					t.Fatalf("active redeem job = %v err = %v, want none left behind", active, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("WithdrawToBalance: %v", err)
			}
			if job.Status != domain.RedeemJobSettled || job.SliceUsdc != tc.wantPaid {
				t.Fatalf("job = %+v, want settled for %d", job, tc.wantPaid)
			}
			payout, paid := privy.LastPayUSDCRequest(h.Privy)
			if !paid || payout.Amount != tc.wantPaid {
				t.Fatalf("payout = %+v (paid=%v), want %d", payout, paid, tc.wantPaid)
			}

			// Bob now owns the whole pot, and it is worth what Alice was paid.
			valuation, err := valuePot(ctx, h.Store, h.Pyth, h.Symbols, nil, pot.groupID, pot.treasury, 150_000_000-tc.wantPaid, potMarksLiveOnly)
			if err != nil {
				t.Fatalf("valuePot: %v", err)
			}
			if valuation.ShareBaseMicros != 100_000_000 || valuation.PotNavMicros != tc.wantStays {
				t.Fatalf("pot after payout = %+v, want %d for bob's 100000000 units", valuation, tc.wantStays)
			}
		})
	}
}

// A deposit is priced at pre-credit NAV over the shares outstanding, whatever the pot holds.
func TestObserveSweep_mintsSharesAtPreCreditNav(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		seed         func(t *testing.T, pot valuationPot, label string)
		preTreasury  int64
		deposit      int64
		wantMinted   int64
		wantNavAfter int64
	}{
		{
			name:         "first deposit mints 1:1",
			seed:         func(t *testing.T, pot valuationPot, label string) {},
			deposit:      100_000_000,
			wantMinted:   100_000_000,
			wantNavAfter: 100_000_000,
		},
		{
			name: "cash-only pot after a losing round trip",
			seed: func(t *testing.T, pot valuationPot, label string) {
				pot.credit(t, pot.alice, 100_000_000)
				pot.roundTrip(t, label, 90_000_000)
			},
			preTreasury:  90_000_000,
			deposit:      90_000_000,
			wantMinted:   100_000_000,
			wantNavAfter: 180_000_000,
		},
		{
			name: "cash-only pot after a winning round trip",
			seed: func(t *testing.T, pot valuationPot, label string) {
				pot.credit(t, pot.alice, 100_000_000)
				pot.roundTrip(t, label, 110_000_000)
			},
			preTreasury:  110_000_000,
			deposit:      55_000_000,
			wantMinted:   50_000_000,
			wantNavAfter: 165_000_000,
		},
		{
			name: "marked pot after the stock halves",
			seed: func(t *testing.T, pot valuationPot, label string) {
				pot.credit(t, pot.alice, 100_000_000)
				pot.buy(t, label+"-buy", valuationBuyUSDC, valuationBuyAtomics)
				registerLiveAAPLxMark(pot.h, pot.groupID, 50_000_000)
			},
			preTreasury:  50_000_000,
			deposit:      75_000_000,
			wantMinted:   100_000_000,
			wantNavAfter: 150_000_000,
		},
		{
			name: "another member's sweep has landed but is not credited yet",
			seed: func(t *testing.T, pot valuationPot, label string) {
				pot.credit(t, pot.alice, 100_000_000)
				if _, err := pot.h.Store.InsertDeposit(context.Background(), pot.alice.userID, pot.groupID, 40_000_000, pot.alice.wallet); err != nil {
					t.Fatalf("InsertDeposit: %v", err)
				}
			},
			// $100 credited + $40 of Alice's USDC already sitting in the treasury uncredited.
			preTreasury:  140_000_000,
			deposit:      100_000_000,
			wantMinted:   100_000_000,
			wantNavAfter: 200_000_000,
		},
	}

	for i, tc := range cases {
		i, tc := i, tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := integrationApp(t)
			label := fmt.Sprintf("nav-mint-%d", i)
			pot := newValuationPot(t, h, label)
			tc.seed(t, pot, label)
			pot.setTreasury(t, tc.preTreasury+tc.deposit)

			result, _, err := pot.sweep(t, pot.bob, label+"-sweep", tc.deposit)

			if err != nil {
				t.Fatalf("ObserveSweep: %v", err)
			}
			if !result.Credited || result.Position.ShareUnits != tc.wantMinted {
				t.Fatalf("bob share_units = %d (credited=%v), want %d", result.Position.ShareUnits, result.Credited, tc.wantMinted)
			}
			if result.Position.AmountDeposited != tc.deposit {
				t.Fatalf("bob amount_deposited = %d, want %d", result.Position.AmountDeposited, tc.deposit)
			}
			snapshots, err := h.Store.ListNavSnapshotsByGroup(context.Background(), pot.groupID)
			if err != nil || len(snapshots) == 0 {
				t.Fatalf("ListNavSnapshotsByGroup: len=%d err=%v", len(snapshots), err)
			}
			if snapshots[0].PotNavMicros != tc.wantNavAfter {
				t.Fatalf("snapshot pot nav = %d, want %d", snapshots[0].PotNavMicros, tc.wantNavAfter)
			}
		})
	}
}

// With no live mark the deposit must wait: it stays pending, mints nothing, and credits at the
// live price once the oracle is back.
func TestObserveSweep_oracleDown_defersThenCreditsAtLiveMark(t *testing.T) {
	t.Parallel()
	h := integrationApp(t)
	ctx := context.Background()
	pot := newValuationPot(t, h, "nav-defer")
	pot.credit(t, pot.alice, 100_000_000)
	pot.buy(t, "nav-defer-buy", valuationBuyUSDC, valuationBuyAtomics)
	pot.setTreasury(t, 50_000_000+75_000_000)
	pyth.RegisterMarkedPotError(h.Pyth, pyth.TreasuryRef{GroupID: pot.groupID}, fmt.Errorf("hermes down"))

	_, depositID, err := pot.sweep(t, pot.bob, "nav-defer-sweep", 75_000_000)

	if !errors.Is(err, ErrPotMarkUnavailable) {
		t.Fatalf("ObserveSweep err = %v, want ErrPotMarkUnavailable", err)
	}
	deposit, found, err := h.Store.GetDepositByID(ctx, depositID)
	if err != nil || !found {
		t.Fatalf("GetDepositByID: found=%v err=%v", found, err)
	}
	if deposit.Status != "pending" {
		t.Fatalf("deposit status = %q, want pending so the poller retries", deposit.Status)
	}
	if got := pot.position(t, pot.bob).ShareUnits; got != 0 {
		t.Fatalf("bob share_units = %d, want 0 while the pot cannot be priced", got)
	}

	// The oracle recovers with the stock at half its cost: the retry mints at that price.
	registerLiveAAPLxMark(h, pot.groupID, 50_000_000)
	result, err := pot.observe(pot.bob, depositID, "nav-defer-sweep", 75_000_000)
	if err != nil {
		t.Fatalf("ObserveSweep retry: %v", err)
	}
	if !result.Credited || result.Position.ShareUnits != 100_000_000 {
		t.Fatalf("bob share_units = %d (credited=%v), want 100000000", result.Position.ShareUnits, result.Credited)
	}
}

// Only USDC the ledger cannot explain is an uncredited deposit.
func TestCreditUncreditedTreasuryUSDC_creditsOnlyUnexplainedUSDC(t *testing.T) {
	t.Parallel()

	type want struct {
		credited       bool
		aliceShares    int64
		aliceDeposited int64
		bobShares      int64
		bobDeposited   int64
	}
	cases := []struct {
		name string
		seed func(t *testing.T, pot valuationPot, label string)
		want want
	}{
		{
			name: "realized gain stays P&L",
			seed: func(t *testing.T, pot valuationPot, label string) {
				pot.credit(t, pot.alice, 100_000_000)
				pot.roundTrip(t, label, 110_000_000)
				pot.setTreasury(t, 110_000_000)
			},
			want: want{aliceShares: 100_000_000, aliceDeposited: 100_000_000},
		},
		{
			name: "usdc reserved for a redeem in flight is not handed to the others",
			seed: func(t *testing.T, pot valuationPot, label string) {
				pot.credit(t, pot.alice, 100_000_000)
				pot.credit(t, pot.bob, 100_000_000)
				pot.setTreasury(t, 200_000_000)
				wedgeRedeemJobInPaying(t, pot.h, pot.alice.userID, pot.groupID, pot.alice.wallet, 100_000_000, 100_000_000)
			},
			want: want{aliceDeposited: 100_000_000, bobShares: 100_000_000, bobDeposited: 100_000_000},
		},
		{
			name: "stray transfer is minted at nav and split by holding",
			seed: func(t *testing.T, pot valuationPot, label string) {
				pot.credit(t, pot.alice, 75_000_000)
				pot.credit(t, pot.bob, 25_000_000)
				pot.roundTrip(t, label, 110_000_000)
				// $110 of pot on the books plus $55 nobody deposited through the app.
				pot.setTreasury(t, 165_000_000)
			},
			want: want{credited: true, aliceShares: 112_500_000, aliceDeposited: 116_250_000, bobShares: 37_500_000, bobDeposited: 38_750_000},
		},
		{
			name: "stray transfer waits while the holdings cannot be priced",
			seed: func(t *testing.T, pot valuationPot, label string) {
				pot.credit(t, pot.alice, 100_000_000)
				pot.buy(t, label+"-buy", valuationBuyUSDC, valuationBuyAtomics)
				pot.setTreasury(t, 50_000_000+20_000_000)
				pyth.RegisterMarkedPotError(pot.h.Pyth, pyth.TreasuryRef{GroupID: pot.groupID}, fmt.Errorf("hermes down"))
			},
			want: want{aliceShares: 100_000_000, aliceDeposited: 100_000_000},
		},
		{
			name: "unconfirmed swap may explain the difference",
			seed: func(t *testing.T, pot valuationPot, label string) {
				pot.credit(t, pot.alice, 100_000_000)
				pot.setTreasury(t, 130_000_000)
				if _, _, err := pot.h.Store.InsertPendingTransaction(context.Background(), postgres.InsertPendingTransactionParams{
					GroupID: pot.groupID, Action: postgres.TransactionActionSell,
					InputMint: jupiter.AAPLxMint, OutputMint: jupiter.USDCMint,
					Amount: 30_000_000, ExecuteRequestID: testRequestID(pot.h.ISO, label+"-pending"),
				}); err != nil {
					t.Fatalf("InsertPendingTransaction: %v", err)
				}
			},
			want: want{aliceShares: 100_000_000, aliceDeposited: 100_000_000},
		},
	}

	for i, tc := range cases {
		i, tc := i, tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := integrationApp(t)
			label := fmt.Sprintf("nav-reconcile-%d", i)
			pot := newValuationPot(t, h, label)
			tc.seed(t, pot, label)

			credited, err := h.Deposits.CreditUncreditedTreasuryUSDC(context.Background(), pot.groupID)

			if err != nil {
				t.Fatalf("CreditUncreditedTreasuryUSDC: %v", err)
			}
			if credited != tc.want.credited {
				t.Fatalf("credited = %v, want %v", credited, tc.want.credited)
			}
			alice, bob := pot.position(t, pot.alice), pot.position(t, pot.bob)
			if alice.ShareUnits != tc.want.aliceShares || alice.AmountDeposited != tc.want.aliceDeposited {
				t.Fatalf("alice = %d units / %d deposited, want %d / %d", alice.ShareUnits, alice.AmountDeposited, tc.want.aliceShares, tc.want.aliceDeposited)
			}
			if bob.ShareUnits != tc.want.bobShares || bob.AmountDeposited != tc.want.bobDeposited {
				t.Fatalf("bob = %d units / %d deposited, want %d / %d", bob.ShareUnits, bob.AmountDeposited, tc.want.bobShares, tc.want.bobDeposited)
			}
		})
	}
}

// Every path reads the same valuation: cash the ledger accounts for plus holdings at the marks
// the policy allows, over every outstanding claim.
func TestValuePot_policiesAndShareBase(t *testing.T) {
	t.Parallel()
	h := integrationApp(t)
	ctx := context.Background()
	pot := newValuationPot(t, h, "nav-policy")
	pot.credit(t, pot.alice, 100_000_000)
	pot.credit(t, pot.bob, 100_000_000)
	pot.buy(t, "nav-policy-buy", valuationBuyUSDC, valuationBuyAtomics)
	wedgeRedeemJobInPaying(t, h, pot.alice.userID, pot.groupID, pot.alice.wallet, 40_000_000, 40_000_000)
	pyth.RegisterMarkedPotError(h.Pyth, pyth.TreasuryRef{GroupID: pot.groupID}, fmt.Errorf("hermes down"))

	// $150 on the ledger; $10 more on chain that nobody has been credited for.
	const treasuryUSDC = int64(160_000_000)

	if _, err := valuePot(ctx, h.Store, h.Pyth, h.Symbols, nil, pot.groupID, pot.treasury, treasuryUSDC, potMarksLiveOnly); !errors.Is(err, ErrPotMarkUnavailable) {
		t.Fatalf("live-only valuation err = %v, want ErrPotMarkUnavailable", err)
	}

	display, err := valuePot(ctx, h.Store, h.Pyth, h.Symbols, nil, pot.groupID, pot.treasury, treasuryUSDC, potMarksBestAvailable)
	if err != nil {
		t.Fatalf("best-available valuation: %v", err)
	}
	if display.CashMicros != 150_000_000 || display.HoldingsMicros != 50_000_000 || display.PotNavMicros != 200_000_000 {
		t.Fatalf("display valuation = %+v, want cash 150 + holdings 50 at cost basis", display)
	}
	if display.ShareBaseMicros != 200_000_000 {
		t.Fatalf("share base = %d, want 200000000 including the in-flight redeem", display.ShareBaseMicros)
	}
	if len(display.Marked.Holdings) != 1 || display.Marked.Holdings[0].Source != pyth.MarkSourceCostBasis {
		t.Fatalf("display marks = %+v, want one cost-basis mark", display.Marked.Holdings)
	}

	registerLiveAAPLxMark(h, pot.groupID, 300_000_000)
	live, err := valuePot(ctx, h.Store, h.Pyth, h.Symbols, nil, pot.groupID, pot.treasury, treasuryUSDC, potMarksLiveOnly)
	if err != nil {
		t.Fatalf("live valuation: %v", err)
	}
	if live.PotNavMicros != 300_000_000 || live.HoldingsMicros != 150_000_000 {
		t.Fatalf("live valuation = %+v, want cash 150 + holdings 150", live)
	}
	snapshot, err := live.navSnapshotValues()
	if err != nil {
		t.Fatalf("navSnapshotValues: %v", err)
	}
	if snapshot.NavPerShareMicros != 1_500_000 || snapshot.TotalShares != 200_000_000 {
		t.Fatalf("snapshot = %+v, want $1.50 over 200000000 units", snapshot)
	}
}
