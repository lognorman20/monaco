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

extension View {
    /// Washes this view in the profit or loss colour for a quarter of a second when
    /// `tick` changes, then lets it fade back.
    ///
    /// It is the quietest way to say "that number is not the one you were reading a
    /// moment ago". Deliberately a wash rather than the full colour the sketch asked
    /// for: at full strength the label inside a change pill stops clearing contrast
    /// for the 250ms it matters most, and a figure you cannot read is not a cue.
    ///
    /// Nil never flashes, which is what a first load passes — nothing changed yet.
    /// Reduce Motion skips it entirely; the figure itself has already updated.
    func priceTickFlash(_ tick: MonacoPriceTick?) -> some View {
        modifier(PriceTickFlash(tick: tick))
    }
}

private struct PriceTickFlash: ViewModifier {
    let tick: MonacoPriceTick?

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var intensity: Double = 0

    func body(content: Content) -> some View {
        content
            .background {
                Capsule().fill(flashColor.opacity(intensity))
            }
            .onChange(of: tick) { _, fresh in flash(fresh) }
    }

    private var flashColor: Color {
        (tick?.isUp ?? true) ? MonacoTheme.profitVivid : MonacoTheme.lossVivid
    }

    private func flash(_ tick: MonacoPriceTick?) {
        guard tick != nil, !reduceMotion else { return }
        intensity = 0.35
        withAnimation(.easeOut(duration: 0.25)) { intensity = 0 }
    }
}
