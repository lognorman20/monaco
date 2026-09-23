import SwiftUI
import UIKit

/// Shared visual tokens for two worlds in one binary.
///
/// **Ink is where money is held** — calm, exact, at most one saturated fill on screen. **Paper is
/// where friends argue about stocks** — warm, faced, up to three. Dark means *this is your money*;
/// light means *these are your people*. Every surface declares its world with `\.monacoWorld`, and
/// the world decides the palette, the elevation and how loud the surface may be.
///
/// Three rules this table exists to keep:
///
/// 1. **Green only ever means profit.** A passed proposal is `inkWash`. An active agent is
///    `brandWash`. An online dot is `brand`. Never green.
/// 2. **Blue only ever means tap.** `brand` is the interactive accent and nothing else carries
///    it — which is why a cabal's tint never reaches a button (`controlTint` is for carets).
/// 3. **A money figure never counts up.** It appears at its value; only `.numericText`
///    interpolates digits it was actually given.
///
/// - Paper surfaces: `bgBase` · `bgRaised` · `bgSunken` (a well) · `fillQuiet` (a control fill)
/// - Paper foreground: `fgPrimary` · `fgMuted` · `fgSubtle` · `fgDisabled` (below AA on purpose)
/// - Edges: `line` (1pt separators and card strokes) · `lineStrong` (E2 in dark, pending vote ring)
/// - `Ink` — the money world; fixed values, identical in both schemes
/// - Intents, none of them green: `brand` · `warning` · `danger` · `inkWash`
/// - Money: `profit` / `loss` and their washes — signed P&L only
/// - `CabalTint` (in `MonacoCabalTint.swift`) — identity fills, only on identity surfaces
enum MonacoTheme {
    // MARK: Paper surfaces

    /// Screen canvas. Flat — no gradient, no texture.
    static let bgBase = Color.adaptive(light: 0xF4F6FA, dark: 0x080B12)

    /// Every card and grouped list.
    static let bgRaised = Color.adaptive(light: 0xFFFFFF, dark: 0x131A28)

    /// A *well recessed into* a surface: chart plot areas, inset rows. Darker than the card it
    /// sits in, in both schemes.
    ///
    /// Not the same job as `fillQuiet`, and this is the fix for a verified defect. One token used
    /// to serve both, and in dark the "sunken" value (`#1B2334`, luminance 0.01683) was *lighter*
    /// than the card it sat inside (`#121826`, 0.01212) — a well that read as a bump. A control
    /// should read lighter than its surface in dark; a well should read darker. `bgSunken` is the
    /// well, `fillQuiet` is the control, and `DesignTokenOrderTests` pins the whole dark ladder
    /// so the two can never converge back into one token.
    static let bgSunken = Color.adaptive(light: 0xEDF1F7, dark: 0x0E1420)

    /// An *inert raised control fill*: segmented track, field fill, skeleton base, `StockMark`
    /// ticker tile, idle chips.
    ///
    /// The dark value is the old `surfaceSunken`, unchanged, so the components that depended on
    /// it are pixel-identical across the split rather than silently flattened into the canvas.
    static let fillQuiet = Color.adaptive(light: 0xEDF1F7, dark: 0x1B2334)

    /// 1pt separators and card strokes.
    static let line = Color.adaptive(light: 0xE2E8F1, dark: 0x232C40)

    /// Raised-elevation stroke in dark, and the pending vote ring. Nowhere else.
    static let lineStrong = Color.adaptive(light: 0xCBD5E4, dark: 0x33405A)

    // MARK: Paper surfaces — existing names

    /// Cool very-light gray sheet in light; true rich dark in dark.
    static let background = bgBase

    static let canvas = bgBase

    /// Kept for source compatibility. The gradient wash is gone; this is the flat canvas.
    static let canvasWash = bgBase

    /// Crisp white cards on the cool canvas; raised ink panels in dark.
    static let surface = bgRaised

    /// Field fill, segmented track, skeleton base, idle chip fill.
    ///
    /// Aliases `fillQuiet`, **not** `bgSunken`: every existing call site of this token is a
    /// control fill, so the split leaves them exactly where they were.
    static let surfaceSunken = fillQuiet

    // MARK: Paper foreground

    static let fgPrimary = Color.adaptive(light: 0x0B1220, dark: 0xF3F6FB)

    static let fgMuted = Color.adaptive(light: 0x5B6880, dark: 0x94A2BC)

    static let fgSubtle = Color.adaptive(light: 0x616E86, dark: 0x8290AA)

    static let fgDisabled = Color.adaptive(light: 0x848EA3, dark: 0x69768D)

    static let primaryText = fgPrimary

    static let ink = fgPrimary

