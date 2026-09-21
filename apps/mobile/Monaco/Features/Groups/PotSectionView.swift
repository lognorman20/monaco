import MonacoCore
import SwiftUI

/// Holdings: every stock the cabal owns, then its cash. The cabal account address lives in
/// the details sheet, not here.
///
/// A holding row carries the same day's shape and change pill as the Stocks tab, from the
/// same series the list routes serve, and opens the same stock screen when tapped. It is
/// the same instrument in both places, so it should not be two different objects.
struct PotSectionView: View {
    let pot: [PotRowDTO]
    var onAddMoney: () -> Void = {}
    /// Nil-op by default so previews and older call sites still compile; the cabal screen
    /// passes a push into the stock.
    var onOpenStock: (String) -> Void = { _ in }

    /// Largest position first (#214).
    private var stocks: [PotRowDTO] {
        pot.filter { !Self.isCash($0) }.sorted {
            (GroupHeroMath.decimal(from: $0.valueUsd) ?? 0) > (GroupHeroMath.decimal(from: $1.valueUsd) ?? 0)
        }
    }
    private var cash: PotRowDTO? { pot.first(where: Self.isCash) }

    private var hasCash: Bool {
        guard let cash, let value = GroupHeroMath.decimal(from: cash.valueUsd) else { return false }
        return value > 0
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            MonacoSectionHeader("Holdings")

            if stocks.isEmpty && !hasCash {
                EmptyState(
                    title: "Nothing bought yet",
                    message: "Add money, then propose the first buy.",
                    actionTitle: "Add money",
                    action: onAddMoney
                )
                .accessibilityIdentifier("pot-empty")
            } else {
                MonacoGroupedList {
                    ForEach(stocks) { row in
                        Button {
                            Haptics.selection()
                            onOpenStock(row.symbol)
                        } label: {
                            stockRow(row, isLast: row.id == stocks.last?.id && cash == nil)
                        }
                        .buttonStyle(.monacoRow)
                        .accessibilityIdentifier("pot-row-\(row.symbol)")
                    }
                    if let cash {
                        MonacoRow(title: "Cash", isLast: true) {
                            StockMark(symbol: "USDC")
                        } trailing: {
                            MoneyText(decimalString: cash.valueUsd, style: .row)
                        }
                        .accessibilityIdentifier("pot-row-\(cash.symbol)")
                    }
                }

                if stocks.isEmpty {
                    Text("Nothing bought yet. Propose the first buy.")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                        .accessibilityIdentifier("pot-nothing-bought")
                }
            }
        }
        .accessibilityIdentifier("group-holdings")
    }

    /// The cabal's own figures on the right — what the position is worth and what it has
    /// made — with the day's shape and the day's move between them and the name.
    ///
    /// The cabal's P&L is the number that belongs on this screen, so it stays where it was.
    /// The day-change pill is the market's number and reads as one: it is tinted by the
    /// day, tappable like every other pill, and never confused with the position's return.
    private func stockRow(_ row: PotRowDTO, isLast: Bool) -> some View {
        PotHoldingRow(row: row, isLast: isLast)
    }

    /// Shares from the raw token amount when present; the decimal `units` string otherwise.
    static func sharesLabel(_ row: PotRowDTO) -> String {
        if let atomics = row.tokenAmount, !atomics.isEmpty {
            return ProposalShareFormatter.sharesLabel(fromAtomics: atomics)
        }
        return "\(row.units) shares"
    }

    static func isCash(_ row: PotRowDTO) -> Bool {
        row.symbol.uppercased() == "USDC"
    }
}

/// One holding. Split out so the sparkline is reduced once per row identity rather than on
/// every layout pass of the cabal screen, which polls.
private struct PotHoldingRow: View {
    let row: PotRowDTO
    let isLast: Bool
    /// Reduced once, when the row value is made. The cabal screen polls, so a
    /// series normalised inside `body` would be redone on every tick for every
    /// holding.
    private let spark: SparklineSeries?

