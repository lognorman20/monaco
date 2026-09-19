import SwiftUI
import UIKit

/// Shared visual tokens: warm paper, ink, and colour only for money and cabal identity.
///
/// - `background` / `canvas` — warm paper sheet, flat
/// - `surface` — cards, grouped lists, sheets, tab bar
/// - `surfaceSunken` — field fill, segmented track, skeleton base, idle chips
/// - `primaryText` / `ink` — headings, body, primary button fill
/// - `secondaryText` / `muted` — captions
/// - `tertiaryText` — timestamps, placeholders
/// - `border` / `hairline` — 1pt separators
/// - `profit` / `loss` (+ `profitWash` / `lossWash`) — signed P&L only; green never means anything else
/// - `warning` — amber, pending and after-hours states
/// - `CabalTint` — pastel identity fills, only inside marks and the cabal hero
enum MonacoTheme {
    static let background = Color.adaptive(light: 0xF4F3EF, dark: 0x121211)

    static let canvas = background

    /// Kept for source compatibility. The gradient wash is gone; this is the flat canvas.
    static let canvasWash = canvas

    static let surface = Color.adaptive(light: 0xFCFBF8, dark: 0x1C1C1A)

    /// Field fill, segmented track, skeleton base, idle chip fill.
    static let surfaceSunken = Color.adaptive(light: 0xECEAE4, dark: 0x262623)

    static let primaryText = Color.adaptive(light: 0x161613, dark: 0xF2F1EC)

    static let ink = primaryText

    static let secondaryText = Color.adaptive(light: 0x6B6A64, dark: 0x9F9D96)

    static let muted = secondaryText

    /// Timestamps and placeholders.
    static let tertiaryText = Color.adaptive(light: 0x9C9A93, dark: 0x6E6C66)

    static let border = Color.adaptive(light: 0xE3E1DA, dark: 0x2E2D2A)

    static let hairline = border

    static let primaryButtonFill = ink

    static let primaryButtonLabel = Color.adaptive(light: 0xFCFBF8, dark: 0x161613)

    static let secondaryButtonFill = surface

    static let secondaryButtonLabel = primaryText

    static let profit = Color.adaptive(light: 0x0E7C4A, dark: 0x3CCB7F)

    static let loss = Color.adaptive(light: 0xC0392B, dark: 0xFF6B5E)

    /// `PnLBadge` background on gains.
    static let profitWash = Color.adaptive(light: 0x0E7C4A, lightAlpha: 0.12, dark: 0x3CCB7F, darkAlpha: 0.18)

    /// `PnLBadge` background on losses.
    static let lossWash = Color.adaptive(light: 0xC0392B, lightAlpha: 0.10, dark: 0xFF6B5E, darkAlpha: 0.18)

    static let destructive = loss

    static let accent = ink

    static let disabled = Color.adaptive(light: 0xB9B7B0, dark: 0x4A4945)

    static let success = profit

    /// Amber: pending, after hours, closing soon.
    static let warning = Color.adaptive(light: 0x9A5B13, dark: 0xE0A458)

    /// Green for gains, red for losses, muted for zero / missing.
    /// Accepts ASCII "-" and the typographic minus "−" (U+2212) as a loss sign.
    static func signed(_ raw: String?) -> Color {
        let trimmed = raw?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if trimmed.isEmpty || trimmed == "—" {
            return muted
        }
        let isNegative = trimmed.hasPrefix("-") || trimmed.hasPrefix("\u{2212}")
        let magnitude = trimmed.drop(while: { !$0.isNumber && $0 != "." })
        let digitsOnly = magnitude.filter { $0.isNumber || $0 == "." }
        if digitsOnly.isEmpty || digitsOnly.allSatisfy({ $0 == "0" || $0 == "." }) {
            return muted
        }
        if isNegative { return loss }
        if trimmed.hasPrefix("+") { return profit }
        let numeric = trimmed
            .replacingOccurrences(of: "%", with: "")
            .replacingOccurrences(of: ",", with: "")
            .replacingOccurrences(of: "$", with: "")
        if let value = Double(numeric) {
            if value > 0 { return profit }
            if value < 0 { return loss }
        }
        return muted
    }

    /// Pastel identity tints. Picked from the group id, never from the name, so a rename keeps the colour.
    enum CabalTint: CaseIterable {
        case sage, peach, butter, clay, sky

        /// Tile, hero and strip background.
        var fill: Color {
            switch self {
            case .sage: return Color.adaptive(light: 0xDCE8D6, dark: 0x2A3A2C)
            case .peach: return Color.adaptive(light: 0xF6DDCB, dark: 0x43301F)
            case .butter: return Color.adaptive(light: 0xF1E7C2, dark: 0x3E3820)
            case .clay: return Color.adaptive(light: 0xEAD6CF, dark: 0x3E2A25)
            case .sky: return Color.adaptive(light: 0xD5E3EA, dark: 0x223540)
            }
        }

