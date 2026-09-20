import MonacoCore
import SwiftUI

/// What the hero's chart slot holds.
///
/// The slot is decided once, from the dashboard itself, so it cannot appear and disappear
/// while the curve loads (#327): a member with no cabals never gets a slot, and a member who
/// has one keeps the same height whether the curve is still loading, too short to draw, or
/// drawn. Home therefore paints its final layout on the first frame instead of shifting
/// ~120 pt twice in the first second.
enum HomeHeroChart: Equatable {
    /// No cabals: there is no curve to wait for.
    case hidden
    /// The slot, held at the curve's height, with a baseline where the curve will be. It says
    /// the same thing whether the series is still loading, failed, or came back too short, so
    /// nothing moves as it resolves.
    case reserved
    case curve([HomePnLSeriesPointDTO])

    /// A flat two-point line reads as broken, so the curve needs three points to earn the slot.
    static func resolve(points: [HomePnLSeriesPointDTO], hasCabals: Bool) -> HomeHeroChart {
        guard hasCabals else { return .hidden }
        return points.count >= 3 ? .curve(points) : .reserved
    }
}

/// The hero: total money across every cabal, all-time P&L, and the P&L curve — on the
/// premium deep-ink money card, in both light and dark. No actions live here —
/// see `HomeBalanceRowSection` for Add money / Cash out.
struct HomeNetWorthSection: View {
    let dashboard: HomeDashboardDTO

    /// The 1H curve's slot, resolved by `HomeView` from the dashboard and the series.
    var chart: HomeHeroChart = .hidden

    private static let chartHeight: CGFloat = 104

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

            chartSlot
        }
        .monacoHeroCard(padding: MonacoTheme.Space.l)
    }

    /// The badge above says "all time"; the curve is the past hour, so it says so too —
    /// otherwise the shape reads as the lifetime return it is not.
    @ViewBuilder
    private var chartSlot: some View {
        switch chart {
        case .hidden:
            EmptyView()
        case .curve(let points):
            slot {
                HomePnLChartSection(points: points, onInk: true, height: Self.chartHeight)
                    // The curve bleeds to the card's edges; the caption keeps its inset.
                    .padding(.horizontal, -MonacoTheme.Space.m)
            }
        case .reserved:
            slot {
                ZStack {
                    Rectangle()
                        .fill(MonacoTheme.onHeroMuted.opacity(0.25))
                        .frame(height: 1)
                    Text("No curve yet")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.onHeroMuted)
                        .padding(.bottom, MonacoTheme.Space.s)
                }
                .frame(height: Self.chartHeight)
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("home-pnl-chart-empty")
            }
        }
    }

    private func slot<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("Past hour")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.onHeroMuted)
            content()
        }
        .padding(.top, MonacoTheme.Space.m)
    }
}
