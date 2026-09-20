import SwiftUI
import Testing
import UIKit
@testable import Monaco

/// WCAG 2.1 contrast over resolved design tokens.
///
/// Tokens are role-based, so repointing one (for example `primaryButtonFill` to the electric-blue
/// brand) can silently push an unrelated pair below AA. This table is the guard: every pair the
/// app actually draws is listed here with the surfaces it sits on.
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
            // Timestamps, field placeholders and every disabled button label.
            pairs.append(Pair("tertiaryText", MonacoTheme.tertiaryText, on: [surface]))
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
        // The toast.
        pairs.append(Pair("toastLabel", MonacoTheme.toastLabel, on: [MonacoTheme.toastFill]))
        return pairs
    }

    /// Glyphs are graphical objects: WCAG 1.4.11 asks for 3:1.
    private static var glyphPairs: [Pair] {
        [
            Pair("toastSuccessGlyph", MonacoTheme.toastSuccessGlyph, on: [MonacoTheme.toastFill], minimum: 3),
            Pair("toastErrorGlyph", MonacoTheme.toastErrorGlyph, on: [MonacoTheme.toastFill], minimum: 3),
        ]
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