        /// Chart line colour for this cabal. Deeper than `fill` so a 2pt line reads on paper.
        var stroke: Color {
            switch self {
            case .sage: return Color.adaptive(light: 0x5E7F55, dark: 0x9CC392)
            case .peach: return Color.adaptive(light: 0xB9724A, dark: 0xF0B08A)
            case .butter: return Color.adaptive(light: 0x9A8230, dark: 0xE3CD7A)
            case .clay: return Color.adaptive(light: 0x94604F, dark: 0xD9A596)
            case .sky: return Color.adaptive(light: 0x4E7A91, dark: 0x93BED3)
            }
        }

        /// The one tint function for a cabal. Every surface (rows, strip cards, hero, chat header, profile)
        /// passes the cabal's `groupId`, never its name, so a cabal is the same colour everywhere.
        /// Stable across launches: FNV-1a 64 over the UTF-8 bytes of the trimmed, lowercased id, mod 5
        /// (lowercased because Swift's `UUID.uuidString` is uppercase while the API sends lowercase).
        /// Never `String.hashValue`, which is randomised per launch.
        static func forGroupId(_ groupId: String) -> CabalTint {
            let all = CabalTint.allCases
            let key = groupId.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
            return all[Int(fnv1a64(key) % UInt64(all.count))]
        }

        /// Background fill for a cabal: `CabalTint.forGroupId(groupId).fill`.
        static func fill(forGroupId groupId: String) -> Color {
            forGroupId(groupId).fill
        }

        /// Chart line colour for a cabal: `CabalTint.forGroupId(groupId).stroke`.
        static func stroke(forGroupId groupId: String) -> Color {
            forGroupId(groupId).stroke
        }

        static func fnv1a64(_ string: String) -> UInt64 {
            var hash: UInt64 = 0xCBF2_9CE4_8422_2325
            for byte in string.utf8 {
                hash ^= UInt64(byte)
                hash = hash &* 0x0000_0100_0000_01B3
            }
            return hash
        }
    }

    /// Two voices: Avenir Next for display and section titles, SF Pro (tabular digits) for everything else.
    enum Typo {
        static var moneyHero: Font { money(size: 44, weight: .semibold, relativeTo: .largeTitle) }
        static var moneyLarge: Font { money(size: 28, weight: .semibold, relativeTo: .title1) }
        static var moneyRow: Font { money(size: 17, weight: .semibold, relativeTo: .body) }
        static var moneyCaption: Font { money(size: 13, weight: .medium, relativeTo: .footnote) }
        static let display = Font.custom("AvenirNext-Bold", size: 30, relativeTo: .largeTitle)
        static let title = Font.custom("AvenirNext-DemiBold", size: 22, relativeTo: .title2)
        static let section = Font.custom("AvenirNext-DemiBold", size: 19, relativeTo: .title3)
        static let rowTitle = Font.system(.body, weight: .semibold)
        static let body = Font.system(.body)
        static let callout = Font.system(.callout)
        static let caption = Font.system(.footnote)
        static let micro = Font.system(.caption2, weight: .semibold)

        /// SF Pro at a fixed design size that still follows Dynamic Type.
        static func money(size: CGFloat, weight: Font.Weight, relativeTo style: UIFont.TextStyle) -> Font {
            let scaled = UIFontMetrics(forTextStyle: style).scaledValue(for: size)
            return Font.system(size: scaled, weight: weight).monospacedDigit()
        }
    }

    enum Radius {
        static let chip: CGFloat = 20
        static let card: CGFloat = 24
        static let sheet: CGFloat = 28
        static let pill: CGFloat = 28
        static let hero: CGFloat = 28
        /// `CabalMark` / `StockMark` at 44pt; marks scale this proportionally.
        static let tile: CGFloat = 16
        static let field: CGFloat = 14
        static let bubble: CGFloat = 20
    }

    enum Space {
        static let xs: CGFloat = 4
        static let s: CGFloat = 8
        static let sm: CGFloat = 12
        static let m: CGFloat = 16
        static let l: CGFloat = 24
        static let xl: CGFloat = 32
        /// Screen side padding.
        static let gutter: CGFloat = 20
    }

    /// Existing call sites. New code uses `Typo`.
    enum TypeRole {
        static let display = Font.custom("AvenirNext-Bold", size: 28)
        static let title = Font.custom("AvenirNext-DemiBold", size: 20)
        static let body = Font.system(.body)
        static let caption = Font.system(.footnote)
    }
}

extension Color {
    /// Light/dark pair from 0xRRGGBB literals.
    static func adaptive(light: UInt32, lightAlpha: Double = 1, dark: UInt32, darkAlpha: Double = 1) -> Color {
        Color(
            uiColor: UIColor { traits in
                traits.userInterfaceStyle == .dark
                    ? UIColor(hex: dark, alpha: darkAlpha)
                    : UIColor(hex: light, alpha: lightAlpha)
            }
        )
    }
}

extension UIColor {
    convenience init(hex: UInt32, alpha: Double = 1) {
        self.init(
            red: CGFloat((hex >> 16) & 0xFF) / 255,
            green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255,
            alpha: CGFloat(alpha)
        )
    }
}
