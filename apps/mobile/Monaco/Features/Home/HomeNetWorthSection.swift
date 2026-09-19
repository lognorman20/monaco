import MonacoCore
import SwiftUI

/// The hero: total money across every cabal, plus all-time P&L. No actions live here —
/// see `HomeBalanceRowSection` for Add money / Cash out.
struct HomeNetWorthSection: View {
    let dashboard: HomeDashboardDTO

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Your money in cabals")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)

            MoneyText(decimalString: dashboard.netWorthUsd, style: .hero)

            HStack(spacing: MonacoTheme.Space.s) {
                PnLBadge(
                    dollarPnl: dashboard.netWorthDollarPnl,
                    percentReturn: dashboard.netWorthPercentReturn
                )
                Text("all time")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("home-net-worth")
    }
}
