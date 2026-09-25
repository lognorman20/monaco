import SwiftUI
import UIKit

/// Shared visual tokens: cream paper, deep forest ink, and vivid money colour.
///
/// The palette is the Monaco brand as it exists on monacolabs.xyz and in the logo: cream
/// `#F8F5EE` paper, a deep forest `#0F291C` ink, and the mark's cream-on-forest pairing.
///
/// The one rule that shapes everything below: **green means "this went up"**. So the brand's
/// forest — dark enough to read as ink rather than as profit — is the interactive colour
/// (primary buttons, selected states, links), exactly as the site uses it for its buttons, and
/// profit stays a distinctly brighter, far more saturated green. The site's mid green `#1E8C55`
/// is deliberately *not* used anywhere in the app: at that lightness and chroma it sits right on
/// top of `profit`, and a "Yes" vote or a selected chip in it would read as a gain.
///
/// - `background` / `canvas` — cream paper sheet, flat
/// - `surface` — cards, grouped lists, sheets, tab bar
/// - `surfaceSunken` — field fill, segmented track, skeleton base, idle chips
/// - `primaryText` / `ink` — headings, body
/// - `secondaryText` / `muted` — captions
/// - `tertiaryText` — timestamps and other quiet real content
/// - `disabledLabel` — disabled control labels and field placeholders; below AA on purpose
/// - `border` / `hairline` — 1pt separators
/// - `brand` / `brandFill` — the forest interactive colour; never a gain, see `MonacoContrastTests`
/// - `profit` / `loss` (+ `profitWash` / `lossWash`) — signed P&L only; green never means anything else
/// - `warning` — amber, pending and after-hours states
/// - `CabalTint` — pastel identity fills, only inside marks and the cabal hero
enum MonacoTheme {
    // MARK: Surfaces

    /// The site's cream sheet in light. In dark, a near-neutral forest-black.
    ///
    /// Dark is not the logo field `#0F291C` even though that is tempting: at that chroma every
    /// warm wash drawn over it turns olive — a loss badge came out `#42492F` — and the money
    /// greens stop being the only saturated thing on screen. So the dark ramp keeps the forest
    /// *hue* (~160°) at a whisper of chroma (0.015–0.025 OKLCh) and lets `profit`, `loss` and the
    /// hero card carry the colour. The full `#0F291C` still appears in dark, as `heroInk`.
    static let background = Color.adaptive(light: 0xF8F5EE, dark: 0x0B1512)

    static let canvas = background

    /// Kept for source compatibility. The gradient wash is gone; this is the flat canvas.
    static let canvasWash = canvas

    /// White cards on the cream canvas (the site's `bg-2`); lifted forest-black panels in dark.
    static let surface = Color.adaptive(light: 0xFFFFFF, dark: 0x13211B)

    /// Field fill, segmented track, skeleton base, idle chip fill.
    static let surfaceSunken = Color.adaptive(light: 0xF1ECE2, dark: 0x1D2E26)

    /// The site's ink, verbatim, for page text. In dark, the logo's cream.
    static let primaryText = Color.adaptive(light: 0x0F291C, dark: 0xF3EEE5)

    static let ink = primaryText

    static let secondaryText = Color.adaptive(light: 0x55645B, dark: 0xA3B3A9)

    static let muted = secondaryText

    /// Timestamps and other real-but-quiet content. Clears AA (4.5:1) on `canvas`, `surface` and
    /// `surfaceSunken` in both schemes — see `MonacoContrastTests`.
    ///
    /// Not for disabled controls or placeholders: see `disabledLabel`.
    static let tertiaryText = Color.adaptive(light: 0x5D6C62, dark: 0x92A398)

    /// Disabled control labels and field placeholders.
    ///
    /// Deliberately below AA and deliberately not `tertiaryText`. WCAG 1.4.3 exempts disabled
    /// controls, and unavailable has to *look* unavailable: at `tertiaryText`'s 4.5:1 a disabled
    /// primary CTA reads as a live button, and the hero-sized `$0.00` placeholder on the amount
    /// screen reads as an amount the member already entered.
    ///
    /// Still legible, though — 2.6:1 to 4.3:1 on the three surfaces, against `tertiaryText`'s
    /// 4.7:1 to 7.0:1. `MonacoContrastTests` holds it inside that band from both sides, so it
    /// cannot drift up into looking live or down into being unreadable.
    static let disabledLabel = Color.adaptive(light: 0x8A968E, dark: 0x6E7D75)

