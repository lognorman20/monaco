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
/// of existing tokens is not caught until its pair is added here. One known gap on purpose:
/// `brand` as *text* is only 4.09:1 on `surfaceSunken` in dark. Nothing draws it there today —
/// `brand` is a fill and a tint, and `brandOnWash` is the token for brand-coloured text — so
/// asserting it would fail on a combination the app does not use. If a call site ever does draw
/// brand text on a sunken surface, this is where it will need a token of its own.
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
        }
        pairs.append(Pair("onBrand", MonacoTheme.onBrand, on: [MonacoTheme.brandFill]))
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
