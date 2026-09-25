import SwiftUI
import Testing
import UIKit
@testable import Monaco

/// WCAG 2.1 contrast over resolved design tokens.
///
/// Tokens are role-based, so repointing one (for example `primaryButtonFill` to the electric-blue
/// brand) can silently push an unrelated pair below AA. This table is the guard.
///
/// It is not exhaustive, and should not be read as a claim that it is. It covers the text, glyph
/// and mark pairs the design system defines roles for; a screen that invents a new combination out
/// of existing tokens is not caught until its pair is added here.
///
/// The old "known gap on purpose" note is gone with the blue brand. It said `brand` as *text* was
/// 4.09:1 on `surfaceSunken` in dark and that nothing drew it there. Both halves were wrong by the
/// time the forest palette landed: `MonacoSectionHeader`, `AssetAboutCard`, `AssetActivityCard` and
/// the balance retry button all draw `brand` as a label, sometimes inside a sunken card. The pair
/// is in the table now, and the forest `brand` clears AA on all three surfaces in both schemes.
enum WCAGContrast {
    struct RGBA {
        var red: Double
        var green: Double
        var blue: Double
        var alpha: Double
    }

    static func resolve(_ color: Color, _ scheme: UIUserInterfaceStyle) -> RGBA {
        let resolved = UIColor(color).resolvedColor(with: UITraitCollection(userInterfaceStyle: scheme))
        var red: CGFloat = 0
        var green: CGFloat = 0
        var blue: CGFloat = 0
        var alpha: CGFloat = 0
        resolved.getRed(&red, green: &green, blue: &blue, alpha: &alpha)
        return RGBA(red: Double(red), green: Double(green), blue: Double(blue), alpha: Double(alpha))
    }

    /// Source-over composite, so a wash is measured on the surface it is drawn over.
    static func composite(_ top: RGBA, over bottom: RGBA) -> RGBA {
        RGBA(
            red: top.red * top.alpha + bottom.red * (1 - top.alpha),
            green: top.green * top.alpha + bottom.green * (1 - top.alpha),
            blue: top.blue * top.alpha + bottom.blue * (1 - top.alpha),
            alpha: 1
        )
    }

