import Foundation
import MonacoCore

/// What a typed cash-out amount means, and what to tell the member about it.
///
/// Cash out has a floor ($0.10, `RedeemDustMinimum`, matched by the backend). The screen used to
/// fold that floor into one boolean: an amount under it simply left the button grey, with the
/// helper still cheerfully reporting what the slice was worth. Worse, nothing stopped a sale that
/// left a remainder under the floor behind — $49.95 of a $50.00 slice strands five cents that no
/// amount, preset or "All" can ever cash out again.
///
/// So the rule is not "may I submit" but "what is this amount": the screen shows the reason, and a
/// sale that would strand dust is promoted to a full exit instead.
enum CashOutAmountRule {
    enum Verdict: Equatable {
        /// Nothing typed yet.
        case noAmount
        /// Under the floor, so it cannot be sold at all.
        case belowMinimum
        /// More than the slice is worth.
        case overSlice
        /// The whole slice goes: either that is what was typed, or what would be left behind is
        /// under the floor and would be stranded.
        case sellsWholeSlice
        /// A partial sale that leaves a workable remainder.
        case ok
    }

    static func verdict(
        enteredMicros: Int64,
        sliceMicros: Int64,
        minimumMicros: Int64 = RedeemDustMinimum.usdcMicros
    ) -> Verdict {
        guard enteredMicros > 0 else { return .noAmount }
        guard enteredMicros <= sliceMicros else { return .overSlice }
        guard enteredMicros >= minimumMicros else { return .belowMinimum }
        let remainder = sliceMicros - enteredMicros
        if remainder == 0 || remainder < minimumMicros { return .sellsWholeSlice }
        return .ok
    }

    /// A slice worth something, but less than the floor: there is no amount the member can type.
    static func sliceIsBelowMinimum(
        sliceMicros: Int64,
        minimumMicros: Int64 = RedeemDustMinimum.usdcMicros
    ) -> Bool {
        sliceMicros > 0 && sliceMicros < minimumMicros
    }

    /// What is actually sold: a verdict of `sellsWholeSlice` takes the slice, not the typed figure.
    static func effectiveMicros(
        for verdict: Verdict,
        enteredMicros: Int64,
        sliceMicros: Int64
    ) -> Int64 {
        switch verdict {
        case .sellsWholeSlice: return sliceMicros
        case .ok: return enteredMicros
        case .noAmount, .belowMinimum, .overSlice: return 0
        }
    }

    /// What a sale puts on the wire.
    enum Sale: Equatable {
        /// Close the position outright: the request carries no share amount at all. Sending a
        /// share amount for a full exit is what strands dust in the first place, because those
        /// units are a rounded conversion of a dollar figure and land a hair short of the slice.
        case wholeSlice
        /// A partial sale of exactly this many share units.
        case units(Int64)

        /// `shareAmountMicros` on the withdraw request. `nil` is the full exit.
        var shareAmountMicros: Int64? {
            switch self {
            case .wholeSlice: return nil
            case .units(let units): return units
            }
        }
    }

    /// The sale a verdict and a unit count add up to, or `nil` when there is nothing to send.
    ///
    /// This is the one decision on the screen that can turn a member's typed partial sale into a
    /// full position exit, so it lives here where it can be tested rather than inline in the
    /// submit path.
    static func sale(for verdict: Verdict, selectedShareUnits: Int64) -> Sale? {
        switch verdict {
        case .sellsWholeSlice:
            return .wholeSlice
        case .ok:
            return selectedShareUnits > 0 ? .units(selectedShareUnits) : nil
        case .noAmount, .belowMinimum, .overSlice:
            return nil
        }
    }

    static func maySubmit(_ verdict: Verdict) -> Bool {
        switch verdict {
        case .ok, .sellsWholeSlice: return true
        case .noAmount, .belowMinimum, .overSlice: return false
        }
    }

    /// Why the amount can't be used, in the member's words. `nil` when nothing is wrong with it.
    static func problem(
        for verdict: Verdict,
        minimumMicros: Int64 = RedeemDustMinimum.usdcMicros
    ) -> String? {
        switch verdict {
        case .belowMinimum:
            return "Cash out at least \(UsdAmountFormatter.format(micros: minimumMicros))"
        case .noAmount, .overSlice, .sellsWholeSlice, .ok:
            // Over the slice is the amount pad's own message.
            return nil
        }
    }

    /// The line under the pad saying what this sale does.
    ///
    /// Once the dust rule promotes a sale to a full exit, "We sell this much of your slice" is the
    /// wrong picture of what is about to happen — the whole slice goes, not the typed part of it.
    static func explainer(for verdict: Verdict) -> String {
        switch verdict {
        case .sellsWholeSlice:
            return "This cashes out your whole slice. The cash moves to your account balance, and you stay in the cabal with nothing in the pot."
        case .noAmount, .belowMinimum, .overSlice, .ok:
            return "We sell this much of your slice and move the cash to your account balance. You stay in the cabal."
        }
    }

    /// The line under the figure when the amount is fine but worth a word.
    static func note(for verdict: Verdict, sliceMicros: Int64) -> String? {
        switch verdict {
        case .sellsWholeSlice:
            return "We'll cash out your whole slice, \(UsdAmountFormatter.format(micros: sliceMicros))"
        case .noAmount, .belowMinimum, .overSlice, .ok:
            return nil
        }
    }
}