    static let secondaryText = fgMuted

    static let muted = fgMuted

    /// Timestamps and other real-but-quiet content. Clears AA (4.5:1) on `canvas`, `surface` and
    /// `surfaceSunken` in both schemes — see `MonacoContrastTests`.
    ///
    /// Not for disabled controls or placeholders: see `disabledLabel`.
    static let tertiaryText = fgSubtle

    /// Disabled control labels and field placeholders.
    ///
    /// Deliberately below AA and deliberately not `tertiaryText`. WCAG 1.4.3 exempts disabled
    /// controls, and unavailable has to *look* unavailable: at `tertiaryText`'s 4.5:1 a disabled
    /// primary CTA reads as a live button, and the hero-sized `$0.00` placeholder on the amount
    /// screen reads as an amount the member already entered.
    ///
    /// Still legible, though — 2.9:1 to 4.2:1 on the three surfaces, against `tertiaryText`'s
    /// 4.5:1 to 6.0:1. `MonacoContrastTests` holds it inside that band from both sides, so it
    /// cannot drift up into looking live or down into being unreadable.
    static let disabledLabel = fgDisabled

    static let border = line

    static let hairline = line

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

    // MARK: Ink — the money world, dark in both schemes

    /// Ink is dark in light mode and dark in dark mode. That is the point: dark means *this is
    /// your money*, light means *these are your people*, and a screenshot has to say so without a
    /// caption. These are therefore fixed `Color(hex:)` values, the same discipline
    /// `profitOnHero` and the toast glyphs already follow — a scheme-adaptive token borrowed onto
    /// ink is how a later retune for paper quietly drops contrast here (#309).
    enum Ink {
        /// The ink slab / band fill.
        static let base = Color(hex: 0x0B1220)

        /// A card inside an ink band.
        static let raised = Color(hex: 0x151D30)

        /// A well inside ink: chart plot area, segmented track on ink.
        static let sunken = Color(hex: 0x070C16)

        /// Divider inside ink.
        static let line = Color.white.opacity(0.10)

        /// Edge of a raised card on ink.
        static let lineStrong = Color.white.opacity(0.18)

        /// Figures and titles.
        static let fgPrimary = Color.white

        /// Captions.
        static let fgMuted = Color.white.opacity(0.62)

        /// Timestamps and eyebrows on ink. 0.48, not 0.45: at 0.45 this measures 4.41:1 on
        /// `Ink.raised` and fails AA.
        static let fgSubtle = Color.white.opacity(0.48)

        /// The interactive accent on ink. Brand blue is too dark on `#0B1220` (2.3:1); this pair
        /// clears 7:1 on every ink surface.
        static let accent = Color(hex: 0x7FA8FF)

        /// The top-left radial highlight on an ink surface. `UnitPoint(0.08, -0.05)`, radius 340.
        static let highlight = Color(hex: 0x2A3A5C, alpha: 0.55)

        /// Full-bleed band edge: the 1pt rule at the top and bottom of an ink band.
        static let edge = Color.white.opacity(0.06)
    }

    // MARK: The intent ramp — three intents, none of them green

    /// Amber wash under `warningOnWash` text: pending, closing soon, after hours.
    static let warningWash = Color.adaptive(light: 0x9A5B13, lightAlpha: 0.12, dark: 0xE0A458, darkAlpha: 0.16)

    /// Amber text drawn *on* `warningWash`.
    static let warningOnWash = Color.adaptive(light: 0x7A4408, dark: 0xE8BE84)

    /// Amber on an ink surface, and its wash.
    static let warningOnInk = Color(hex: 0xE8BE84)

    static let warningWashOnInk = Color(hex: 0xE0A458, alpha: 0.18)

    /// "This went wrong with your money": a failed transfer, a destructive confirmation.
    ///
    /// `danger` bold is `loss`. That overlap is the one deliberate collision between the intent
    /// ramp and the money ramp — a failed transfer and a losing position are the same news — and
    /// it is the only one, which is what keeps green meaning profit and nothing else.
    static let danger = loss

    static let dangerWash = lossWash

    static let dangerOnWash = lossOnWash

    /// The neutral intent. A *passed* proposal is this, never green: green is profit.
    static let inkWash = Color.adaptive(light: 0x0B1220, lightAlpha: 0.08, dark: 0xFFFFFF, darkAlpha: 0.10)

    /// Brand wash on an ink surface, for `Ink.accent` text and an active-agent chip.
    static let brandWashOnInk = Color(hex: 0x3B7BFF, alpha: 0.20)

