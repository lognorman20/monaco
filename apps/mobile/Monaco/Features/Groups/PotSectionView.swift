import MonacoCore
import SwiftUI

/// Holdings: every stock the cabal owns, then its cash. The cabal account address lives in
/// the details sheet, not here.
struct PotSectionView: View {
    let pot: [PotRowDTO]
    var onAddMoney: () -> Void = {}

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
        let displayName = AssetCatalogDisplayName.format(catalogName: "", symbol: row.symbol, kind: row.resolvedAssetKind)
        return MonacoRow(
            title: displayName,
            subtitle: "\(quantityLabel(row)) · \(UsdAmountFormatter.format(decimalString: row.markUsd))",
            isLast: isLast
        ) {
            StockMark(symbol: row.symbol, displayName: displayName, assetKind: row.resolvedAssetKind)
        } trailing: {
            MoneyText(decimalString: row.valueUsd, style: .row)
            PnLText(dollarPnl: row.dollarPnl, style: .caption)
                .accessibilityIdentifier("pot-row-pnl-\(row.symbol)")
            if row.afterHours == true, row.resolvedAssetKind != .preIpo {
                Text("After hours")
                    .font(MonacoTheme.Typo.micro)
                    .foregroundStyle(MonacoTheme.warning)
                    .accessibilityIdentifier("pot-after-hours-\(row.symbol)")
            }
        }
        .accessibilityIdentifier("pot-row-\(row.symbol)")
    }

    private func quantityLabel(_ row: PotRowDTO) -> String {
        if let atomics = row.tokenAmount, !atomics.isEmpty {
            return ProposalShareFormatter.sharesLabel(
                fromAtomics: atomics,
                decimals: row.resolvedTokenDecimals,
                kind: row.resolvedAssetKind
            )
        }
        let unit = row.resolvedAssetKind == .preIpo ? PreIpoCopy.tokenLabelPlural : "shares"
        return "\(row.units) \(unit)"
    }

    static func isCash(_ row: PotRowDTO) -> Bool {
        row.symbol.uppercased() == "USDC"
    }
}
