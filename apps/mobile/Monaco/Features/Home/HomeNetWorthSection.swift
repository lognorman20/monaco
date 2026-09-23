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
    /// `loaded` is the separate series read (#217), nil until it lands or while it is failing.
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

/// **The ink fold.** Home opens on a full-bleed ink slab that runs behind the status bar: total
/// money across every cabal, all-time P&L, the live curve, the window pills — and, folded in under
/// a hairline, the cash that is not in a cabal yet with its two money-movement actions. Paper
/// slides up over the slab's bottom edge at a 28pt radius.
///
/// This is one designed object where there used to be three stacked rectangles: a hero card, a
/// white balance card and a caption apologising that the curve was stuck on the past hour.
///
/// Ink is dark in *both* schemes — that is the whole argument. A screenshot of this screen says
/// "dark is your money, light is your people" without a word of copy.
struct HomeNetWorthSection: View {
    let dashboard: HomeDashboardDTO

    /// The curve's slot, resolved by `HomeView` from the dashboard and the selected window.
    var chart: HomeHeroChart = .hidden

    /// The window the curve is drawing. `HomeView` owns the read behind it.
    @Binding var range: HomeLeaderboardRange
    var isRangeLoading = false
    var rangeFailed = false

    /// The cash fold under the hairline. Everything this needs is the balance row's, moved in.
    let balanceFold: HomeBalanceFold

    /// True once the fold has scrolled past and the nav bar has taken the figure (§4 #21).
    /// The figure fades out as the title fades in, so the handoff is one movement rather than
    /// two things swapping places. `HomeView` owns the flag and the curve on both sides of it.
    var isHandedOff = false

    private static let chartHeight: CGFloat = 104

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                HStack(alignment: .center, spacing: MonacoTheme.Space.s) {
                    Text("Your money in cabals")
                        .displayFont(.eyebrow)
                        .foregroundStyle(MonacoTheme.Ink.fgSubtle)
                    Spacer(minLength: MonacoTheme.Space.s)
                    // Post-auth the brand stops vanishing. At 10% it is a watermark, not a logo,
                    // and it sits on the eyebrow line rather than in the corner, where it would
                    // collide with the cash fold's capsules.
                    MonacoMark(size: 22, monochrome: Color.white.opacity(0.10))
                }

                // Rule 3: a money figure never counts up. It appears at its value — the opacity
                // here is the handoff into the nav bar, never the figure arriving.
                MoneyText(decimalString: dashboard.netWorthUsd, style: .hero, color: MonacoTheme.Ink.fgPrimary)
                    .opacity(isHandedOff ? 0 : 1)

                HStack(spacing: MonacoTheme.Space.s) {
                    PnLBadge(
                        dollarPnl: dashboard.netWorthDollarPnl,
                        percentReturn: dashboard.netWorthPercentReturn,
                        onInk: true
                    )
                    Text("all time")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.Ink.fgMuted)
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("home-net-worth")

            chartSlot

            Divider()
                .overlay(MonacoTheme.Ink.line)
                .padding(.top, MonacoTheme.Space.m)

            balanceFold
                .padding(.top, MonacoTheme.Space.m)
        }
        .padding(.top, MonacoTheme.Space.m)
        .monacoInkSlab()
    }

    /// The badge above says "all time"; the curve draws whichever window the pills are on, so the
    /// pills sit directly under it and there is nothing left to caption.
    @ViewBuilder
    private var chartSlot: some View {
        switch chart {
        case .hidden:
            EmptyView()
        case .curve(let points):
            slot {
                HomePnLChartSection(points: points, onInk: true, height: Self.chartHeight, range: range)
                    // The curve bleeds to both screen edges; everything else keeps the gutter.
                    .padding(.horizontal, -MonacoTheme.Space.gutter)
            }
        case .reserved(let hasResolved):
            slot {
                ZStack {
                    Rectangle()
                        .fill(MonacoTheme.Ink.fgSubtle.opacity(0.45))
                        .frame(height: 1)
                    if let note = reservedNote(hasResolved: hasResolved) {
                        Text(note)
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.Ink.fgMuted)
                            .padding(.bottom, MonacoTheme.Space.s)
                    }
                }
                .frame(height: Self.chartHeight)
                .accessibilityElement(children: .combine)
                .accessibilityHidden(reservedNote(hasResolved: hasResolved) == nil)
                .accessibilityIdentifier(hasResolved ? "home-pnl-chart-empty" : "home-pnl-chart-pending")
            }
        }
    }

    /// Silent until a window has actually answered: on a cold start the curve lands about a second
    /// later, and "No curve yet" in the meantime is a sentence the member watches appear and then
    /// be taken away. A window that failed says so rather than borrowing another window's shape.
    private func reservedNote(hasResolved: Bool) -> String? {
        if rangeFailed { return "Couldn't load \(range.windowPhrase)" }
        if isRangeLoading { return nil }
        return hasResolved ? "No curve yet" : nil
    }

    private func slot<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            content()
            InkSegmented(
                HomeLeaderboardRange.allCases,
                selection: $range,
                label: \.label,
                accessibilityName: \.accessibilityName
            )
            .accessibilityIdentifier("home-hero-range")
        }
        .padding(.top, MonacoTheme.Space.m)
    }
}

private extension InkSegmented {
    /// Sugar so the call site reads as the two key paths it is, not two closures.
    init(
        _ options: [T],
        selection: Binding<T>,
        label: KeyPath<T, String>,
        accessibilityName: KeyPath<T, String>
    ) {
        self.init(
            options,
            selection: selection,
            label: { $0[keyPath: label] },
            accessibilityName: { $0[keyPath: accessibilityName] }
        )
    }
}
