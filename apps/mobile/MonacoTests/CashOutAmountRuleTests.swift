import Foundation
import MonacoCore
import Testing
@testable import Monaco

struct CashOutAmountRuleTests {
    private let floor = RedeemDustMinimum.usdcMicros // $0.10
    private let slice: Int64 = 50_000_000 // $50.00

    @Test func aPartialSaleThatLeavesAWorkableRemainderIsOrdinary() {
        #expect(CashOutAmountRule.verdict(enteredMicros: 20_000_000, sliceMicros: slice) == .ok)
    }

    @Test func nothingTypedIsNotAProblemToReport() {
        #expect(CashOutAmountRule.verdict(enteredMicros: 0, sliceMicros: slice) == .noAmount)
        #expect(CashOutAmountRule.problem(for: .noAmount) == nil)
    }

    @Test func underTheFloorSaysWhatTheFloorIs() {
        let verdict = CashOutAmountRule.verdict(enteredMicros: 50_000, sliceMicros: slice)
        #expect(verdict == .belowMinimum)
        #expect(CashOutAmountRule.maySubmit(verdict) == false)
        #expect(CashOutAmountRule.problem(for: verdict) == "Cash out at least $0.10")
    }

    @Test func moreThanTheSliceIsLeftToTheAmountPad() {
        let verdict = CashOutAmountRule.verdict(enteredMicros: slice + 1, sliceMicros: slice)
        #expect(verdict == .overSlice)
        #expect(CashOutAmountRule.maySubmit(verdict) == false)
        #expect(CashOutAmountRule.problem(for: verdict) == nil)
    }

    /// $49.95 of a $50.00 slice used to go out as a partial sale, stranding five cents that no
    /// amount, preset or "All" could ever cash out again.
    @Test func aSaleThatWouldStrandDustTakesTheWholeSlice() {
        let entered = slice - (floor - 1)
        let verdict = CashOutAmountRule.verdict(enteredMicros: entered, sliceMicros: slice)
        #expect(verdict == .sellsWholeSlice)
        #expect(CashOutAmountRule.maySubmit(verdict))
        #expect(
            CashOutAmountRule.effectiveMicros(for: verdict, enteredMicros: entered, sliceMicros: slice) == slice
        )
        #expect(CashOutAmountRule.note(for: verdict, sliceMicros: slice) == "We'll cash out your whole slice, $50.00")
    }

    @Test func aRemainderExactlyAtTheFloorIsFineToLeaveBehind() {
        let entered = slice - floor
        #expect(CashOutAmountRule.verdict(enteredMicros: entered, sliceMicros: slice) == .ok)
        #expect(
            CashOutAmountRule.effectiveMicros(for: .ok, enteredMicros: entered, sliceMicros: slice) == entered
        )
    }

    @Test func sellingEverythingIsAFullExit() {
        #expect(CashOutAmountRule.verdict(enteredMicros: slice, sliceMicros: slice) == .sellsWholeSlice)
    }

    // MARK: - What goes on the wire

    /// The highest-stakes decision on the screen: a full exit sends no share amount at all, so the
    /// backend closes the position outright. Passing the converted unit count here instead would
    /// send a partial sale for the full slice amount and strand the dust all over again — which is
    /// exactly what the rest of this rule exists to prevent.
    @Test func aFullExitSendsNoShareAmount() {
        let entered = slice - (floor - 1)
        let verdict = CashOutAmountRule.verdict(enteredMicros: entered, sliceMicros: slice)
        #expect(verdict == .sellsWholeSlice)

        let sale = CashOutAmountRule.sale(for: verdict, selectedShareUnits: 49_999_000)
        #expect(sale == .wholeSlice)
        #expect(sale?.shareAmountMicros == nil)
    }

    @Test func sellingTheWholeSliceOutrightAlsoSendsNoShareAmount() {
        let verdict = CashOutAmountRule.verdict(enteredMicros: slice, sliceMicros: slice)
        #expect(CashOutAmountRule.sale(for: verdict, selectedShareUnits: 50_000_000)?.shareAmountMicros == nil)
    }

    @Test func aPartialSaleSendsExactlyTheUnitsItSelected() {
        let sale = CashOutAmountRule.sale(for: .ok, selectedShareUnits: 1_234)
        #expect(sale == .units(1_234))
        #expect(sale?.shareAmountMicros == 1_234)
    }

    /// Nothing the screen refuses may reach the wire — and in particular must not fall through to
    /// the nil that means "sell everything".
    @Test func anAmountTheScreenWontTakeIsNotASaleAtAll() {
        #expect(CashOutAmountRule.sale(for: .noAmount, selectedShareUnits: 10) == nil)
        #expect(CashOutAmountRule.sale(for: .belowMinimum, selectedShareUnits: 10) == nil)
        #expect(CashOutAmountRule.sale(for: .overSlice, selectedShareUnits: 10) == nil)
        // A partial sale that converts to no units is not a sale either.
        #expect(CashOutAmountRule.sale(for: .ok, selectedShareUnits: 0) == nil)
    }

    /// A promotion to a full exit changes what the screen is about to do, so it stops saying
    /// "this much of your slice".
    @Test func theExplainerFollowsThePromotion() {
        #expect(CashOutAmountRule.explainer(for: .sellsWholeSlice).contains("your whole slice"))
        #expect(CashOutAmountRule.explainer(for: .ok).contains("this much of your slice"))
        #expect(CashOutAmountRule.explainer(for: .ok) != CashOutAmountRule.explainer(for: .sellsWholeSlice))
    }

    @Test func aSliceUnderTheFloorHasNoAmountThatWorks() {
        #expect(CashOutAmountRule.sliceIsBelowMinimum(sliceMicros: 50_000))
        #expect(CashOutAmountRule.sliceIsBelowMinimum(sliceMicros: floor) == false)
        #expect(CashOutAmountRule.sliceIsBelowMinimum(sliceMicros: 0) == false)
        // Every amount inside such a slice is refused, which is why the screen shows an
        // explanation instead of a keypad.
        #expect(CashOutAmountRule.verdict(enteredMicros: 50_000, sliceMicros: 50_000) == .belowMinimum)
    }
}
