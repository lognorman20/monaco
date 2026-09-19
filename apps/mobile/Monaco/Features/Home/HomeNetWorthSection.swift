import MonacoCore
import SwiftUI

/// The hero: total money across every cabal, plus all-time P&L. No actions live here —
/// see `HomeBalanceRowSection` for Add money / Cash out.
struct HomeNetWorthSection: View {
    let dashboard: HomeDashboardDTO

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoHeroHeader(
                title: UsdAmountFormatter.format(decimalString: dashboard.netWorthUsd),
                caption: "Your money in cabals"
            )
            .dynamicTypeSize(...DynamicTypeSize.accessibility2)

            HStack(spacing: MonacoTheme.Space.s) {
                Text(dashboard.netWorthDollarPnl)
                    .font(.subheadline.monospacedDigit().weight(.semibold))
                    .foregroundStyle(MonacoTheme.signed(dashboard.netWorthDollarPnl))
                Text(PercentReturnFormatter.format(dashboard.netWorthPercentReturn))
                    .font(.subheadline.monospacedDigit().weight(.semibold))
                    .foregroundStyle(MonacoTheme.signed(dashboard.netWorthPercentReturn))
                Text("all time")
                    .font(MonacoTheme.TypeRole.caption)
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("home-net-worth")
    }
}

enum HomePnLTint {
    static func color(_ raw: String) -> Color {
        MonacoTheme.signed(raw)
    }
}
