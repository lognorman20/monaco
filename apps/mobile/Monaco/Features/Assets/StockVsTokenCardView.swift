import MonacoCore
import SwiftUI

/// "Stock vs token": what AAPLc is changing hands for on Base, what its mark says it
/// is worth, and what the share costs on its home exchange.
///
/// This is the screen's argument for existing. Every other broker can show a price;
/// this card shows the price the exchange stopped printing at, the price that kept
/// moving after the bell, and the gap between the token and its own mark.
///
/// All of the wording and every rule about what may be shown lives in
/// `StockVsTokenCard` in MonacoCore, next to its tests. Nothing here decides what is
/// true; it decides what it looks like. In particular the premium is the server's
/// figure, measured between the two per-token legs — this view never derives one.
struct StockVsTokenCardView: View {
    let card: StockVsTokenCard

    var body: some View {
        AssetDetailCard(title: "Stock vs token", identifier: "asset-detail-stock-vs-token") {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                LegRow(leg: card.token)
                AssetCardDivider()
                LegRow(leg: card.mark)
                if let premium = card.premium {
                    PremiumRow(premium: premium)
                }
                if let spread = card.spread {
                    Text(spread)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                        .accessibilityIdentifier("asset-stock-vs-token-spread")
                }
                // The share is a reference, not a third comparable price, so it sits
                // below the divider that closes the comparison rather than inside it.
                AssetCardDivider()
                LegRow(leg: card.equity)
                Text(card.footnote)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("asset-stock-vs-token-footnote")
            }
        }
    }
}

/// One line of the comparison: who and where, then the number and how fresh it is.
private struct LegRow: View {
    let leg: StockVsTokenCard.Leg

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// Above the accessibility sizes the two columns stop fitting side by side, and
    /// a price scaled to 60% to keep them there is worse than a stacked row.
    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        Group {
            if isStacked {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    identity
                    figure(alignment: .leading)
                }
            } else {
                HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                    identity
                    Spacer(minLength: MonacoTheme.Space.s)
                    figure(alignment: .trailing)
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(leg.spoken)
        // Keyed on the leg's role, not on its ticker: the token and its mark share a
        // ticker, and a test asking for "aaplc" would find whichever came first.
        .accessibilityIdentifier("asset-stock-vs-token-leg-\(leg.id)")
    }

    private var identity: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(leg.ticker)
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
            Text(leg.venue)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
            Text(leg.source)
                .font(MonacoTheme.Typo.micro)
                .foregroundStyle(MonacoTheme.tertiaryText)
        }
    }

    @ViewBuilder
    private func figure(alignment: HorizontalAlignment) -> some View {
        VStack(alignment: alignment, spacing: 2) {
            if let price = leg.priceUsdcMicros {
                MoneyText(micros: price, style: .row)
                freshnessLine
                if let confidence = leg.confidence {
                    // Pyth's own interval. It sits under the price, in the quiet
                    // colour, because it qualifies the number above it.
                    Text(confidence)
                        .font(MonacoTheme.Typo.micro)
                        .foregroundStyle(MonacoTheme.tertiaryText)
                        .monospacedDigit()
                }
            } else {
                // No number at all. A dash would read as zero; the reason reads as
                // the reason.
                Text("—")
                    .moneyFont(.row)
                    .foregroundStyle(MonacoTheme.disabledLabel)
                Text(leg.unavailableReason ?? "No price")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.warning)
                    .multilineTextAlignment(alignment == .trailing ? .trailing : .leading)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var freshnessLine: some View {
        HStack(spacing: 5) {
            MarketSessionDot(isLive: leg.isLive)
            Text(leg.freshness)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(leg.isLive ? MonacoTheme.profit : MonacoTheme.muted)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
        }
    }
}

/// The gap between the token and its mark, as a pill and a sentence.
private struct PremiumRow: View {
    let premium: StockVsTokenCard.Premium

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Text(premium.label)
                .moneyFont(.row, weight: .semibold)
                .foregroundStyle(tint)
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(Capsule().fill(wash))
            Text(premium.caption)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.top, MonacoTheme.Space.xs)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(premium.caption)
        .accessibilityIdentifier("asset-stock-vs-token-premium")
    }

    /// A premium is not profit. It is priced above or below the mark, and green here
    /// would read as a gain the member has made — which they have not. The tokens are
    /// the brand's, deliberately: this is information, not money.
    private var tint: Color {
        switch premium.direction {
        case .inline: return MonacoTheme.muted
        case .above, .below: return MonacoTheme.brandOnWash
        }
    }

    private var wash: Color {
        premium.direction == .inline ? MonacoTheme.surfaceSunken : MonacoTheme.brandWash
    }
}

/// A live dot, or a moon once the feed behind the line has stopped printing.
///
/// The dot breathes only while the line is live, and only when the reader has not
/// asked for less motion. A pulse on a frozen price would claim a liveness that is
/// the opposite of what the line beside it says.
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
