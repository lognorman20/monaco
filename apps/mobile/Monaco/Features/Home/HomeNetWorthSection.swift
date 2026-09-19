import MonacoCore
import SwiftUI

/// The hero: total money across every cabal, all-time P&L, and the P&L curve — on the
/// premium deep-ink money card, in both light and dark. No actions live here —
/// see `HomeBalanceRowSection` for Add money / Cash out.
struct HomeNetWorthSection: View {
    let dashboard: HomeDashboardDTO

    /// The 1H series. Fewer than three points and the curve is hidden: a flat line reads as broken.
    var pnlPoints: [HomePnLSeriesPointDTO] = []

    /// Reserves the curve's height while the series loads after first paint (#217).
    var isChartLoading = false

    private var showsChart: Bool { pnlPoints.count >= 3 }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                Text("Your money in cabals")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.onHeroMuted)

                MoneyText(decimalString: dashboard.netWorthUsd, style: .hero, color: MonacoTheme.onHero)

                HStack(spacing: MonacoTheme.Space.s) {
                    PnLBadge(
                        dollarPnl: dashboard.netWorthDollarPnl,
                        percentReturn: dashboard.netWorthPercentReturn,
                        onInk: true
                    )
                    Text("all time")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.onHeroMuted)
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("home-net-worth")

            if showsChart {
                HomePnLChartSection(points: pnlPoints, onInk: true, height: 104)
                    .padding(.top, MonacoTheme.Space.m)
                    // The curve bleeds to the card's edges; the text above keeps its inset.
                    .padding(.horizontal, -MonacoTheme.Space.m)
            } else if isChartLoading {
                Color.clear
                    .frame(height: 104)
                    .padding(.top, MonacoTheme.Space.m)
                    .accessibilityLabel("Loading chart")
                    .accessibilityIdentifier("home-pnl-chart-loading")
            }
        }
        .monacoHeroCard(padding: MonacoTheme.Space.l)
    }
}
