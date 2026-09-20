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

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var titleWidthFloor = MonacoRowLayout.baseMinimumTitleWidth

    private var layout: MonacoRowLayout {
        MonacoRowLayout(dynamicTypeSize: dynamicTypeSize, scaledTitleWidthFloor: titleWidthFloor)
    }

    private var spark: SparklineSeries? { SparklineSeries(usdcMicros: row.sparkUsdcMicros) }

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
                    Sparkline(series: spark, tone: PnLTone(change24h: row.change24h), width: 44, height: 20)
                }
                VStack(alignment: .trailing, spacing: 2) {
                    MoneyText(decimalString: row.valueUsd, style: .row)
                    PnLText(dollarPnl: row.dollarPnl, style: .caption)
                        .accessibilityIdentifier("pot-row-pnl-\(row.symbol)")
                }
            }
            if row.change24h != nil {
                DayChangePill(change24h: row.change24h, priceUsdcMicros: row.markUsdcMicros)
                    .accessibilityIdentifier("pot-row-change-\(row.symbol)")
            }
            if row.afterHours == true {
                Text("After hours")
                    .font(MonacoTheme.Typo.micro)
                    .foregroundStyle(MonacoTheme.warning)
                    .accessibilityIdentifier("pot-after-hours-\(row.symbol)")
            }
        }
    }
}
