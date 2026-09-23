import SwiftUI
import Testing
import UIKit
@testable import Monaco

/// `DisplayFont` builds its `@ScaledMetric` bases from `DisplayRole.baseSize`, so these assertions
/// are over the numbers that actually render — the same guard `MoneyStyleScalingTests` puts on the
/// money ladder, for the same reason: the sizes are restated as literals in the property wrappers
/// and nothing but a test ties the two together.
struct DisplayRoleTests {
    @Test func everyRoleHasItsDesignSize() {
        #expect(DisplayRole.display.baseSize == 32)
        #expect(DisplayRole.title.baseSize == 24)
        #expect(DisplayRole.section.baseSize == 18)
        #expect(DisplayRole.eyebrow.baseSize == 11)
    }

    @Test func rolesScaleAgainstTheRightTextStyles() {
        #expect(DisplayRole.display.textStyle == .largeTitle)
        #expect(DisplayRole.title.textStyle == .title2)
        #expect(DisplayRole.section.textStyle == .title3)
        #expect(DisplayRole.eyebrow.textStyle == .caption2)
    }

    @Test func weightsMatchTheTable() {
        #expect(DisplayRole.display.weight == .bold)
        #expect(DisplayRole.title.weight == .bold)
        #expect(DisplayRole.section.weight == .semibold)
        #expect(DisplayRole.eyebrow.weight == .bold)
    }

    /// Tracking is a fraction of the *rendered* size, not a point value, so it follows Dynamic
    /// Type. A fixed −0.6pt on a 32pt display face becomes invisible at AX5, and a fixed +0.66pt
    /// on an 11pt eyebrow becomes a gap.
    @Test func trackingIsProportionalAndSignedTheRightWay() {
        #expect(DisplayRole.display.trackingEm == -0.02)
        #expect(DisplayRole.title.trackingEm == -0.01)
        #expect(DisplayRole.section.trackingEm == 0)
        #expect(DisplayRole.eyebrow.trackingEm == 0.06)
    }

    /// Only the eyebrow is uppercased, and the role does it rather than the copy — so VoiceOver
    /// reads "Your money in cabals", not an acronym.
    @Test func onlyTheEyebrowIsUppercased() {
        for role in DisplayRole.allCases {
            #expect(role.isUppercased == (role == .eyebrow))
        }
    }

    /// The display voice is Avenir Next, which ships with iOS.
    ///
    /// `UIFont(name:size:)` returns nil when a face is missing and `Font.custom` falls back to the
    /// system face *silently*, so a missing face would ship an app that looks subtly wrong and
    /// reports nothing. This is the guard against that: every display weight must resolve to a
    /// real Avenir Next face, not to a fallback.
    @Test func displayTypeResolvesToAvenirNext() {
        for role in DisplayRole.allCases {
            let name = DisplayRole.faceName(for: role.weight)
            let font = UIFont(name: name, size: role.baseSize)
            #expect(font != nil, "\(role) asked for \(name), which did not resolve")
            #expect(
                font?.familyName == "Avenir Next",
                "\(role) resolved to \(font?.familyName ?? "nil"), not Avenir Next"
            )
            #expect(
                font?.familyName != UIFont.systemFont(ofSize: role.baseSize).familyName,
                "\(role) fell back to the system face"
            )
        }
    }

    /// Nav titles are the one display surface with a point cap: a large title that keeps growing
    /// at AX5 pushes the whole screen down before the content has said anything.
    @Test func navTitlesAreCapped() {
        let large = UIFontMetrics(forTextStyle: .largeTitle).scaledFont(
            for: UIFont(name: DisplayRole.uiFaceName(for: .bold), size: MonacoNavType.largeSize)!,
            maximumPointSize: MonacoNavType.largeCap,
            compatibleWith: UITraitCollection(preferredContentSizeCategory: .accessibilityExtraExtraExtraLarge)
        )
        #expect(large.pointSize <= MonacoNavType.largeCap)

        let inline = UIFontMetrics(forTextStyle: .headline).scaledFont(
            for: UIFont(name: DisplayRole.uiFaceName(for: .semibold), size: MonacoNavType.inlineSize)!,
            maximumPointSize: MonacoNavType.inlineCap,
            compatibleWith: UITraitCollection(preferredContentSizeCategory: .accessibilityExtraExtraExtraLarge)
        )
        #expect(inline.pointSize <= MonacoNavType.inlineCap)
    }
}

/// `MoneyStyle.mega`, the 56pt figure for the three screens where the figure *is* the screen.
struct MegaMoneyStyleTests {
    @Test func megaIsTheLargestFigureAndScalesLikeHero() {
        #expect(MoneyStyle.mega.baseSize == 56)
        #expect(MoneyStyle.mega.baseSize > MoneyStyle.hero.baseSize)
        #expect(MoneyStyle.mega.textStyle == .largeTitle)
        #expect(MoneyStyle.mega.weight == .semibold)
        #expect(MoneyStyle.mega.minimumScaleFactor == 0.4)
    }

    /// Both figures that own their screen stop growing at `accessibility2`, and nothing else does.
    /// Uncapped, a 56pt figure at AX5 fills the screen before the keypad under it has a chance.
    @Test func onlyTheScreenOwningFiguresCapDynamicType() {
        #expect(MoneyStyle.mega.capsDynamicType)
        #expect(MoneyStyle.hero.capsDynamicType)
        #expect(!MoneyStyle.large.capsDynamicType)
        #expect(!MoneyStyle.row.capsDynamicType)
        #expect(!MoneyStyle.caption.capsDynamicType)
    }

    /// Money stays standard-width SF with tabular digits. `.fontWidth(.expanded)` composed with
    /// `.monospacedDigit()` is unverified on device, and a figure that silently loses tabular
    /// alignment mid-roll is the one failure a money app cannot ship. Promote after hardware
    /// verification, not before — this is the assertion that has to be deleted to do it.
    @Test func moneyIsNotExpanded() {
        let money = UIFont.monospacedDigitSystemFont(ofSize: MoneyStyle.mega.baseSize, weight: .semibold)
        let expanded = UIFont.systemFont(ofSize: MoneyStyle.mega.baseSize, weight: .semibold, width: .expanded)
        let moneyWidth = ("1234567890" as NSString).size(withAttributes: [.font: money]).width
        let expandedWidth = ("1234567890" as NSString).size(withAttributes: [.font: expanded]).width
        #expect(moneyWidth != expandedWidth)
    }
}

private extension DisplayRole {
    var uiWeight: UIFont.Weight {
        switch weight {
        case .bold: return .bold
        case .semibold: return .semibold
        default: return .regular
        }
    }
}
