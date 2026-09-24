import MonacoCore
import SwiftUI

/// The market's state, and — whenever the exchange is shut — the sentence this
/// product exists for: "After hours · Trading 24/7 on Solana".
///
/// One chip, used by the hero and by the stock-vs-token card, because two copies of
/// it would drift: the hero's would say "After hours" and the card's would say
/// "Closed" on the same screen at the same moment.
///
/// The copy itself is `MarketSessionCopy` in MonacoCore, which is host-tested. This
/// file is only the shape.
struct MarketSessionChip: View {
    let session: MarketSessionChipCopy
    /// `.stacked` puts the second line under the capsule (the hero). `.inline` folds
    /// it into the capsule with a middot, for a card header where a second line
    /// would push the numbers down.
    var layout: Layout = .stacked

    enum Layout {
        case stacked
        case inline
    }

    var body: some View {
        Group {
            switch layout {
            case .stacked: stacked
            case .inline: inline
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(session.spoken)
    }

    private var stacked: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            capsule(title: session.title)
            if let detail = session.detail {
                Text(detail)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    // The 24/7 line is the argument, not a footnote: let it wrap
                    // rather than truncating at the accessibility text sizes.
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var inline: some View {
        capsule(title: session.detail.map { "\(session.title) · \($0)" } ?? session.title)
    }

    private func capsule(title: String) -> some View {
        HStack(spacing: 6) {
            MarketSessionDot(isLive: session.isLive)
            Text(title)
                .font(MonacoTheme.Typo.caption.weight(.semibold))
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(Capsule().fill(MonacoTheme.surfaceSunken))
    }
}

/// The glyph in front of a session chip: a lit dot while the exchange is printing, a
/// moon once it is shut.
///
/// The dot breathes only while the market is live, and only when the reader has not
/// asked for less motion. A pulse on a closed market would claim a liveness that is
/// the opposite of what the chip says.
struct MarketSessionDot: View {
    let isLive: Bool

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var isPulsing = false

    var body: some View {
        Image(systemName: isLive ? "circle.fill" : "moon.fill")
            .font(.system(size: 8))
            .foregroundStyle(isLive ? MonacoTheme.profit : MonacoTheme.warning)
            .opacity(isLive && isPulsing ? 0.45 : 1)
            .animation(
                isLive && !reduceMotion
                    ? .easeInOut(duration: 1.2).repeatForever(autoreverses: true)
                    : nil,
                value: isPulsing
            )
            .onAppear { if isLive && !reduceMotion { isPulsing = true } }
            .onChange(of: isLive) { _, live in
                isPulsing = live && !reduceMotion
            }
            .accessibilityHidden(true)
    }
}