    static let border = Color.adaptive(light: 0xE5DFD2, dark: 0x2A3E35)

    static let hairline = border

    // MARK: Brand

    /// The brand accent as *text and glyphs*: "See all", "Show more", "Try again", the focus ring,
    /// the selected tab item, the "Yes" vote dot, the stats range marker.
    ///
    /// A forest one step lighter than `brandFill`, because those labels sit next to `primaryText`
    /// in the same row (`MonacoSectionHeader` draws the title in `ink` and the action in `brand`)
    /// and an identical ink would stop reading as tappable. ΔL* 0.12 from `primaryText` in light;
    /// in dark it separates by hue instead — mint against the warm cream, 70° apart.
    ///
    /// Never a gain: `profit` is 2.2× (light) to 2.9× (dark) its chroma and ΔL* 0.11–0.14 away.
    /// `brandAndProfitCannotBeConfused` pins that.
    static let brand = Color.adaptive(light: 0x204A37, dark: 0xBFE6C8)

    /// Button/chip fill: primary buttons, selected segments, selected amount chips, "Approve",
    /// the "Yes" vote pill, my chat bubbles.
    ///
    /// Light is the site's ink `#0F291C` verbatim — the same value its own buttons use. Dark
    /// inverts the logo instead of lightening the forest: the mark is cream on forest, so in dark
    /// the button is the brand's `green-soft` with an ink label. That also puts the primary action
    /// as far from `profit` as the palette allows (ΔL* 0.17, 7.7× the chroma).
    static let brandFill = Color.adaptive(light: 0x0F291C, dark: 0xE4F2E6)

    /// Label on `brandFill`: cream on forest in light (14.2:1), forest on green-soft in dark (13.4:1).
    /// Not `Color.white` any more — `brandFill` is light in dark mode.
    static let onBrand = Color.adaptive(light: 0xF8F5EE, dark: 0x0F291C)

    /// Tinted chip / selected-row wash under brand text — the selected tab pill, selected rows.
    ///
    /// A low-alpha *ink*, not a green one. A mint wash would land within a few points of
    /// `profitWash`, and a selected chip that looks like a gain badge is the exact failure the
    /// forest brand exists to avoid. Over cream this resolves to a soft sage-grey `#DCDDD5`,
    /// against `profitWash`'s `#D5E1D5`… which is close enough that the *text* on top has to do
    /// the work, and it does: `brandOnWash` is ink, `profitOnWash` is green.
    static let brandWash = Color.adaptive(light: 0x0F291C, lightAlpha: 0.12, dark: 0xBFE6C8, darkAlpha: 0.16)

    /// Brand text and glyphs drawn *on* `brandWash`. Plain `brand` does not clear AA over the
    /// wash in dark; this pair does, on every surface the wash sits on.
    static let brandOnWash = Color.adaptive(light: 0x0F291C, dark: 0xCFEAD6)

    // MARK: Deep forest "money" hero cards (premium even in light mode)

    /// Card base for the Home net-worth hero and the cabal hero: the logo field `#0F291C`, the
    /// same value in both schemes. In dark it is the one fully saturated forest panel in the app,
    /// lifted off the near-neutral canvas by chroma rather than by lightness.
    static let heroInk = Color(hex: 0x0F291C)

    /// Very subtle top-left radial highlight on the hero card. No purple, no glass.
    static let heroInkHighlight = Color(hex: 0x2E6B4B, alpha: 0.55)

    /// Primary text on a hero card.
    static let onHero = Color.white

    /// Captions on a hero card.
    static let onHeroMuted = Color.white.opacity(0.62)

    /// Divider inside a hero card.
    static let onHeroHairline = Color.white.opacity(0.14)

    // MARK: Money

    /// Signed P&L text on a plain surface. Clears AA on `canvas`, `surface` and `surfaceSunken`,
    /// not only on bare white. On a wash, use `profitOnWash` / `lossOnWash`.
    ///
    /// Light keeps `#007A45` unchanged from the blue-brand palette, on purpose. It is the
    /// brightest, most saturated green that still clears AA on the cream `surfaceSunken` (4.61:1
    /// — `#00854A` drops to 4.00:1), and it already sits 0.14 L* and 2.2× the chroma away from
    /// `brand`. Dark lifts slightly, from `#1FD286` to `#35D68C`, to widen the same gap against
    /// the mint `brand` from ΔL* 0.096 to 0.111.
    static let profit = Color.adaptive(light: 0x007A45, dark: 0x35D68C)

