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

    @Test func aSliceUnderTheFloorHasNoAmountThatWorks() {
        #expect(CashOutAmountRule.sliceIsBelowMinimum(sliceMicros: 50_000))
        #expect(CashOutAmountRule.sliceIsBelowMinimum(sliceMicros: floor) == false)
        #expect(CashOutAmountRule.sliceIsBelowMinimum(sliceMicros: 0) == false)
        // Every amount inside such a slice is refused, which is why the screen shows an
        // explanation instead of a keypad.
        #expect(CashOutAmountRule.verdict(enteredMicros: 50_000, sliceMicros: 50_000) == .belowMinimum)
    }
}
