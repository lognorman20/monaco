import SwiftUI
import UIKit

/// The display voice: SF Pro's **width** axis, not a second typeface.
///
/// `display` and `title` are what a screen leads with, `section` heads a group of rows, and
/// `eyebrow` is the tracked uppercase label above a figure — "YOUR MONEY IN CABALS", "IN THE POT",
/// "UP FOR VOTE", "LIVE", and every stat label. The app had no tracked uppercase at all before
/// v3, which is why `eyebrow` carries more of the new voice than its 11pt suggests.
enum DisplayRole: CaseIterable {
    case display, title, section, eyebrow

    /// Design size at the default text size.
    var baseSize: CGFloat {
        switch self {
        case .display: return 32
        case .title: return 24
        case .section: return 18
        case .eyebrow: return 11
        }
    }

    var weight: Font.Weight {
        switch self {
        case .display, .title, .eyebrow: return .bold
        case .section: return .semibold
        }
    }

    /// The text style the role scales with.
    var textStyle: Font.TextStyle {
        switch self {
        case .display: return .largeTitle
        case .title: return .title2
        case .section: return .title3
        case .eyebrow: return .caption2
        }
    }

    /// Letter spacing as a fraction of the rendered size, so it scales with Dynamic Type instead
    /// of drifting into a gap at AX5.
    var trackingEm: CGFloat {
        switch self {
        case .display: return -0.02
        case .title: return -0.01
        case .section: return 0
        case .eyebrow: return 0.06
        }
    }

    /// Only `eyebrow` is uppercased, and it is uppercased *by the role* rather than in the copy,
    /// so VoiceOver reads the original sentence.
    var isUppercased: Bool { self == .eyebrow }

    /// SF Pro Expanded at this role's weight and a given rendered size.
    func font(size: CGFloat) -> Font {
        Font.system(size: size, weight: weight).width(.expanded)
    }

    /// The `UIFontMetrics` pre-scaled font, for the deprecated `MonacoTheme.Typo` statics and for
    /// UIKit appearance proxies, which have no view tree to scale inside.
    ///
    /// Prefer `.displayFont(_:)` everywhere else: this asks `UIFontMetrics` for a size once,
    /// outside the environment, so it ignores a `.dynamicTypeSize` cap on the view tree and does
    /// not re-render when the text size changes.
    var preScaledFont: Font {
        let uiTextStyle = UIFont.TextStyle(displayRole: self)
        let scaled = UIFontMetrics(forTextStyle: uiTextStyle).scaledValue(for: baseSize)
        return font(size: scaled)
    }
}

/// Scales a display figure inside the view tree, so a `.dynamicTypeSize` cap applies to it and a
/// text-size change invalidates the view. The exact parallel of `MoneyFont`, for the same reason.
struct DisplayFont: ViewModifier {
    let role: DisplayRole

    // `@ScaledMetric` needs its text style and base size as literals in the property wrapper, so
    // there is one per role rather than one driven by `role`. The sizes come from `DisplayRole`
    // so the two cannot drift; `MonacoTypeTests` asserts the text styles against it.
    @ScaledMetric(relativeTo: .largeTitle) private var display = DisplayRole.display.baseSize
    @ScaledMetric(relativeTo: .title2) private var title = DisplayRole.title.baseSize
    @ScaledMetric(relativeTo: .title3) private var section = DisplayRole.section.baseSize
    @ScaledMetric(relativeTo: .caption2) private var eyebrow = DisplayRole.eyebrow.baseSize

    private var size: CGFloat {
        switch role {
        case .display: return display
        case .title: return title
        case .section: return section
        case .eyebrow: return eyebrow
        }
    }

    func body(content: Content) -> some View {
        content
            .font(role.font(size: size))
            .tracking(size * role.trackingEm)
            .textCase(role.isUppercased ? .uppercase : nil)
    }
}

extension View {
    /// The only correct way to set a display face.
    ///
    /// Not `Font.system(size:weight:)`, which does not scale with Dynamic Type at all, and not a
    /// pre-scaled `UIFontMetrics` static, which scales once outside the environment and ignores a
    /// `.dynamicTypeSize` cap.
    func displayFont(_ role: DisplayRole) -> some View {
        modifier(DisplayFont(role: role))
    }
}

/// The nav-bar faces, for the UIKit appearance proxy that Chunk B owns.
///
/// Nav titles are the one display surface with a point cap: a large title that keeps growing at
/// AX5 pushes the whole screen down before the content has said anything.
enum MonacoNavType {
    static let inlineSize: CGFloat = 17
    static let inlineCap: CGFloat = 22
    static let largeSize: CGFloat = 30
    static let largeCap: CGFloat = 40

    /// SF Pro Expanded Semibold 17, capped at 22.
    static var inlineTitle: UIFont {
        scaled(size: inlineSize, weight: .semibold, textStyle: .headline, cap: inlineCap)
    }

    /// SF Pro Expanded Bold 30, capped at 40.
    static var largeTitle: UIFont {
        scaled(size: largeSize, weight: .bold, textStyle: .largeTitle, cap: largeCap)
    }

    /// Tracking in points for a nav title at `size`, matching `DisplayRole`'s −0.02/−0.01em.
    static func inlineTracking(size: CGFloat) -> CGFloat { size * -0.01 }

    static func largeTracking(size: CGFloat) -> CGFloat { size * -0.02 }

    private static func scaled(
        size: CGFloat,
        weight: UIFont.Weight,
        textStyle: UIFont.TextStyle,
        cap: CGFloat
    ) -> UIFont {
        let base = UIFont.systemFont(ofSize: size, weight: weight, width: .expanded)
        return UIFontMetrics(forTextStyle: textStyle).scaledFont(for: base, maximumPointSize: cap)
    }
}

private extension UIFont.TextStyle {
    init(displayRole: DisplayRole) {
        switch displayRole {
        case .display: self = .largeTitle
        case .title: self = .title2
        case .section: self = .title3
        case .eyebrow: self = .caption2
        }
    }
}
