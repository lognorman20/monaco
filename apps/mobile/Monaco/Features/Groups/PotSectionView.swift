import MonacoCore
import SwiftUI

/// Holdings: every stock the cabal owns, then its cash. The cabal account address lives in
/// the details sheet, not here.
struct PotSectionView: View {
    let pot: [PotRowDTO]
    var onAddMoney: () -> Void = {}

    private var stocks: [PotRowDTO] { pot.filter { !Self.isCash($0) } }
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
                        stockRow(row, isLast: row.id == stocks.last?.id && cash == nil)
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

    private func stockRow(_ row: PotRowDTO, isLast: Bool) -> some View {
        MonacoRow(
            title: AssetDisplayNames.name(forSymbol: row.symbol) ?? AssetSymbolFormatter.display(row.symbol),
            subtitle: "\(sharesLabel(row)) · \(UsdAmountFormatter.format(decimalString: row.markUsd))",
            isLast: isLast
        ) {
            StockMark(symbol: AssetSymbolFormatter.display(row.symbol))
        } trailing: {
            MoneyText(decimalString: row.valueUsd, style: .row)
            PnLText(dollarPnl: row.dollarPnl, style: .caption)
                .accessibilityIdentifier("pot-row-pnl-\(row.symbol)")
            if row.afterHours == true {
                Text("After hours")
                    .font(MonacoTheme.Typo.micro)
                    .foregroundStyle(MonacoTheme.warning)
                    .accessibilityIdentifier("pot-after-hours-\(row.symbol)")
            }
        }
        .accessibilityIdentifier("pot-row-\(row.symbol)")
    }

    /// Shares from the raw token amount when present; the decimal `units` string otherwise.
    private func sharesLabel(_ row: PotRowDTO) -> String {
        if let atomics = row.tokenAmount, !atomics.isEmpty {
            return ProposalShareFormatter.sharesLabel(fromAtomics: atomics)
        }
        return "\(row.units) shares"
    }

    static func isCash(_ row: PotRowDTO) -> Bool {
        row.symbol.uppercased() == "USDC"
    }
}
