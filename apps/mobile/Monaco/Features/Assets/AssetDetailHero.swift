import MonacoCore
import SwiftUI

/// The top of the stock screen: the price, what it did, and what the exchange is doing.
///
/// The price is a `MoneyText`, so a poll rolls the digits instead of swapping the
/// label, and the change pill flashes when a poll actually moved the number. While a
/// finger is on the curve both figures belong to the sample under it — that is the
/// whole point of the scrub, and it is why the hero takes them as values rather than
/// reading the live price itself.
struct AssetDetailHero: View {
    let displayName: String
    let priceUsdcMicros: Int64?
    let move: AssetDetailModel.Move?
    let isScrubbing: Bool
    /// Raised only by a poll that changed the price — never by the first load.
    let tick: MonacoPriceTick?
    let session: MarketSessionChipCopy?

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text(displayName)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .accessibilityIdentifier("asset-detail-name")

            price
                .accessibilityIdentifier("asset-detail-price")

            if let move {
                changeRow(move)
            }

            if let session {
                sessionChip(session)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private var price: some View {
        if let priceUsdcMicros {
            MoneyText(micros: priceUsdcMicros, style: .hero)
        } else {
            Text("—")
                .moneyFont(.hero)
                .foregroundStyle(MonacoTheme.muted)
        }
    }

    /// "▲ $5.50 · 2.4%  Past day · AAPL", or the scrubbed sample's own time in place
    /// of the period. Dollars come from the curve, so they are only shown when there
    /// is one — and so is the instrument tag, because a figure folded from the curve
    /// is the underlying equity's while the price above it is the token's. Two
    /// numbers that cannot be reconciled by subtraction have to say which is which.
    private func changeRow(_ move: AssetDetailModel.Move) -> some View {
        HStack(spacing: MonacoTheme.Space.s) {
            Group {
                if let dollars = move.dollars {
                    PnLBadge(dollarPnl: dollars, percentReturn: move.ratio)
                        .priceTickFlash(tick)
                } else {
                    // Bare text, no pill: a capsule wash here would draw a stray
                    // capsule hugging the glyphs, so the flash takes a rounded
                    // rectangle with a little room around it instead.
                    PercentText(percentReturn: move.ratio, style: .row)
                        .priceTickFlash(
                            tick,
                            in: RoundedRectangle(cornerRadius: 8, style: .continuous),
                            expand: 5
                        )
                }
            }

            Text(periodLabel(move))
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(isScrubbing ? MonacoTheme.ink : MonacoTheme.muted)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spokenChange(move))
        .accessibilityIdentifier("asset-detail-move")
    }

    /// "Past day" on its own, or "Past day · AAPL" when the figure is measured on
    /// another instrument than the price above it. The chart's own caption under the
    /// curve spells the same fact out in full; here it has to stay one line.
    private func periodLabel(_ move: AssetDetailModel.Move) -> String {
        guard let symbol = move.basisSymbol else { return move.label }
        return "\(move.label) · \(symbol)"
    }

    /// VoiceOver gets the long form: "Up $5.50, 2.4%, Past day. AAPL on its home
    /// exchange." — a combined label would read the "·" and drop the caption.
    private func spokenChange(_ move: AssetDetailModel.Move) -> String {
        var sentence = move.dollars.map { PnLSpeech.badge(dollarPnl: $0, percentReturn: move.ratio) }
            ?? PnLSpeech.percent(PercentReturnFormatter.format(move.ratio))
        sentence += ", \(move.label)"
        if let caption = move.basisCaption { sentence += ". \(caption)." }
        return sentence
    }

    /// The exchange's state, and — whenever it is shut — the sentence this product
    /// exists for, on its own line so it is read rather than squeezed into a capsule.
    private func sessionChip(_ session: MarketSessionChipCopy) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            HStack(spacing: 6) {
                Image(systemName: session.isLive ? "circle.fill" : "moon.fill")
                    .font(.system(size: 8))
                    .foregroundStyle(session.isLive ? MonacoTheme.profit : MonacoTheme.warning)
                Text(session.title)
                    .font(MonacoTheme.Typo.caption.weight(.semibold))
                    .foregroundStyle(MonacoTheme.ink)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(Capsule().fill(MonacoTheme.surfaceSunken))

            if let detail = session.detail {
                Text(detail)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(session.spoken)
        .accessibilityIdentifier("asset-detail-session")
    }
}

/// The hero's shape while both calls are still in flight, so the screen does not jump
/// when the numbers land.
struct AssetDetailHeroSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            SkeletonBlock(width: 120, height: 14)
            SkeletonBlock(width: 200, height: 40, radius: 12)
            SkeletonBlock(width: 150, height: 20, radius: 10)
            SkeletonBlock(width: 110, height: 24, radius: 12)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading stock")
        .accessibilityIdentifier("asset-detail-hero-skeleton")
    }
}