    /// Carets, spinners, pickers, sliders and every other UIKit-backed control tint.
    ///
    /// Deliberately **not** `brand`. Blue means tap, and a caret is not a tap target; the two
    /// root `.tint(MonacoTheme.ink)` overrides that hid brand blue from the tab bar go away with
    /// Chunk B, and this is what the controls beneath them repoint to instead of inheriting the
    /// accent by accident.
    static let controlTint = fgPrimary

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

    /// State glyphs on `toastFill`. The toast panel is dark in both schemes, so — like the hero
    /// pair above — these are fixed colours rather than aliases of the scheme-adaptive
    /// `profitVivid` / `lossVivid`, which are documented for chart strokes on paper. Pointing a
    /// toast token at a token tuned for a different background is how `primaryButtonFill = brandFill`
    /// turned the toast into a blue capsule (#309): a later retune for paper would quietly drop
    /// the glyph's contrast here.
    ///
    /// 9.5:1 light, 8.0:1 dark.
    static let toastSuccessGlyph = Color(hex: 0x1FD286)

    /// 7.4:1 light, 6.2:1 dark.
    static let toastErrorGlyph = Color(hex: 0xFF7A7E)

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

    /// Two voices. Avenir Next leads — display, titles and section heads, via `.displayFont(_:)`.
    /// SF Pro with tabular digits carries money and UI type.
    ///
    /// SF Pro's expanded width axis was tried as the display voice in v3 and reverted: it read as
    /// novelty rather than authority, which is the wrong register for a screen showing somebody's
    /// money. Avenir Next is the serious, brokerage-like voice this app has always had. It ships
    /// with iOS, so there is no bundle cost, no licence to verify, and no silent-fallback failure
    /// mode from a bad `Info.plist`.
    enum Typo {
        /// Prefer `.moneyFont(_:)`. These statics pre-scale with `UIFontMetrics`, so they ignore a
        /// `.dynamicTypeSize` cap on the view tree and do not re-render when the text size changes.
        static var moneyHero: Font { money(size: 44, weight: .semibold, relativeTo: .largeTitle) }
        static var moneyLarge: Font { money(size: 28, weight: .semibold, relativeTo: .title1) }
        static var moneyRow: Font { money(size: 17, weight: .semibold, relativeTo: .body) }
        static var moneyCaption: Font { money(size: 13, weight: .medium, relativeTo: .footnote) }
        /// Use `.displayFont(.display)`. This static pre-scales with `UIFontMetrics`, so it
        /// ignores a `.dynamicTypeSize` cap and does not re-render when the text size changes;
        /// the modifier scales inside the view tree, the way `.moneyFont(_:)` already does.
        @available(*, deprecated, message: "Use .displayFont(.display)")
        static var display: Font { DisplayRole.display.preScaledFont }

        /// Use `.displayFont(.title)`.
        @available(*, deprecated, message: "Use .displayFont(.title)")
        static var title: Font { DisplayRole.title.preScaledFont }

        /// Use `.displayFont(.section)`.
        @available(*, deprecated, message: "Use .displayFont(.section)")
        static var section: Font { DisplayRole.section.preScaledFont }

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

    /// Two steps, not eight. The old ladder (20/24/28/28/28/16/14/20) meant nothing on screen
    /// read as more important than anything else. `container` is everything that holds content;
    /// `object` is the handful of things that *are* the screen.
    ///
    /// Chips and pills are `Capsule()`, never a radius approximation. All shapes `.continuous`.
    enum Radius {
        /// Cards, grouped lists, strip cards, proposal cards, rows.
        static let container: CGFloat = 20

        /// Ink slabs and bands, sheets, hero, bottom bars.
        static let object: CGFloat = 28

        /// Scheduled for deletion: use `container`.
        static let chip: CGFloat = 20

        /// Scheduled for deletion: use `container`.
        static let card: CGFloat = 24

        static let sheet: CGFloat = object
        static let pill: CGFloat = object
        static let hero: CGFloat = object
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
        /// Screen side padding. Every screen root, no exceptions.
        static let gutter: CGFloat = 20

        /// Between two top-level sections.
        static let section: CGFloat = 28

        /// A section header to its own content.
        static let headerToContent: CGFloat = 12

        /// Clearance above and below a full-bleed ink band.
        static let band: CGFloat = 40
    }

    /// Scheduled for deletion once its five remaining call sites migrate to `.displayFont(_:)`.
    /// Uses the same Avenir Next faces meanwhile, so no surface is rendering a different voice.
    enum TypeRole {
        @available(*, deprecated, message: "Use .displayFont(.display)")
        static var display: Font { Font.custom("AvenirNext-Bold", size: 28) }

        @available(*, deprecated, message: "Use .displayFont(.title)")
        static var title: Font { Font.custom("AvenirNext-Bold", size: 20) }

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
