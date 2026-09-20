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
    /// The slot, held at the curve's height, with a baseline where the curve will be.
    ///
    /// `hasResolved` is whether a series has actually come back. Until it has, the slot holds
    /// the height silently — the curve is on its way and saying anything about it would be a
    /// guess. Only a series that landed and turned out too short to draw is labelled.
    case reserved(hasResolved: Bool)
    case curve([HomePnLSeriesPointDTO])

    /// A flat two-point line reads as broken, so the curve needs three points to earn the slot.
    ///
    /// `loaded` is the separate 1H read (#217), nil until it lands or while it is failing.
    /// `embedded` is the copy the dashboard carries, which the backend currently hard-codes to
    /// an empty array — so it only counts as an answer when it actually has points. Without
    /// that distinction every cold start resolves "not read yet" as "read, and empty".
    static func resolve(
        loaded: [HomePnLSeriesPointDTO]?,
        embedded: [HomePnLSeriesPointDTO],
        hasCabals: Bool
    ) -> HomeHeroChart {
        guard hasCabals else { return .hidden }
        guard let series = loaded ?? (embedded.isEmpty ? nil : embedded) else {
            return .reserved(hasResolved: false)
        }
        return series.count >= 3 ? .curve(series) : .reserved(hasResolved: true)
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
        case .reserved(let hasResolved):
            slot {
                ZStack {
                    Rectangle()
                        .fill(MonacoTheme.onHeroMuted.opacity(0.25))
                        .frame(height: 1)
                    // Silent until a series has actually come back: on a cold start the curve
                    // lands about a second later, and "No curve yet" in the meantime is a
                    // sentence the member watches appear and then be taken away.
                    if hasResolved {
                        Text("No curve yet")
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.onHeroMuted)
                            .padding(.bottom, MonacoTheme.Space.s)
                    }
                }
                .frame(height: Self.chartHeight)
                .accessibilityElement(children: .combine)
                .accessibilityHidden(!hasResolved)
                .accessibilityIdentifier(hasResolved ? "home-pnl-chart-empty" : "home-pnl-chart-pending")
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
