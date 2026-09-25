import MonacoCore
import SwiftUI

/// How a day-change pill reads. Members use the two very differently: a percent
/// compares one stock against another, a dollar figure answers "what did that do to
/// my money". Both come from figures already on the row, so switching costs nothing.
enum DayChangeMode: String, CaseIterable {
    case percent
    case dollars

    var next: DayChangeMode { self == .percent ? .dollars : .percent }

    /// What VoiceOver offers on the row, naming what a tap would switch *to*.
    var switchActionName: String {
        next == .dollars ? "Show day change in dollars" : "Show day change as a percent"
    }
}

/// The one storage key behind every pill, so the whole app flips together — as it
/// does on Robinhood. A per-row choice would mean a list where some rows are
/// percents and some are dollars, which is unreadable.
enum DayChangeModeStorage {
    static let key = "monaco.stocks.dayChangeMode"
}

/// The tone of a day change, read off the figure that is actually displayed.
///
/// Deliberately not `PnLTone(dollarPnl:)`: that one calls anything under half a cent
/// flat, and a +0.45% day is a rise even though the ratio "0.0045" rounds to $0.00.
extension PnLTone {
    init(change24h: String?) {
        let formatted = PercentReturnFormatter.format(change24h)
        if formatted == "—" || formatted == "0.0%" {
            self = .flat
        } else if formatted.hasPrefix("\u{2212}") {
            self = .loss
        } else {
            self = .profit
        }
    }

    /// The tone of a row's sparkline, which is not always the tone of its pill.
    ///
    /// The line is Pyth's underlying equity and the pill is Jupiter's price for the
    /// xStock token, and those two genuinely diverge. When the row knows they are
    /// different instruments it tints the line from the line, so the colour is
    /// about the picture on screen rather than about a number measured somewhere
    /// else.
    init(sparkTint: SparkTint, change24h: String?) {
        switch sparkTint {
        case .reportedDayChange:
            self.init(change24h: change24h)
        case let .series(rising, flat):
            self = flat ? .flat : (rising ? .profit : .loss)
        }
    }
}

/// The filled capsule on the right of a market row: "+1.24%", or "+$2.84" once
/// tapped. Tapping toggles every pill in the app.
struct DayChangePill: View {
    let change24h: String?
    let priceUsdcMicros: Int64?
    var style: MoneyStyle = .caption

    @AppStorage(DayChangeModeStorage.key) private var storedMode = DayChangeMode.percent.rawValue

    private var mode: DayChangeMode { DayChangeMode(rawValue: storedMode) ?? .percent }
    private var tone: PnLTone { PnLTone(change24h: change24h) }

    private var percentText: String { DayChangeFigures.percentText(change24h: change24h) }

    /// Dollars when the member asked for them and they can be worked out; the
    /// percent otherwise. A row with a change but no price keeps its percent rather
    /// than blanking when the mode flips.
    private var label: String {
        guard mode == .dollars,
              let dollars = DayChangeFigures.dollarText(change24h: change24h, priceUsdcMicros: priceUsdcMicros)
        else { return percentText }
        return dollars
    }

    private var isReadable: Bool { percentText != "—" }

    /// How far the hit area reaches past the drawn capsule on each side.
    ///
    /// The capsule is about 22pt tall, which is the right size to read and the
    /// wrong size to hit — under the HIG's 44pt minimum, and the UI test that opens
    /// a stock had to aim a quarter of the way into the row to miss it. The padding
    /// is applied for hit testing and then taken straight back off, so the target
    /// grows without the row growing with it.
    private static let tapTargetPadding: CGFloat = 11

    var body: some View {
        Text(label)
            .moneyFont(style, weight: .semibold)
            .foregroundStyle(isReadable ? tone.washColor : MonacoTheme.muted)
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .padding(.horizontal, style == .caption ? 8 : 12)
            .padding(.vertical, style == .caption ? 4 : 6)
            .background(Capsule().fill(isReadable ? tone.wash : MonacoTheme.surfaceSunken))
            .padding(DayChangePill.tapTargetPadding)
            .contentShape(Rectangle())
            // A plain tap gesture inside a row-sized Button is swallowed by the row.
            // A high-priority one is not, which is what lets the pill be its own
            // control without the row being taken apart into two hit areas.
            .highPriorityGesture(TapGesture().onEnded { toggle() })
            .padding(-DayChangePill.tapTargetPadding)
            .accessibilityLabel("Day change")
            .accessibilityValue(DayChangeSpeech.value(change24h: change24h, priceUsdcMicros: priceUsdcMicros, mode: mode))
    }