    /// Loss, on the brand's own warm red (`down` `#C8543E`, hue ~33°) rather than the old cool
    /// `#D1272B`. `#C8543E` itself is only 4.0:1 on white, so text uses a deeper draw of it.
    static let loss = Color.adaptive(light: 0xB23A28, dark: 0xF08A72)

    /// Saturated P&L for chart strokes/fills and figures sitting on a deep forest hero card,
    /// where the background carries the contrast (8.0:1 on `heroInk`).
    static let profitVivid = Color.adaptive(light: 0x0FB268, dark: 0x35D68C)

    static let lossVivid = Color.adaptive(light: 0xCE4C33, dark: 0xF08A72)

    /// `PnLBadge` background on gains.
    static let profitWash = Color.adaptive(light: 0x00693B, lightAlpha: 0.14, dark: 0x35D68C, darkAlpha: 0.20)

    /// `PnLBadge` background on losses. Dark draws a redder base than `loss` (`#E85A42`) because a
    /// warm wash over a green-cast surface goes brown before it goes red.
    static let lossWash = Color.adaptive(light: 0xB23A28, lightAlpha: 0.12, dark: 0xE85A42, darkAlpha: 0.22)

    /// P&L text drawn *on* its own wash. `profit` / `loss` are derived for paper, and the wash
    /// lifts the background under them, so the badge needs its own deeper pair (AA on every surface).
    static let profitOnWash = Color.adaptive(light: 0x00693B, dark: 0x35D68C)

    static let lossOnWash = Color.adaptive(light: 0x9C3122, dark: 0xF5A18C)

    /// `PnLBadge` on a forest hero card: the wash must read on ink, not on paper.
    static let profitWashOnHero = Color(hex: 0x35D68C, alpha: 0.20)

    static let lossWashOnHero = Color(hex: 0xF5A18C, alpha: 0.22)

    /// P&L figures on a deep forest hero card. The hero is dark in both schemes, so these are
    /// fixed colours. `lossVivid` is tuned for chart strokes on paper and does not clear AA on the
    /// hero loss wash, which is why the hero has its own pair.
    static let profitOnHero = Color(hex: 0x35D68C)

    static let lossOnHero = Color(hex: 0xF5A18C)

    // MARK: Toast

    /// Toast capsule. A lifted forest-grey in light, a raised panel in dark — never the brand
    /// fill, or the toast reads as a second primary button stacked above the real one.
    ///
    /// Light used to be `#0B1220`, which under the forest brand would have landed on top of
    /// `brandFill`'s `#0F291C`. It is lifted to `#33413A` so `theToastIsNotTheBrandFill` keeps its
    /// margin (0.030 relative luminance apart, against the 0.02 the test asks for).
    static let toastFill = Color.adaptive(light: 0x33413A, dark: 0x26332C)

    /// Hairline on the toast. Carries the capsule's edge in dark, where the drop shadow is invisible.
    static let toastStroke = Color.adaptive(light: 0x33413A, lightAlpha: 0, dark: 0xFFFFFF, darkAlpha: 0.10)

    static let toastLabel = Color.white

    /// State glyphs on `toastFill`. The toast panel is dark in both schemes, so — like the hero
    /// pair above — these are fixed colours rather than aliases of the scheme-adaptive
    /// `profitVivid` / `lossVivid`, which are documented for chart strokes on paper. Pointing a
    /// toast token at a token tuned for a different background is how `primaryButtonFill = brandFill`
    /// turned the toast into a blue capsule (#309): a later retune for paper would quietly drop
    /// the glyph's contrast here.
    ///
    /// 5.9:1 light, 8.1:1 dark.
    static let toastSuccessGlyph = Color(hex: 0x35D68C)

    /// 5.0:1 light, 6.8:1 dark.
    static let toastErrorGlyph = Color(hex: 0xF5A18C)

    // MARK: Roles

    static let primaryButtonFill = brandFill

    static let primaryButtonLabel = onBrand

    static let secondaryButtonFill = surface

    static let secondaryButtonLabel = primaryText

    static let destructive = loss

    static let accent = brand

    static let disabled = Color.adaptive(light: 0xCBD2CB, dark: 0x37453D)

    static let success = profit

    /// Amber: pending, after hours, closing soon.
    static let warning = Color.adaptive(light: 0x8A5A16, dark: 0xE5B26A)

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

