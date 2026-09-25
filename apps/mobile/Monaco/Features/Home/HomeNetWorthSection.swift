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

/// The top of Home: total money across every cabal, all-time P&L, and the past hour's curve —
/// set straight on the paper, the way a brokerage opens on the figure rather than on a card.
///
/// This used to be a deep ink card. It was the only designed moment on the screen, which made
/// the rest of Home read as the settings that came after it, and it made the money look like a
/// wallet balance. The figure is the title now; the curve bleeds to the screen's edges under it.
struct HomeNetWorthSection: View {
    let dashboard: HomeDashboardDTO

    /// The 1H curve's slot, resolved by `HomeView` from the dashboard and the series.
    var chart: HomeHeroChart = .hidden

    // lane: portfolio
    /// Opens the portfolio. Nil keeps the figure a plain figure (previews, older call sites).
    var onSeePortfolio: (() -> Void)?

    private static let chartHeight: CGFloat = 92

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                // lane: portfolio
                HStack(spacing: MonacoTheme.Space.xs) {
                    Text("Your money in cabals")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    if onSeePortfolio != nil {
                        Image(systemName: "chevron.right")
                            .font(.caption2.weight(.semibold))
                            .foregroundStyle(MonacoTheme.tertiaryText)
                            .accessibilityHidden(true)
                    }
                }

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
            .padding(.horizontal, MonacoTheme.Space.m)
            .accessibilityElement(children: .combine)
            // lane: portfolio
            .modifier(SeePortfolioAction(action: onSeePortfolio))
            .accessibilityIdentifier("home-net-worth")

            chartSlot
        }
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
                HomePnLChartSection(points: points, height: Self.chartHeight)
            }
        case .reserved(let hasResolved):
            slot {
                ZStack {
                    MonacoRule(color: MonacoTheme.hairline)
                    // Silent until a series has actually come back: on a cold start the curve
                    // lands about a second later, and "No curve yet" in the meantime is a
                    // sentence the member watches appear and then be taken away.
                    if hasResolved {
                        Text("No curve yet")
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.muted)
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
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            content()
            Text("Past hour")
                .font(MonacoTheme.Typo.stamp)
                .foregroundStyle(MonacoTheme.tertiaryText)
                .padding(.horizontal, MonacoTheme.Space.m)
        }
        .padding(.top, MonacoTheme.Space.m)
    }
}

// lane: portfolio
/// Makes Home's figure the way into the portfolio: the whole block is the target, and it
/// reads as a button to VoiceOver.
private struct SeePortfolioAction: ViewModifier {
    let action: (() -> Void)?

    func body(content: Content) -> some View {
        if let action {
            Button {
                Haptics.selection()
                action()
            } label: {
                content
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityHint("Opens your portfolio")
        } else {
            content
        }
    }
}
