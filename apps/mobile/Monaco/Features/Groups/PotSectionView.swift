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
        VStack(alignment: .leading, spacing: 12) {
            Text("Holdings")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

            if stocks.isEmpty && !hasCash {
                emptyState
            } else {
                VStack(spacing: 0) {
                    ForEach(stocks) { row in
                        stockRow(row, isLast: row.id == stocks.last?.id && cash == nil)
                    }
                    if let cash {
                        cashRow(cash)
                    }
                }
                .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
                .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))

                if stocks.isEmpty {
                    Text("Nothing bought yet. Propose the first buy.")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.muted)
                        .accessibilityIdentifier("pot-nothing-bought")
                }
            }
        }
        .accessibilityIdentifier("group-holdings")
    }

    private func stockRow(_ row: PotRowDTO, isLast: Bool) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(AssetSymbolFormatter.format(row.symbol))
                    .font(.body.weight(.semibold))
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                Text("\(row.units) shares · \(UsdAmountFormatter.format(decimalString: row.markUsd))")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 2) {
                Text(UsdAmountFormatter.format(decimalString: row.valueUsd))
                    .font(.body.weight(.semibold).monospacedDigit())
                    .foregroundStyle(MonacoTheme.ink)
                Text(row.dollarPnl)
                    .font(.footnote.monospacedDigit())
                    .foregroundStyle(MonacoTheme.signed(row.dollarPnl))
                    .accessibilityIdentifier("pot-row-pnl-\(row.symbol)")
                if row.afterHours == true {
                    Text("After hours")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(MonacoTheme.warning)
                        .accessibilityIdentifier("pot-after-hours-\(row.symbol)")
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .frame(minHeight: 60)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("pot-row-\(row.symbol)")
    }

    private func cashRow(_ row: PotRowDTO) -> some View {
        HStack(spacing: 12) {
            Text("Cash")
                .font(.body.weight(.semibold))
                .foregroundStyle(MonacoTheme.ink)
            Spacer(minLength: 8)
            Text(UsdAmountFormatter.format(decimalString: row.valueUsd))
                .font(.body.weight(.semibold).monospacedDigit())
                .foregroundStyle(MonacoTheme.ink)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .frame(minHeight: 60)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("pot-row-\(row.symbol)")
    }

    private var emptyState: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Nothing bought yet")
                .font(.body.weight(.semibold))
                .foregroundStyle(MonacoTheme.ink)
            Text("Add money, then propose the first buy.")
                .font(.subheadline)
                .foregroundStyle(MonacoTheme.muted)
            Button("Add money", action: onAddMoney)
                .buttonStyle(.monacoSecondary)
                .accessibilityIdentifier("pot-empty-add-money")
        }
        .accessibilityIdentifier("pot-empty")
    }

    static func isCash(_ row: PotRowDTO) -> Bool {
        row.symbol.uppercased() == "USDC"
    }
}