    /// Saturated identity tints. Picked from the group id, never from the name, so a rename keeps
    /// the colour. `soft` is the low-alpha wash for tinted areas that still hold ink text.
    ///
    /// Retuned for the forest brand. The old five were carried over from the electric-blue app and
    /// none of them was a money colour, which was the correctness bar — but at full saturation a
    /// teal, a safety orange and a hot crimson are a different design language from cream paper and
    /// forest ink, and a cabal row sat between the two. These five keep the same job and the same
    /// separation while belonging to the palette around them.
    ///
    /// The constraint is not contrast, it is *hue*. Five cabals have to be five colours at a glance,
    /// and none of them may be mistaken for `profit` or `loss` in a row that also carries money:
    ///
    /// | tint   | hue  | L*    | from `profit` | from `loss` |
    /// |--------|------|-------|---------------|-------------|
    /// | pine   | 190° | 0.487 | 35°           | 159°        |
    /// | ochre  |  78° | 0.535 | 78°           |  46°        |
    /// | plum   | 341° | 0.447 | 175°          |  51°        |
    /// | indigo | 255° | 0.452 |  99°          | 137°        |
    /// | moss   | 118° | 0.438 |  38°          |  93°        |
    ///
    /// Hue alone is not enough, which a first pass proved on the Home list: ochre and moss are the
    /// closest pair at 40°, and at equal lightness two cabals in adjacent rows read as one colour.
    /// So every pair is separated by 60° of hue *or* 0.08 of L\*, and ochre and moss take the
    /// second route — moss is a deep olive where ochre is a mid gold.
    ///
    /// That is also why moss barely lifts in dark while the others do. Lifting each fill to the
    /// same contrast floor independently flattened the ladder and put ochre and moss back within
    /// 0.024 of each other; the ladder is the constraint, and white initials clear AA on a darker
    /// tile anyway.
    ///
    /// The closest approach to a money colour is moss at 38° from profit. Hue is what does that
    /// work — these tints are not quieter than the money colours, they run 0.9× to 1.2× profit's
    /// chroma — and it is enough because a tint only ever fills a mark or a stripe, never a figure.
    ///
    /// White initials clear 4.5:1 on every fill in both schemes, where the old palette met only the
    /// 3:1 large-text bar in dark. That was defensible for 15pt bold initials; holding the body bar
    /// costs nothing here and means `fill` is safe behind small white text too.
    enum CabalTint: CaseIterable {
        // Order is the identity mapping: a cabal's tint is its id hashed mod 5, so these stay in
        // their slots and no existing cabal changes which of the five it gets.
        case pine, ochre, plum, indigo, moss

        /// Mark tile, accent stripe, chart key.
        var fill: Color {
            switch self {
            case .pine: return Color.adaptive(light: 0x0E6E6A, dark: 0x11827D)
            case .ochre: return Color.adaptive(light: 0x8F6410, dark: 0x9A6C11)
            case .plum: return Color.adaptive(light: 0x7A3A66, dark: 0xB15494)
            case .indigo: return Color.adaptive(light: 0x2F5788, dark: 0x4076B9)
            case .moss: return Color.adaptive(light: 0x4E5817, dark: 0x545F19)
            }
        }

        /// Initials and glyphs drawn on `fill`.
        var onFill: Color { .white }

        /// Low-alpha wash of `fill` for tinted surfaces that still carry ink text.
        var soft: Color { fill.opacity(0.12) }

        /// Brighter than `fill` so the tint still reads as a mark or stripe on a deep ink hero card.
        var onInk: Color {
            switch self {
            case .pine: return Color(hex: 0x35C4BD)
            case .ochre: return Color(hex: 0xE2B04A)
            case .plum: return Color(hex: 0xD68CC2)
            case .indigo: return Color(hex: 0x7DAFE0)
            case .moss: return Color(hex: 0xB8CC63)
            }
        }

        /// Chart line colour for this cabal.
        var stroke: Color {
            switch self {
            case .pine: return Color.adaptive(light: 0x0E6E6A, dark: 0x35C4BD)
            case .ochre: return Color.adaptive(light: 0x8F6410, dark: 0xE2B04A)
            case .plum: return Color.adaptive(light: 0x7A3A66, dark: 0xD68CC2)
            case .indigo: return Color.adaptive(light: 0x2F5788, dark: 0x7DAFE0)
            case .moss: return Color.adaptive(light: 0x4E5817, dark: 0xB8CC63)
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
