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
    /// The ticker, in the market's voice, beside the company's name.
    var ticker: String = ""
    let priceUsdcMicros: Int64?
    let move: AssetDetailModel.Move?
    let isScrubbing: Bool
    /// Raised only by a poll that changed the price — never by the first load.
    let tick: MonacoPriceTick?
    let session: MarketSessionChipCopy?

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            // The company in the brand's voice, the ticker in the market's: this is the one
            // screen where the name leads, because the tap was the question "what is this?".
            HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                Text(displayName)
                    .font(MonacoTheme.Typo.title)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                    .accessibilityIdentifier("asset-detail-name")
                if !ticker.isEmpty, ticker != displayName {
                    Text(ticker)
                        .font(MonacoTheme.Typo.data)
                        .foregroundStyle(MonacoTheme.muted)
                        .accessibilityHidden(true)
                }
            }

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

    /// A quote, so it sets in the market's voice — the one hero figure in the app that does.
    @ViewBuilder
    private var price: some View {
        if let priceUsdcMicros {
            MoneyText(micros: priceUsdcMicros, style: .hero, voice: .market)
        } else {
            Text("—")
                .moneyFont(.hero, voice: .market)
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
                .font(isScrubbing ? MonacoTheme.Typo.stamp : MonacoTheme.Typo.caption)
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
    ///
    /// The chip itself is `MarketSessionChip`, shared with the stock-vs-token card.
    /// Two copies of it drift: the same screen would say "After hours" in one place
    /// and "Closed" in the other at the same moment.
    private func sessionChip(_ session: MarketSessionChipCopy) -> some View {
        MarketSessionChip(session: session)
            .accessibilityIdentifier("asset-detail-session")
    }
}

/// The hero's shape while both calls are still in flight, so the screen does not jump
/// when the numbers land.
///
/// Each block is the thing it stands in for: text-height bars for the name, the ticker
/// and the captions, the price's own height, and capsules where the change badge and
/// the session chip will be — not four rounded slabs of roughly the right width.
struct AssetDetailHeroSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            // "Apple  AAPL"
            HStack(alignment: .bottom, spacing: MonacoTheme.Space.s) {
                SkeletonBlock(width: 104, height: 22, radius: 4)
                SkeletonBlock(width: 44, height: 13, radius: 3)
                    .padding(.bottom, 2)
            }
            .padding(.vertical, 5)
            // "$232.05"
            SkeletonBlock(width: 196, height: 36, radius: 6)
                .padding(.vertical, 8)
            // "▲ $6.59 · 2.9%  Past day · AAPL"
            HStack(spacing: MonacoTheme.Space.s) {
                SkeletonBlock(width: 118, height: 26, radius: 13)
                SkeletonBlock(width: 96, height: 12, radius: 3)
            }
            // "Market open" and the line under it
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                SkeletonBlock(width: 124, height: 28, radius: 14)
                SkeletonBlock(width: 104, height: 12, radius: 3)
                    .padding(.vertical, 3)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading stock")
        .accessibilityIdentifier("asset-detail-hero-skeleton")
    }
}