    init(row: PotRowDTO, isLast: Bool) {
        self.row = row
        self.isLast = isLast
        spark = SparklineSeries(usdcMicros: row.sparkUsdcMicros)
    }

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var titleWidthFloor = MonacoRowLayout.baseMinimumTitleWidth
    @AppStorage(DayChangeModeStorage.key) private var storedMode = DayChangeMode.percent.rawValue

    private var layout: MonacoRowLayout {
        MonacoRowLayout(dynamicTypeSize: dynamicTypeSize, scaledTitleWidthFloor: titleWidthFloor)
    }

    private var currentMode: DayChangeMode { DayChangeMode(rawValue: storedMode) ?? .percent }

    /// The market's day pill sits beside the value rather than under it.
    ///
    /// `MonacoRow` stacks everything it is given in the trailing column, so four
    /// things there — value, the cabal's P&L, the market's day pill and an "After
    /// hours" caption — turned a 60pt row into roughly 120pt and put the position's
    /// green P&L directly above the market's red day pill, which reads as one
    /// figure contradicting itself. The two are different measurements: the P&L is
    /// this cabal's, since it bought; the pill is the market's, today. Side by side
    /// they read as two, which is what they are.
    ///
    /// After hours becomes the same moon glyph the Stocks tab puts next to a price,
    /// rather than a line of prose. It is one signal in one shape across the app,
    /// and it costs the row no height.
    var body: some View {
        MonacoRow(
            title: AssetDisplayNames.name(forSymbol: row.symbol) ?? AssetSymbolFormatter.display(row.symbol),
            subtitle: "\(PotSectionView.sharesLabel(row)) · \(UsdAmountFormatter.format(decimalString: row.markUsd))",
            chevron: true,
            isLast: isLast
        ) {
            StockMark(symbol: AssetSymbolFormatter.display(row.symbol), logoURL: row.logoURL)
        } trailing: {
            HStack(spacing: MonacoTheme.Space.s) {
                if let spark, !layout.isStacked {
                    Sparkline(series: spark, tone: sparkTone, width: 44, height: 20)
                }
                VStack(alignment: .trailing, spacing: 2) {
                    HStack(spacing: MonacoTheme.Space.s) {
                        if row.afterHours == true {
                            Image(systemName: "moon.fill")
                                .font(.caption2)
                                .foregroundStyle(MonacoTheme.warning)
                                .accessibilityLabel("After hours")
                                .accessibilityIdentifier("pot-after-hours-\(row.symbol)")
                        }
                        MoneyText(decimalString: row.valueUsd, style: .row)
                        if row.stockDayMove != nil, !layout.isStacked {
                            DayChangePill(dayMove: row.stockDayMove, referencePriceUsdcMicros: row.dayMoveReferencePriceUsdcMicros)
                                .accessibilityIdentifier("pot-row-change-\(row.symbol)")
                        }
                    }
                    PnLText(dollarPnl: row.dollarPnl, style: .caption)
                        .accessibilityIdentifier("pot-row-pnl-\(row.symbol)")
                    // At accessibility sizes the row is stacked anyway, so the pill
                    // goes back under the value rather than being squeezed out.
                    if row.stockDayMove != nil, layout.isStacked {
                        DayChangePill(dayMove: row.stockDayMove, referencePriceUsdcMicros: row.dayMoveReferencePriceUsdcMicros)
                            .accessibilityIdentifier("pot-row-change-\(row.symbol)")
                    }
                }
            }
        }
        // VoiceOver can hear the day change on this row; without this it could not
        // switch it, which StockListRow has offered all along.
        .accessibilityAction(named: Text(currentMode.switchActionName)) {
            storedMode = currentMode.next.rawValue
        }
    }

    /// The same decision the Stocks tab's rows make, from the same shared type:
    /// when the drawn series and the reported change are different instruments,
    /// the line is tinted from the line.
    private var sparkTone: PnLTone {
        PnLTone(
            sparkTint: SparkTint(series: spark, basesDisagree: row.sparkAndChangeDisagreeOnInstrument),
            change24h: row.stockDayMove?.ratio
        )
    }
}