    private func toggle() {
        guard isReadable else { return }
        Haptics.selection()
        storedMode = mode.next.rawValue
    }
}

/// What VoiceOver reads for a day change.
enum DayChangeSpeech {
    static func value(change24h: String?, priceUsdcMicros: Int64?, mode: DayChangeMode) -> String {
        let percent = PnLSpeech.percent(PercentReturnFormatter.format(change24h))
        guard mode == .dollars,
              let dollars = DayChangeFigures.dollarDelta(change24h: change24h, priceUsdcMicros: priceUsdcMicros)
        else { return percent }
        return PnLSpeech.dollars(dollars)
    }
}

/// A market row: logo, ticker over an optional second line, the day's shape, the
/// price and a day-change pill.
///
/// The ticker is the label, the way a watchlist reads. The company name is what the
/// detail screen is for — it sits under the price there, with the ticker in the nav
/// bar — so a row never spends its one line on "Apple" when "AAPL" is what a member
/// scans for and what every other trading app has taught them to scan for.
///
/// One row for the Stocks tab, the cabal's holdings and anything else that lists a
/// stock, so the three cannot drift. It is built on `MonacoRowLayout` rather than
/// on `MonacoRow` because of the sparkline column, and follows the same rules: the
/// labels keep a floor and truncate, the figures shrink, and at accessibility text
/// sizes the whole thing stacks and the sparkline steps aside — it is decoration,
/// and the pill beside it says the same thing in words.
struct StockListRow: View {
    let row: MarketRowData
    /// The exchange session this page was priced in. A moon next to the price is
    /// how a list says "this print is from outside the cash session".
    var afterHours = false
    var isLast = false

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var titleWidthFloor = MonacoRowLayout.baseMinimumTitleWidth
    @AppStorage(DayChangeModeStorage.key) private var storedMode = DayChangeMode.percent.rawValue

    private var asset: MarketAssetDTO { row.asset }
    private var layout: MonacoRowLayout {
        MonacoRowLayout(dynamicTypeSize: dynamicTypeSize, scaledTitleWidthFloor: titleWidthFloor)
    }

    private var title: String {
        AssetSymbolFormatter.display(asset.symbol)
    }

    /// Only the rows with something to say get a second line ("2 cabals · your slice").
    /// The ticker used to be the fallback here; it is the title now, and repeating it
    /// underneath itself would be noise.
    private var subtitle: String? {
        row.subtitle
    }

    var body: some View {
        content
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.vertical, 8)
            .frame(minHeight: 64)
            .contentShape(Rectangle())
            .overlay(alignment: .bottom) {
                if !isLast {
                    Rectangle()
                        .fill(MonacoTheme.hairline)
                        .frame(height: 1)
                        // Derived from this row's own 40pt mark, so the separator
                        // starts where the text does — as it does in every other
                        // list in the app.
                        .padding(.leading, layout.separatorLeadingInset(markSize: StockListRow.markSize))
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityHint(row.accessoryLabel ?? "")
            .accessibilityAction(named: Text(currentMode.switchActionName)) {
                storedMode = currentMode.next.rawValue
            }
    }

    private var currentMode: DayChangeMode { DayChangeMode(rawValue: storedMode) ?? .percent }

    @ViewBuilder
    private var content: some View {
        if layout.isStacked {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                HStack(spacing: MonacoTheme.Space.sm) {
                    mark
                    labels
                }
                HStack(spacing: MonacoTheme.Space.s) {
                    price
                    DayChangePill(change24h: asset.change24h, priceUsdcMicros: asset.priceUsdcMicros)
                }
            }
        } else {
            HStack(spacing: MonacoTheme.Space.sm) {
                mark
                labels.frame(minWidth: layout.minimumTitleWidth, alignment: .leading)
                if let spark = row.spark {
                    Sparkline(
                        series: spark,
                        tone: PnLTone(sparkTint: row.sparkTint, change24h: asset.change24h)
                    )
                    .padding(.horizontal, MonacoTheme.Space.xs)
                }
                VStack(alignment: .trailing, spacing: 3) {
                    price
                    DayChangePill(change24h: asset.change24h, priceUsdcMicros: asset.priceUsdcMicros)
                }
                .layoutPriority(1)
            }
        }
    }

    /// The mark on a market row. Smaller than `MonacoRow`'s 44pt because a market
    /// row carries a sparkline column as well, and the separator inset is derived
    /// from this rather than assumed.
    static let markSize: CGFloat = 46

