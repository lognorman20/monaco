import MonacoCore
import SwiftUI

/// "Stock vs token": Apple on its home exchange against AAPLx on Solana, how far
/// apart they are, and how sure each feed is of its own number.
///
/// This is the screen's argument for existing. Every other broker can show a price;
/// this card shows the price the exchange stopped printing at, the price that kept
/// moving after the bell, and the gap between them — with Pyth's own confidence
/// band under each figure.
///
/// All of the wording and every rule about what may be shown lives in
/// `StockVsTokenCard` in MonacoCore, next to its tests. Nothing here decides what is
/// true; it decides what it looks like.
struct StockVsTokenCardView: View {
    let card: StockVsTokenCard

    var body: some View {
        AssetDetailCard(title: "Stock vs token", identifier: "asset-detail-stock-vs-token") {
            VStack(alignment: .leading, spacing: 0) {
                // The two legs are a ruled table of two lines, closed by a rule, and the
                // verdict — the premium and what it means — sits under it.
                LegRow(leg: card.equity)
                    .padding(.bottom, MonacoTheme.Space.sm)
                AssetCardDivider()
                LegRow(leg: card.token)
                    .padding(.vertical, MonacoTheme.Space.sm)
                AssetCardDivider()
                if let premium = card.premium {
                    PremiumRow(premium: premium)
                        .padding(.top, MonacoTheme.Space.sm)
                }
                Text(card.footnote)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.top, MonacoTheme.Space.sm)
                    .accessibilityIdentifier("asset-stock-vs-token-footnote")
            }
        }
    }
}

/// One side of the comparison: who and where, then the number and how fresh it is.
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
        .accessibilityIdentifier("asset-stock-vs-token-leg-\(leg.ticker.lowercased())")
    }

    private var identity: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(leg.ticker)
                .font(MonacoTheme.Typo.ticker)
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
                MoneyText(micros: price, style: .row, voice: .market)
                freshnessLine
                if let confidence = leg.confidence {
                    // Pyth's own interval. It sits under the price, in the quiet
                    // colour, because it qualifies the number above it.
                    Text(confidence)
                        .font(MonacoTheme.Typo.dataMicro)
                        .foregroundStyle(MonacoTheme.tertiaryText)
                }
            } else {
                // No number at all. A dash would read as zero; the reason reads as
                // the reason.
                Text("—")
                    .moneyFont(.row, voice: .market)
                    .foregroundStyle(MonacoTheme.disabledLabel)
                Text(leg.unavailableReason ?? "No price")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.warning)
                    .multilineTextAlignment(alignment == .trailing ? .trailing : .leading)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    /// The dot carries the state — lit while the feed is printing, a moon once it has
    /// stopped — and the words stay quiet. "Live" used to be set in profit green, which
    /// on this screen means a price went up; a feed being on is not a gain. The time is
    /// a timestamp, so it sets in the market's voice like every other one.
    private var freshnessLine: some View {
        HStack(spacing: 5) {
            MarketSessionDot(isLive: leg.isLive)
            Text(leg.freshness)
                .font(MonacoTheme.Typo.stamp)
                .foregroundStyle(MonacoTheme.muted)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
        }
    }
}

/// The gap between the two, as a wash chip and the sentence that reads it out.
private struct PremiumRow: View {
    let premium: StockVsTokenCard.Premium

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Text(premium.label)
                .font(MonacoTheme.Typo.dataStrong)
                .foregroundStyle(tint)
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(Capsule().fill(wash))
            Text(premium.caption)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(premium.caption)
        .accessibilityIdentifier("asset-stock-vs-token-premium")
    }

    /// A premium is not profit. It is priced above or below the stock, and green
    /// here would read as a gain the member has made — which they have not.
    /// The tokens are the brand's, deliberately: this is information, not money.
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
