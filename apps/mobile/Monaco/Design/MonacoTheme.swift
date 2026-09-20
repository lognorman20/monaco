import SwiftUI
import UIKit

/// Shared visual tokens: cool paper, deep ink, one electric-blue brand accent, and vivid money colour.
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
    // MARK: Surfaces

    /// Cool very-light gray sheet in light; true rich dark in dark.
    static let background = Color.adaptive(light: 0xF5F7FA, dark: 0x0A0D14)

    static let canvas = background

    /// Kept for source compatibility. The gradient wash is gone; this is the flat canvas.
    static let canvasWash = canvas

    /// Crisp white cards on the cool canvas; raised ink panels in dark.
    static let surface = Color.adaptive(light: 0xFFFFFF, dark: 0x121826)

    /// Field fill, segmented track, skeleton base, idle chip fill.
    static let surfaceSunken = Color.adaptive(light: 0xEDF1F7, dark: 0x1B2334)

    static let primaryText = Color.adaptive(light: 0x0B1220, dark: 0xF3F6FB)

    static let ink = primaryText

    static let secondaryText = Color.adaptive(light: 0x5B6880, dark: 0x94A2BC)

    static let muted = secondaryText

    /// Timestamps, placeholders and disabled labels. Real content, so it clears AA (4.5:1)
    /// on `canvas`, `surface` and `surfaceSunken` in both schemes — see `MonacoContrastTests`.
    static let tertiaryText = Color.adaptive(light: 0x616E86, dark: 0x8290AA)

    static let border = Color.adaptive(light: 0xE3E8F0, dark: 0x232C40)

    static let hairline = border

    // MARK: Brand

    /// The one brand accent: electric blue. Primary buttons, selected tab, links, focus rings,
    /// selected segments, the "Yes" vote, my chat bubbles, progress and vote dots.
    /// Brand blue never means gain — that is `profit`.
    static let brand = Color.adaptive(light: 0x1652F0, dark: 0x3B7BFF)

    /// Button/chip fill. Deeper than `brand` in dark so white labels clear AA (4.64:1).
    static let brandFill = Color.adaptive(light: 0x1652F0, dark: 0x2C6BF5)

    /// Label on `brandFill`. White both modes: 6.00:1 light, 4.64:1 dark.
    static let onBrand = Color.white

    /// Tinted chip / selected-row wash under brand text.
    static let brandWash = Color.adaptive(light: 0x1652F0, lightAlpha: 0.10, dark: 0x3B7BFF, darkAlpha: 0.18)

    /// Brand text and glyphs drawn *on* `brandWash`. Plain `brand` is only 3.69:1 there in dark;
    /// this pair clears AA on every surface the wash sits on.
    static let brandOnWash = Color.adaptive(light: 0x1652F0, dark: 0x7FA8FF)

    // MARK: Dark "money" hero cards (premium even in light mode)

    /// Deep ink card base for the Home net-worth hero and the cabal hero.
    static let heroInk = Color.adaptive(light: 0x0B1220, dark: 0x151D30)

    /// Very subtle top-left radial highlight on the hero card. No purple, no glass.
    static let heroInkHighlight = Color.adaptive(light: 0x2A3A5C, lightAlpha: 0.55, dark: 0x31415F, darkAlpha: 0.55)

    /// Primary text on a hero card.
    static let onHero = Color.white

    /// Captions on a hero card.
    static let onHeroMuted = Color.white.opacity(0.62)

    /// Divider inside a hero card.
    static let onHeroHairline = Color.white.opacity(0.14)

    // MARK: Money

    /// Signed P&L text on a plain surface. Clears AA on `canvas`, `surface` and `surfaceSunken`,
    /// not only on bare white. On a wash, use `profitOnWash` / `lossOnWash`.
    static let profit = Color.adaptive(light: 0x007A45, dark: 0x1FD286)

    static let loss = Color.adaptive(light: 0xD1272B, dark: 0xFF5A5F)

    /// Saturated P&L for chart strokes/fills and figures sitting on a dark hero card,
    /// where the background carries the contrast (6.8:1 on `heroInk`).
    static let profitVivid = Color.adaptive(light: 0x00B368, dark: 0x1FD286)

    static let lossVivid = Color.adaptive(light: 0xE5383B, dark: 0xFF5A5F)

    /// `PnLBadge` background on gains.
    static let profitWash = Color.adaptive(light: 0x00874D, lightAlpha: 0.14, dark: 0x1FD286, darkAlpha: 0.20)

    /// `PnLBadge` background on losses.
    static let lossWash = Color.adaptive(light: 0xDC2F33, lightAlpha: 0.12, dark: 0xFF5A5F, darkAlpha: 0.20)

    /// P&L text drawn *on* its own wash. `profit` / `loss` are derived for paper, and the wash
    /// lifts the background under them, so the badge needs its own deeper pair (AA on every surface).
    static let profitOnWash = Color.adaptive(light: 0x006B3D, dark: 0x1FD286)

    static let lossOnWash = Color.adaptive(light: 0xB3261E, dark: 0xFF7A7E)

    /// `PnLBadge` on a dark hero card: the wash must read on ink, not on paper.
    static let profitWashOnHero = Color.adaptive(light: 0x1FD286, lightAlpha: 0.20, dark: 0x1FD286, darkAlpha: 0.20)

    static let lossWashOnHero = Color.adaptive(light: 0xFF5A5F, lightAlpha: 0.22, dark: 0xFF5A5F, darkAlpha: 0.22)

    /// P&L figures on a deep ink hero card. The hero is dark in both schemes, so these are fixed
    /// colours. `lossVivid` is tuned for chart strokes on paper and only reaches 3.3:1 on the
    /// hero loss wash, which is why the hero has its own pair.
    static let profitOnHero = Color(hex: 0x1FD286)

    static let lossOnHero = Color(hex: 0xFF7A7E)

    // MARK: Toast

    /// Toast capsule. Ink in light, a raised ink panel in dark — never the brand fill, or the toast
    /// reads as a second primary button stacked above the real one.
    static let toastFill = Color.adaptive(light: 0x0B1220, dark: 0x1B2334)

    /// Hairline on the toast. Carries the capsule's edge in dark, where the drop shadow is invisible.
    static let toastStroke = Color.adaptive(light: 0x0B1220, lightAlpha: 0, dark: 0xFFFFFF, darkAlpha: 0.10)

    static let toastLabel = Color.white

    /// Success glyph on `toastFill` (6.8:1 light, 7.9:1 dark).
    static let toastSuccessGlyph = profitVivid

    /// Failure glyph on `toastFill` (4.4:1 light, 5.2:1 dark).
    static let toastErrorGlyph = lossVivid

    // MARK: Roles

    static let primaryButtonFill = brandFill

    static let primaryButtonLabel = onBrand

    static let secondaryButtonFill = surface

    static let secondaryButtonLabel = primaryText

    static let destructive = loss

    static let accent = brand

    static let disabled = Color.adaptive(light: 0xC2CAD8, dark: 0x3A4459)

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

    /// Saturated identity tints. Picked from the group id, never from the name, so a rename keeps the colour.
    /// Every `fill` carries white initials at AA; `soft` is the low-alpha wash for tinted areas that
    /// still hold ink text. No purple, and nothing close to brand blue or profit green.
    enum CabalTint: CaseIterable {
        case sage, peach, butter, clay, sky

        /// Mark tile, accent stripe, chart key.
        var fill: Color {
            switch self {
            case .sage: return Color.adaptive(light: 0x0D7D74, dark: 0x10938A)
            case .peach: return Color.adaptive(light: 0xC2570C, dark: 0xD9681A)
            case .butter: return Color.adaptive(light: 0xA16207, dark: 0xBC7A10)
            case .clay: return Color.adaptive(light: 0xBE3455, dark: 0xD44467)
            case .sky: return Color.adaptive(light: 0x17627D, dark: 0x1E7A99)
            }
        }

        /// Initials and glyphs drawn on `fill`.
        var onFill: Color { .white }

        /// Low-alpha wash of `fill` for tinted surfaces that still carry ink text.
        var soft: Color { fill.opacity(0.12) }

        /// Brighter than `fill` so the tint still reads as a mark or stripe on a deep ink hero card.
        var onInk: Color {
            switch self {
            case .sage: return Color(hex: 0x2CC3B4)
            case .peach: return Color(hex: 0xFF9248)
            case .butter: return Color(hex: 0xEBB13C)
            case .clay: return Color(hex: 0xFF6C8B)
            case .sky: return Color(hex: 0x46B3DB)
            }
        }

        /// Chart line colour for this cabal.
        var stroke: Color {
            switch self {
            case .sage: return Color.adaptive(light: 0x0D7D74, dark: 0x2CC3B4)
            case .peach: return Color.adaptive(light: 0xC2570C, dark: 0xFF9248)
            case .butter: return Color.adaptive(light: 0xA16207, dark: 0xEBB13C)
            case .clay: return Color.adaptive(light: 0xBE3455, dark: 0xFF6C8B)
            case .sky: return Color.adaptive(light: 0x17627D, dark: 0x46B3DB)
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

        /// Low-alpha wash for a cabal: `CabalTint.forGroupId(groupId).soft`.
        static func soft(forGroupId groupId: String) -> Color {
            forGroupId(groupId).soft
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
        /// Prefer `.moneyFont(_:)`. These statics pre-scale with `UIFontMetrics`, so they ignore a
        /// `.dynamicTypeSize` cap on the view tree and do not re-render when the text size changes.
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

        /// SF Pro pre-scaled against the process-wide content size category.
        /// Prefer `.moneyFont(_:)`, which scales inside the view tree.
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
    /// A single fixed colour from an 0xRRGGBB literal — same in both schemes.
    init(hex: UInt32, alpha: Double = 1) {
        self.init(uiColor: UIColor(hex: hex, alpha: alpha))
    }

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