    private var mark: some View {
        StockMark(symbol: asset.symbol, size: StockListRow.markSize, logoURL: asset.logoURL)
            .frame(width: StockListRow.markSize, height: StockListRow.markSize)
    }

    private var labels: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(layout.titleLineLimit)
                .truncationMode(.tail)
            if let subtitle {
                Text(subtitle)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(layout.subtitleLineLimit)
                    .truncationMode(.tail)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private var price: some View {
        HStack(spacing: 4) {
            if afterHours {
                Image(systemName: "moon.fill")
                    .font(.caption2)
                    .foregroundStyle(MonacoTheme.warning)
                    .accessibilityLabel("After hours")
            }
            if let micros = asset.priceUsdcMicros {
                MoneyText(micros: micros, style: .row)
            } else {
                Text("—")
                    .moneyFont(.row)
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
    }
}

/// A Webull-style mover card for a horizontal strip: logo, ticker, the day's shape
/// and the move. Narrow on purpose — a strip is scanned, not read.
///
/// The card scales with the text inside it. It used to be pinned to a flat 148pt
/// while its ticker, price and pill all grew, so at large text sizes the price and
/// the pill overflowed the card they sit in. `StockListRow` had the stacked layout
/// for this; the card had nothing.
struct StockMoverCard: View {
    let row: MarketRowData

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var cardWidth = StockMoverCard.baseWidth
    @ScaledMetric(relativeTo: .body) private var sparkWidth = StockMoverCard.baseSparkWidth
    @ScaledMetric(relativeTo: .body) private var sparkHeight = StockMoverCard.baseSparkHeight
    @AppStorage(DayChangeModeStorage.key) private var storedMode = DayChangeMode.percent.rawValue

    static let baseWidth: CGFloat = 148
    static let baseSparkWidth: CGFloat = 104
    static let baseSparkHeight: CGFloat = 30
    /// Past this the card would be wider than a phone, and a horizontal strip of
    /// one-and-a-bit cards is not a scanning affordance any more. The strip hides
    /// itself at accessibility sizes instead — see `StockMoverStrip.isAvailable`.
    static let maximumWidth: CGFloat = 260

    private var asset: MarketAssetDTO { row.asset }
    private var currentMode: DayChangeMode { DayChangeMode(rawValue: storedMode) ?? .percent }

    private var width: CGFloat { min(cardWidth, StockMoverCard.maximumWidth) }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            HStack(spacing: MonacoTheme.Space.s) {
                StockMark(symbol: asset.symbol, size: 28, logoURL: asset.logoURL)
                Text(AssetSymbolFormatter.display(asset.symbol))
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
            }
            if let spark = row.spark {
                Sparkline(
                    series: spark,
                    tone: PnLTone(sparkTint: row.sparkTint, change24h: asset.change24h),
                    width: min(sparkWidth, StockMoverCard.maximumWidth - 2 * MonacoTheme.Space.sm),
                    height: sparkHeight
                )
            } else {
                // Keeps every card the same height, so a symbol without history does
                // not make the strip ragged.
                Color.clear.frame(height: sparkHeight)
            }
            HStack(spacing: MonacoTheme.Space.s) {
                if let micros = asset.priceUsdcMicros {
                    MoneyText(micros: micros, style: .caption)
                        .lineLimit(1)
                        .minimumScaleFactor(0.7)
                }
                Spacer(minLength: 0)
                DayChangePill(change24h: asset.change24h, priceUsdcMicros: asset.priceUsdcMicros)
                    .layoutPriority(1)
            }
        }
        .padding(MonacoTheme.Space.sm)
        .frame(width: width, alignment: .leading)
        .background(MonacoTheme.surface)
        .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.tile, style: .continuous))
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        // VoiceOver can hear the day change on this surface; without this it could
        // not switch it, which `StockListRow` has offered all along.
        .accessibilityAction(named: Text(currentMode.switchActionName)) {
            storedMode = currentMode.next.rawValue
        }
    }
}

/// Whether the Top movers strip is worth drawing at the current text size.
///
/// It is a scanning affordance: several cards at a glance, swiped sideways. At
/// accessibility sizes one card fills the screen, so the strip stops being a way
/// to scan and becomes a second, worse copy of the list below it. Hiding it is the
/// honest answer — nothing is lost, because every mover is in Popular too.
enum StockMoverStrip {
    static func isAvailable(at dynamicTypeSize: DynamicTypeSize) -> Bool {
        !dynamicTypeSize.isAccessibilitySize
    }
}
