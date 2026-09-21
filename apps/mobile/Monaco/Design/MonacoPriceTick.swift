import SwiftUI

/// A price that just moved under a poll.
///
/// The sequence number is what makes this an event rather than a state: two ticks
/// up in a row are two flashes, and a view that re-renders for any other reason does
/// not flash at all.
struct MonacoPriceTick: Equatable {
    let sequence: Int
    let isUp: Bool
}

/// The shape of the wash over time: up to `peak` almost at once, then back to
/// nothing over a quarter of a second.
///
/// It is a keyframe track rather than a pair of state writes. SwiftUI commits one
/// transaction per update, so setting an intensity and then animating it back to
/// zero inside the same synchronous block is a net change of 0 → 0: there is
/// nothing to interpolate and the wash never renders at all. This is the same
/// pitfall `opacityLoop` documents in `MonacoFeedback`, and a track is the fix for
/// both — the animation is a function of time, not a difference between two states.
///
/// Exposed so a test can read the curve the view actually plays.
enum MonacoPriceTickFlash {
    /// Deliberately a wash rather than the full colour the sketch asked for: at full
    /// strength the label inside a change pill stops clearing contrast for the
    /// 250ms it matters most, and a figure you cannot read is not a cue.
    static let peak: Double = 0.35
    static let rise: TimeInterval = 0.05
    static let fade: TimeInterval = 0.25

    /// Two linear tracks with an eased fall, not a `CubicKeyframe`: a cubic infers
    /// its tangents from the keyframes around it, and with a ramp up and a single
    /// frame down it overshoots the peak by half again — a wash asked for at 0.35
    /// rendered at 0.54, dark enough to swallow the label it sits behind.
    @KeyframesBuilder<Double>
    static func keyframes(_ start: Double) -> some Keyframes<Double> {
        LinearKeyframe(peak, duration: rise)
        LinearKeyframe(0, duration: fade, timingCurve: .easeOut)
    }
}

extension View {
    /// Washes this view in the profit or loss colour for a quarter of a second when
    /// `tick` changes, then lets it fade back.
    ///
    /// It is the quietest way to say "that number is not the one you were reading a
    /// moment ago".
    ///
    /// Nil never flashes, which is what a first load passes — nothing changed yet.
    /// Reduce Motion skips it entirely; the figure itself has already updated.
    func priceTickFlash(_ tick: MonacoPriceTick?) -> some View {
        priceTickFlash(tick, in: Capsule())
    }

    /// The wash takes the shape it sits behind. A pill gets a capsule; bare text
    /// gets a rounded rectangle with a little room around it, because a capsule
    /// hugging the glyphs reads as a stray control rather than as a highlight.
    func priceTickFlash<S: Shape>(_ tick: MonacoPriceTick?, in shape: S, expand: CGFloat = 0) -> some View {
        modifier(PriceTickFlash(tick: tick, shape: shape, expand: expand))
    }
}

private struct PriceTickFlash<S: Shape>: ViewModifier {
    let tick: MonacoPriceTick?
    let shape: S
    let expand: CGFloat

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content.background { wash }
    }

    @ViewBuilder
    private var wash: some View {
        if !reduceMotion {
            shape
                .fill(flashColor)
                .padding(-expand)
                .keyframeAnimator(initialValue: 0.0, trigger: tick) { view, intensity in
                    view.opacity(intensity)
                } keyframes: { start in
                    MonacoPriceTickFlash.keyframes(start)
                }
                .allowsHitTesting(false)
        }
    }

    /// The tick in the body *is* the fresh one: the trigger fires from the same
    /// update that delivered it, so there is no stale value to read here.
    private var flashColor: Color {
        (tick?.isUp ?? true) ? MonacoTheme.profitVivid : MonacoTheme.lossVivid
    }
}
