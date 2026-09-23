import SwiftUI
import Testing
import UIKit
@testable import Monaco

/// The ink band has no fixed height, and this is the test that keeps it that way.
///
/// It is the one full-bleed object in the app and it carries the most important thing on its
/// screen — the balance, or the vote that closes in an hour. A band that clips at a large text
/// size hides exactly the content it exists to present, and a fixed height is the easy way to get
/// there, so the height is measured rather than asserted about.
@MainActor
struct MonacoInkBandTests {
    private static let width: CGFloat = 390

    private func height(
        _ typeSize: DynamicTypeSize,
        @ViewBuilder content: () -> some View
    ) -> CGFloat {
        let root = content()
            .dynamicTypeSize(typeSize)
            .frame(width: Self.width)
        let controller = UIHostingController(rootView: root)
        controller.view.backgroundColor = .clear
        return controller.sizeThatFits(in: CGSize(width: Self.width, height: .greatestFiniteMagnitude)).height
    }

    private func sampleBand(_ typeSize: DynamicTypeSize) -> CGFloat {
        height(typeSize) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                Text("Needs your vote").displayFont(.eyebrow)
                Text("Weekend investors").displayFont(.title)
                Text("Closes in 4 hours, and this line is long enough to wrap at a large text size.")
                    .font(MonacoTheme.Typo.caption)
            }
            .monacoInkBand()
        }
    }

    @Test func theBandGrowsWithTheTextInsideIt() {
        let base = sampleBand(.large)
        let ax5 = sampleBand(.accessibility5)
        #expect(base > 0, "the band measured no height at all")
        #expect(ax5 > base, "the band did not grow from large (\(base)) to AX5 (\(ax5)): it is clipping")
    }

    /// The growth has to be real growth, not one extra line before a fixed frame takes over.
    @Test func theBandIsNotQuietlyCappedOnTheWayUp() {
        let sizes: [DynamicTypeSize] = [.large, .xxxLarge, .accessibility3, .accessibility5]
        let heights = sizes.map { sampleBand($0) }
        for index in 0..<(heights.count - 1) {
            #expect(
                heights[index + 1] >= heights[index],
                "the band shrank from \(sizes[index]) to \(sizes[index + 1])"
            )
        }
        #expect(heights.last! > heights.first! * 1.5, "the band barely grew across the whole range")
    }

    /// The band's own chrome is a known, fixed amount: 24pt of internal padding top and bottom,
    /// plus 40pt of clearance on each side of it. If a band ever measures less than its content
    /// plus that chrome, the padding has been dropped.
    @Test func theBandAddsItsPaddingAndItsClearance() {
        let bare = height(.large) {
            Text("Weekend investors").displayFont(.title)
        }
        let banded = height(.large) {
            Text("Weekend investors").displayFont(.title).monacoInkBand()
        }
        let chrome = MonacoInkBandMetrics.verticalPadding * 2 + MonacoInkBandMetrics.clearance * 2
        #expect(banded >= bare + chrome - 1, "expected \(bare) + \(chrome) of chrome, measured \(banded)")
    }

    /// Everything inside a band is in the ink world, so a shared component picks the ink pair
    /// without being told twice — and a component that reads `\.monacoWorld` on a *paper* screen
    /// still gets `.paper`, because ink is the exception and the app opens into daylight.
    @Test func theBandDeclaresTheInkWorldAndTheDefaultIsPaper() {
        #expect(EnvironmentValues().monacoWorld == .paper)
        #expect(MonacoPalette.palette(for: .ink).background == MonacoTheme.Ink.base)
        #expect(MonacoPalette.palette(for: .paper).background == MonacoTheme.bgBase)
    }

    /// On ink, a "quiet fill" is a *well* — a segmented track on an ink band is recessed, not
    /// raised. Pointing it at a lighter fill would put a raised control inside a recessed band,
    /// which is the confusion the paper `bgSunken`/`fillQuiet` split just undid.
    @Test func inkHasNoRaisedControlFill() {
        #expect(MonacoPalette.ink.quietFill == MonacoTheme.Ink.sunken)
        #expect(MonacoPalette.paper.quietFill == MonacoTheme.fillQuiet)
    }

    /// The loudness budget, in its countable form. Ink gets one saturated fill; a paper social
    /// surface gets three; a paper surface that is about *money* is held to the ink budget, which
    /// is why the budget takes the question instead of answering it from the world alone.
    @Test func theLoudnessBudgetHoldsMoneySurfacesToTheInkBudget() {
        #expect(MonacoWorld.ink.loudnessBudget(isMoneySurface: true) == 1)
        #expect(MonacoWorld.ink.loudnessBudget(isMoneySurface: false) == 1)
        #expect(MonacoWorld.paper.loudnessBudget(isMoneySurface: true) == 1)
        #expect(MonacoWorld.paper.loudnessBudget(isMoneySurface: false) == 3)
    }
}