    static func luminance(_ rgba: RGBA) -> Double {
        func channel(_ value: Double) -> Double {
            value <= 0.03928 ? value / 12.92 : pow((value + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * channel(rgba.red) + 0.7152 * channel(rgba.green) + 0.0722 * channel(rgba.blue)
    }

    /// `backgrounds` are listed bottom-up: `[.canvas, .profitWash]` is the wash drawn over canvas.
    static func ratio(_ foreground: Color, on backgrounds: [Color], _ scheme: UIUserInterfaceStyle) -> Double {
        var background = resolve(backgrounds[0], scheme)
        background.alpha = 1
        for layer in backgrounds.dropFirst() {
            background = composite(resolve(layer, scheme), over: background)
        }
        let front = composite(resolve(foreground, scheme), over: background)
        let a = luminance(front)
        let b = luminance(background)
        return (max(a, b) + 0.05) / (min(a, b) + 0.05)
    }
}

/// OKLCh over resolved tokens, for the questions WCAG contrast cannot answer.
///
/// Contrast is a ratio against a *background*; it says nothing about whether two foregrounds look
/// like each other. `brand` and `profit` share a hue — the brand is a forest and profit is a green
/// — so the only honest way to assert they cannot be confused is perceptual lightness and chroma,
/// which is what OKLCh gives and HSB does not.
enum OKLCh {
    struct Value {
        /// Perceptual lightness, 0…1.
        var lightness: Double
        /// Chroma — how far from grey. ~0.04 is a tint, ~0.15 is a saturated colour.
        var chroma: Double
        /// Hue angle in degrees.
        var hue: Double
    }

    static func value(_ color: Color, _ scheme: UIUserInterfaceStyle) -> Value {
        let rgba = WCAGContrast.resolve(color, scheme)
        func linear(_ v: Double) -> Double {
            v <= 0.04045 ? v / 12.92 : pow((v + 0.055) / 1.055, 2.4)
        }
        let r = linear(rgba.red), g = linear(rgba.green), b = linear(rgba.blue)
        let l = 0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b
        let m = 0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b
        let s = 0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b
        let l_ = cbrt(l), m_ = cbrt(m), s_ = cbrt(s)
        let lightness = 0.2104542553 * l_ + 0.7936177850 * m_ - 0.0040720468 * s_
        let a = 1.9779984951 * l_ - 2.4285922050 * m_ + 0.4505937099 * s_
        let bb = 0.0259040371 * l_ + 0.7827717662 * m_ - 0.8086757660 * s_
        return Value(
            lightness: lightness,
            chroma: (a * a + bb * bb).squareRoot(),
            hue: (atan2(bb, a) * 180 / .pi).truncatingRemainder(dividingBy: 360)
        )
    }

    /// Euclidean distance in OKLab. Roughly 0.02 is a just-noticeable difference.
    static func distance(_ first: Color, _ second: Color, _ scheme: UIUserInterfaceStyle) -> Double {
        let a = value(first, scheme), b = value(second, scheme)
        let aa = a.chroma * cos(a.hue * .pi / 180), ab = a.chroma * sin(a.hue * .pi / 180)
        let ba = b.chroma * cos(b.hue * .pi / 180), bb = b.chroma * sin(b.hue * .pi / 180)
        return ((a.lightness - b.lightness) * (a.lightness - b.lightness)
            + (aa - ba) * (aa - ba) + (ab - bb) * (ab - bb)).squareRoot()
    }
}

private struct Pair {
    let label: String
    let foreground: Color
    let backgrounds: [Color]
    /// 4.5 for body text, 3 for large text and graphical objects.
    let minimum: Double

    init(_ label: String, _ foreground: Color, on backgrounds: [Color], minimum: Double = 4.5) {
        self.label = label
        self.foreground = foreground
        self.backgrounds = backgrounds
        self.minimum = minimum
    }
}

struct MonacoContrastTests {
    private static let surfaces: [Color] = [MonacoTheme.canvas, MonacoTheme.surface, MonacoTheme.surfaceSunken]

    private static var textPairs: [Pair] {
        var pairs: [Pair] = []
        for surface in surfaces {
            pairs.append(Pair("primaryText", MonacoTheme.primaryText, on: [surface]))
            pairs.append(Pair("secondaryText", MonacoTheme.secondaryText, on: [surface]))
            // Timestamps and other quiet real content. Disabled labels and placeholders are
            // deliberately *not* this token any more — see `disabledLabelStaysQuieterThanContent`.
            pairs.append(Pair("tertiaryText", MonacoTheme.tertiaryText, on: [surface]))
            // Amounts that neither gained nor lost, and their badge.
            pairs.append(Pair("flat", PnLTone.flat.color, on: [surface]))
            pairs.append(Pair("flat on wash", PnLTone.flat.washColor, on: [surface, PnLTone.flat.wash]))
            // Warnings in banners and rows.
            pairs.append(Pair("warning", MonacoTheme.warning, on: [surface]))
            // `destructive` aliases `loss`; listed under its own role so repointing it is caught.
            pairs.append(Pair("destructive", MonacoTheme.destructive, on: [surface]))
            // PnLBadge: signed text on its own wash.
            pairs.append(Pair("profitOnWash", MonacoTheme.profitOnWash, on: [surface, MonacoTheme.profitWash]))
            pairs.append(Pair("lossOnWash", MonacoTheme.lossOnWash, on: [surface, MonacoTheme.lossWash]))
            // Bare signed text, no wash.
            pairs.append(Pair("profit", MonacoTheme.profit, on: [surface]))
            pairs.append(Pair("loss", MonacoTheme.loss, on: [surface]))
            // CircleAction glyph and selected chips.
            pairs.append(Pair("brandOnWash", MonacoTheme.brandOnWash, on: [surface, MonacoTheme.brandWash]))
            // `brand` as a label: "See all", "Show more", "Try again", the selected tab item.
            pairs.append(Pair("brand", MonacoTheme.brand, on: [surface]))
        }
        pairs.append(Pair("onBrand", MonacoTheme.onBrand, on: [MonacoTheme.brandFill]))
        // The same pair through the role aliases, so repointing a role is caught even if the
        // token it aliased stayed put.
        pairs.append(Pair(
            "primaryButtonLabel",
            MonacoTheme.primaryButtonLabel,
            on: [MonacoTheme.primaryButtonFill]
        ))
        pairs.append(Pair(
            "secondaryButtonLabel",
            MonacoTheme.secondaryButtonLabel,
            on: [MonacoTheme.secondaryButtonFill]
        ))
        pairs.append(Pair("onHero", MonacoTheme.onHero, on: [MonacoTheme.heroInk]))
        pairs.append(Pair("onHeroMuted", MonacoTheme.onHeroMuted, on: [MonacoTheme.heroInk]))
        pairs.append(Pair("profitOnHero", MonacoTheme.profitOnHero, on: [MonacoTheme.heroInk]))
        pairs.append(Pair("lossOnHero", MonacoTheme.lossOnHero, on: [MonacoTheme.heroInk]))
        pairs.append(Pair(
            "profitOnHero on wash",
            MonacoTheme.profitOnHero,
            on: [MonacoTheme.heroInk, MonacoTheme.profitWashOnHero]
        ))
        pairs.append(Pair(
            "lossOnHero on wash",
            MonacoTheme.lossOnHero,
            on: [MonacoTheme.heroInk, MonacoTheme.lossWashOnHero]
        ))
        // A flat figure on the hero card, over its own wash.
        pairs.append(Pair("flat on hero", PnLTone.flat.inkCardColor, on: [MonacoTheme.heroInk]))
        pairs.append(Pair(
            "flat on hero wash",
            PnLTone.flat.inkCardColor,
            on: [MonacoTheme.heroInk, PnLTone.flat.inkCardWash]
        ))
        // The toast.
        pairs.append(Pair("toastLabel", MonacoTheme.toastLabel, on: [MonacoTheme.toastFill]))
        return pairs
    }

    /// Graphical objects and large bold text: WCAG 1.4.11 / 1.4.3 ask for 3:1.
    ///
    /// The toast glyphs are held to 4.5 rather than 3 even though they are graphical. They are the
    /// only thing distinguishing a failure toast from a success one for a member who does not read
    /// the copy, they sit over money screens, and they have the headroom — the nearest is 6.2:1.
    /// At a 3:1 bar a retune could halve their contrast and still pass.
    private static var glyphPairs: [Pair] {
        var pairs: [Pair] = [
            Pair("toastSuccessGlyph", MonacoTheme.toastSuccessGlyph, on: [MonacoTheme.toastFill], minimum: 4.5),
            Pair("toastErrorGlyph", MonacoTheme.toastErrorGlyph, on: [MonacoTheme.toastFill], minimum: 4.5),
        ]
        for tint in MonacoTheme.CabalTint.allCases {
            // Bold tile initials, 15pt and up: the large-text bar, not the body one.
            pairs.append(Pair("\(tint) onFill", tint.onFill, on: [tint.fill], minimum: 3))
            // The same mark on a deep ink hero card.
            pairs.append(Pair("\(tint) onInk", tint.onInk, on: [MonacoTheme.heroInk], minimum: 3))
        }
        return pairs
    }

    @Test func adaptiveTokensActuallyResolvePerScheme() {
        // Guards the harness itself: if resolution stopped following the scheme every ratio below
        // would be measured twice in light mode and the table would prove nothing.
        let light = WCAGContrast.resolve(MonacoTheme.canvas, .light)
        let dark = WCAGContrast.resolve(MonacoTheme.canvas, .dark)
        #expect(WCAGContrast.luminance(light) > WCAGContrast.luminance(dark))
    }

    @Test func knownRatioMatchesTheWCAGFormula() {
        let ratio = WCAGContrast.ratio(.white, on: [.black], .light)
        #expect(abs(ratio - 21) < 0.01)
    }

    @Test func textTokensClearAA() {
        for pair in Self.textPairs {
            for scheme in [UIUserInterfaceStyle.light, .dark] {
                let ratio = WCAGContrast.ratio(pair.foreground, on: pair.backgrounds, scheme)
                #expect(
                    ratio >= pair.minimum,
                    "\(pair.label) in \(scheme == .light ? "light" : "dark") is \(ratio), below \(pair.minimum)"
                )
            }
        }
    }

    @Test func glyphTokensClearTheGraphicalMinimum() {
        for pair in Self.glyphPairs {
            for scheme in [UIUserInterfaceStyle.light, .dark] {
                let ratio = WCAGContrast.ratio(pair.foreground, on: pair.backgrounds, scheme)
                #expect(
                    ratio >= pair.minimum,
                    "\(pair.label) in \(scheme == .light ? "light" : "dark") is \(ratio), below \(pair.minimum)"
                )
            }
        }
    }

    /// The rule the whole palette is built around: **green means "this went up"**.
    ///
    /// The brand is a forest and `profit` is a green, so they share a hue — 4° to 8° apart, which
    /// is nothing. Hue cannot separate them and this test does not pretend it can. What separates
    /// them is that the brand reads as *ink* and profit reads as *colour*: profit is markedly
    /// lighter and at least twice the chroma, in both schemes.
    ///
    /// Measured today, with the floors this asserts in brackets:
    ///
    /// | scheme | pair             | ΔL* (≥0.09) | chroma ratio (≥1.9) | ΔE OKLab (≥0.13) |
    /// |--------|------------------|-------------|---------------------|------------------|
    /// | light  | brand vs profit  | 0.136       | 2.16                | 0.152            |
    /// | light  | fill  vs profit  | 0.253       | 3.06                | 0.267            |
    /// | dark   | brand vs profit  | 0.111       | 2.91                | 0.157            |
    /// | dark   | fill  vs profit  | 0.170       | 7.72                | 0.224            |
    ///
    /// For scale: a just-noticeable difference in OKLab is about 0.02, so the closest of these is
    /// seven JNDs apart. The old electric-blue brand managed 0.307 / 0.337 by going to a different
    /// hue entirely; a same-hue palette cannot match that, and these floors are where a retune
    /// would start making a primary button look like a gain.
    @Test func brandAndProfitCannotBeConfused() {
        for scheme in [UIUserInterfaceStyle.light, .dark] {
            let name = scheme == .light ? "light" : "dark"
            let profit = OKLCh.value(MonacoTheme.profit, scheme)
            let text = OKLCh.value(MonacoTheme.primaryText, scheme)
            for (label, token) in [("brand", MonacoTheme.brand), ("brandFill", MonacoTheme.brandFill)] {
                let brand = OKLCh.value(token, scheme)
                let deltaLightness = abs(profit.lightness - brand.lightness)
                // Which side the brand sits on flips with the scheme — it is ink on cream in light
                // and cream on ink in dark, because it follows the surfaces. What does not flip is
                // that it stays on the *text's* side of profit. Asserting "brand is darker than
                // profit" would only have been true in light mode.
                #expect(
                    abs(brand.lightness - text.lightness) < abs(profit.lightness - text.lightness),
                    "\(label) in \(name) is further from primaryText than profit is: it reads as money"
                )
                #expect(
                    deltaLightness >= 0.09,
                    "\(label) vs profit in \(name): ΔL* is \(deltaLightness), under 0.09"
                )
                #expect(
                    profit.chroma / brand.chroma >= 1.9,
                    "\(label) vs profit in \(name): profit is only \(profit.chroma / brand.chroma)× its chroma"
                )
                let delta = OKLCh.distance(token, MonacoTheme.profit, scheme)
                #expect(delta >= 0.13, "\(label) vs profit in \(name): ΔE OKLab is \(delta), under 0.13")
            }
            // And the two money colours from each other. Here hue *is* the separator — and it has
            // to stay one, because red/green is the pair a member reads fastest and the pair a
            // deuteranopic member reads slowest.
            let hueGap = abs(
                OKLCh.value(MonacoTheme.profit, scheme).hue - OKLCh.value(MonacoTheme.loss, scheme).hue
            )
            #expect(min(hueGap, 360 - hueGap) >= 90, "profit and loss in \(name) are \(hueGap)° apart")
            let moneyDelta = OKLCh.distance(MonacoTheme.profit, MonacoTheme.loss, scheme)
            #expect(moneyDelta >= 0.2, "profit vs loss in \(name): ΔE OKLab is \(moneyDelta)")
            // A gain badge must not look like a selected chip either. `brandWash` and `profitWash`
            // land within a few points of each other over cream (`#DCDDD5` against `#D5E1D5`), so
            // the text on them is what carries the difference and it is the text that is pinned.
            let onWash = OKLCh.distance(MonacoTheme.brandOnWash, MonacoTheme.profitOnWash, scheme)
            #expect(onWash >= 0.13, "brandOnWash vs profitOnWash in \(name): ΔE OKLab is \(onWash)")
        }
    }

    /// `disabledLabel` is the one token held *below* AA on purpose, so it needs a two-sided guard:
    /// AA-or-better makes an unavailable control read as a live one, and too low makes it
    /// unreadable. The upper bound is expressed against `tertiaryText` rather than a bare number,
    /// so the two cannot quietly converge back into the single token this split undid.
    @Test func disabledLabelStaysQuieterThanContent() {
        for surface in Self.surfaces {
            for scheme in [UIUserInterfaceStyle.light, .dark] {
                let disabled = WCAGContrast.ratio(MonacoTheme.disabledLabel, on: [surface], scheme)
                let content = WCAGContrast.ratio(MonacoTheme.tertiaryText, on: [surface], scheme)
                #expect(disabled < 4.5, "disabledLabel is \(disabled): a disabled control reads as live")
                #expect(disabled >= 2.5, "disabledLabel is \(disabled): unavailable is not the same as invisible")
                #expect(
                    content - disabled > 1,
                    "tertiaryText (\(content)) and disabledLabel (\(disabled)) have converged"
                )
            }
        }
    }

    /// The toast panel is dark in both schemes, so its glyphs are fixed colours. Aliasing them to
    /// the scheme-adaptive `profitVivid` / `lossVivid` — whose documented job is chart strokes on
    /// paper — is the `primaryButtonFill = brandFill` pattern from #309: the alias looks harmless
    /// until the borrowed token is retuned for the background it was actually written for.
    /// Caught structurally, because a ratio check passes right up until the day it does not.
    @Test func toastGlyphsDoNotFollowTheScheme() {
        for (name, glyph) in [
            ("toastSuccessGlyph", MonacoTheme.toastSuccessGlyph),
            ("toastErrorGlyph", MonacoTheme.toastErrorGlyph),
        ] {
            let light = WCAGContrast.resolve(glyph, .light)
            let dark = WCAGContrast.resolve(glyph, .dark)
            #expect(
                abs(light.red - dark.red) < 0.001
                    && abs(light.green - dark.green) < 0.001
                    && abs(light.blue - dark.blue) < 0.001,
                "\(name) resolves differently per scheme: it is following a paper token again"
            )
        }
    }

    @Test func theToastIsNotTheBrandFill() {
        // The toast borrowed `primaryButton*`, so an error toast read as a second primary button
        // stacked above the real one. Keep the two apart.
        for scheme in [UIUserInterfaceStyle.light, .dark] {
            let toast = WCAGContrast.resolve(MonacoTheme.toastFill, scheme)
            let button = WCAGContrast.resolve(MonacoTheme.primaryButtonFill, scheme)
            let delta = abs(WCAGContrast.luminance(toast) - WCAGContrast.luminance(button))
            #expect(delta > 0.02, "toastFill and primaryButtonFill are nearly the same colour")
        }
    }
}
