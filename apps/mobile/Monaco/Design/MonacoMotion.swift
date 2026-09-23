import SwiftUI

/// The app's four named curves, plus the one bouncy curve, and nothing else.
///
/// Before this, near-identical springs were hand-rolled per component and had already drifted —
/// the button press effect ran `.spring(0.25, 0.8)` while the segmented thumb ran
/// `.spring(0.3, 0.85)`, for the same "a control responded to my finger" moment. Both are `snap`.
///
/// Nothing in the app loops except presence dots and the skeleton. No parallax, no blur
/// transitions, no hero-image morphs, and no count-ups on a money figure.
enum MonacoMotion {
    /// A control responding to a finger: a button press, a segmented thumb, a chip selecting.
    static let snap = Animation.spring(response: 0.28, dampingFraction: 0.86)

    /// Something arriving or rearranging on its own: a row inserting, a sheet settling, a face
    /// travelling into a vote slot.
    static let settle = Animation.spring(response: 0.42, dampingFraction: 0.82)

    /// A crossfade. No overshoot, so it never draws attention to itself.
    static let glide = Animation.easeOut(duration: 0.22)

    /// Used in exactly one place in the entire app: a vote crossing threshold. If a second call
    /// site appears, one of them is wrong.
    static let celebrate = Animation.spring(response: 0.50, dampingFraction: 0.60)
}

extension Animation {
    /// `nil` under Reduce Motion, so the change still happens and simply is not animated.
    ///
    /// Every animated call site reads
    /// `withAnimation(MonacoMotion.settle.reduced(reduceMotion)) { ... }` or
    /// `.animation(MonacoMotion.snap.reduced(reduceMotion), value:)`, taking `reduceMotion` from
    /// `@Environment(\.accessibilityReduceMotion)`. Forgetting the accessor is then a visible
    /// omission at the call site rather than an invisible one, which is the whole point: a curve
    /// that is only sometimes gated is a curve nobody audits.
    func reduced(_ isReduced: Bool) -> Animation? {
        isReduced ? nil : self
    }
}
