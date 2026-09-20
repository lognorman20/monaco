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

    var body: some View {
        Text(label)
            .moneyFont(style, weight: .semibold)
            .foregroundStyle(isReadable ? tone.washColor : MonacoTheme.muted)
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .padding(.horizontal, style == .caption ? 8 : 12)
            .padding(.vertical, style == .caption ? 4 : 6)
            .background(Capsule().fill(isReadable ? tone.wash : MonacoTheme.surfaceSunken))
            .contentShape(Capsule())
            // A plain tap gesture inside a row-sized Button is swallowed by the row.
            // A high-priority one is not, which is what lets the pill be its own
            // control without the row being taken apart into two hit areas.
            .highPriorityGesture(TapGesture().onEnded { toggle() })
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

/// A market row: logo, name over a second line, the day's shape, the price and a
/// day-change pill.
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
        ProposeStock.displayName(symbol: asset.symbol, catalogName: asset.name)
    }

    private var subtitle: String {
        row.subtitle ?? AssetSymbolFormatter.display(asset.symbol)
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
                        .padding(.leading, layout.separatorLeadingInset)
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
                    Sparkline(series: spark, tone: PnLTone(change24h: asset.change24h))
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

    private var mark: some View {
        StockMark(symbol: asset.symbol, size: 40, logoURL: asset.logoURL)
            .frame(width: 40, height: 40)
    }

    private var labels: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(layout.titleLineLimit)
                .truncationMode(.tail)
            Text(subtitle)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .lineLimit(layout.subtitleLineLimit)
                .truncationMode(.tail)
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
struct StockMoverCard: View {
    let row: MarketRowData

    private var asset: MarketAssetDTO { row.asset }

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
                Sparkline(series: spark, tone: PnLTone(change24h: asset.change24h), width: 104, height: 30)
            } else {
                // Keeps every card the same height, so a symbol without history does
                // not make the strip ragged.
                Color.clear.frame(height: 30)
            }
            HStack(spacing: MonacoTheme.Space.s) {
                if let micros = asset.priceUsdcMicros {
                    MoneyText(micros: micros, style: .caption)
                }
                Spacer(minLength: 0)
                DayChangePill(change24h: asset.change24h, priceUsdcMicros: asset.priceUsdcMicros)
            }
        }
        .padding(MonacoTheme.Space.sm)
        .frame(width: 148, alignment: .leading)
        .background(MonacoTheme.surface)
        .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.tile, style: .continuous))
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}
