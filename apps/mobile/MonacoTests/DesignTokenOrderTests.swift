import SwiftUI
import Testing
import UIKit
@testable import Monaco

/// The dark surface ladder, pinned from both sides.
///
/// This is the regression guard for a defect that shipped: `surfaceSunken` served two opposite
/// jobs — a well recessed into a surface, and an inert raised control fill — and in dark it was
/// *lighter* than the card it sat inside. A contrast table cannot catch that; both values cleared
/// AA against the text on them, and the surfaces were still wrong relative to each other. Only an
/// ordering assertion catches it, which is why this file exists separately from
/// `DesignContrastTests`.
struct DesignTokenOrderTests {
    /// Bottom to top. A surface further down this list must be darker in dark mode.
    private static let darkLadder: [(String, Color)] = [
        ("bgBase", MonacoTheme.bgBase),
        ("bgSunken", MonacoTheme.bgSunken),
        ("bgRaised", MonacoTheme.bgRaised),
        ("fillQuiet", MonacoTheme.fillQuiet),
    ]

    /// The gap between adjacent rungs, as a ratio of relative luminances.
    ///
    /// Deliberately **not** a WCAG contrast ratio. WCAG's `(L+0.05)/(L+0.05)` adds a viewing-flare
    /// constant that swamps everything at these luminances — the whole ladder spans 0.0034 to
    /// 0.0168 and every adjacent WCAG ratio lands near 1.06, so a 1.15 WCAG bar would fail the
    /// correct values. A plain luminance ratio is the right measure for "is this surface visibly
    /// above the one behind it", and at 1.15 a rung cannot quietly slide into its neighbour.
    private static let minimumStep = 1.15

    private func luminance(_ color: Color, _ scheme: UIUserInterfaceStyle) -> Double {
        WCAGContrast.luminance(WCAGContrast.resolve(color, scheme))
    }

    @Test func theDarkSurfaceLadderAscendsStrictly() {
        for index in 0..<(Self.darkLadder.count - 1) {
            let (lowerName, lower) = Self.darkLadder[index]
            let (upperName, upper) = Self.darkLadder[index + 1]
            let lowerLuminance = luminance(lower, .dark)
            let upperLuminance = luminance(upper, .dark)
            #expect(
                upperLuminance > lowerLuminance,
                "\(upperName) (\(upperLuminance)) is not above \(lowerName) (\(lowerLuminance)) in dark"
            )
            let step = upperLuminance / lowerLuminance
            #expect(
                step >= Self.minimumStep,
                "\(lowerName) → \(upperName) is only \(step)× in dark: the two rungs have converged"
            )
        }
    }

    /// The specific inversion that shipped. Stated on its own so a failure here names the bug
    /// rather than a generic ordering violation.
    @Test func aWellIsDarkerThanTheCardItSitsInside() {
        for scheme in [UIUserInterfaceStyle.light, .dark] {
            let well = luminance(MonacoTheme.bgSunken, scheme)
            let card = luminance(MonacoTheme.bgRaised, scheme)
            #expect(well < card, "bgSunken (\(well)) is not recessed into bgRaised (\(card))")
        }
    }

    /// A control fill has to read *above* the surface it sits on in dark, which is the opposite of
    /// a well and the reason one token could not do both.
    @Test func aControlFillIsLighterThanTheCardItSitsOnInDark() {
        let control = luminance(MonacoTheme.fillQuiet, .dark)
        let card = luminance(MonacoTheme.bgRaised, .dark)
        #expect(control > card, "fillQuiet (\(control)) does not read as raised on bgRaised (\(card))")
    }

    /// The migration's safety property: every existing call site of `surfaceSunken` is a control
    /// fill, so the alias has to point at `fillQuiet`. Pointed at `bgSunken` instead, the segmented
    /// track, the field fill, the skeleton base and the `StockMark` tile would all silently sink
    /// into the canvas in dark, and nothing else in the suite would say so.
    @Test func surfaceSunkenStillMeansTheControlFill() {
        for scheme in [UIUserInterfaceStyle.light, .dark] {
            let alias = WCAGContrast.resolve(MonacoTheme.surfaceSunken, scheme)
            let quiet = WCAGContrast.resolve(MonacoTheme.fillQuiet, scheme)
            #expect(
                abs(alias.red - quiet.red) < 0.001
                    && abs(alias.green - quiet.green) < 0.001
                    && abs(alias.blue - quiet.blue) < 0.001,
                "surfaceSunken no longer resolves to fillQuiet: the four dependent components moved"
            )
        }
    }

    /// Ink is dark in light mode and dark in dark mode. If an ink token ever starts following the
    /// scheme, "dark means this is your money" stops being true on the screen that says it loudest.
    @Test func inkDoesNotFollowTheScheme() {
        let tokens: [(String, Color)] = [
            ("Ink.base", MonacoTheme.Ink.base),
            ("Ink.raised", MonacoTheme.Ink.raised),
            ("Ink.sunken", MonacoTheme.Ink.sunken),
            ("Ink.accent", MonacoTheme.Ink.accent),
            ("warningOnInk", MonacoTheme.warningOnInk),
            ("brandWashOnInk", MonacoTheme.brandWashOnInk),
        ]
        for (name, token) in tokens {
            let light = WCAGContrast.resolve(token, .light)
            let dark = WCAGContrast.resolve(token, .dark)
            #expect(
                abs(light.red - dark.red) < 0.001
                    && abs(light.green - dark.green) < 0.001
                    && abs(light.blue - dark.blue) < 0.001,
                "\(name) resolves differently per scheme: it is following a paper token"
            )
        }
    }

    /// The ink ladder has the same shape as the paper one: a well below the slab, a card above it.
    @Test func theInkSurfaceLadderAscendsStrictly() {
        let sunken = luminance(MonacoTheme.Ink.sunken, .dark)
        let base = luminance(MonacoTheme.Ink.base, .dark)
        let raised = luminance(MonacoTheme.Ink.raised, .dark)
        #expect(sunken < base, "Ink.sunken (\(sunken)) is not recessed into Ink.base (\(base))")
        #expect(base < raised, "Ink.raised (\(raised)) is not above Ink.base (\(base))")
    }

    /// Green means profit and nothing else. The intent ramp and the money ramp are allowed exactly
    /// one shared value — `danger` is `loss`, because a failed transfer and a losing position are
    /// the same news — and this pins that to one.
    @Test func theIntentRampBorrowsFromTheMoneyRampExactlyOnce() {
        let intents: [(String, Color)] = [
            ("brand", MonacoTheme.brand),
            ("warning", MonacoTheme.warning),
            ("danger", MonacoTheme.danger),
        ]
        let money: [(String, Color)] = [
            ("profit", MonacoTheme.profit),
            ("loss", MonacoTheme.loss),
            ("profitVivid", MonacoTheme.profitVivid),
            ("lossVivid", MonacoTheme.lossVivid),
        ]
        var overlaps: [String] = []
        for (intentName, intent) in intents {
            for (moneyName, moneyColor) in money where sameColour(intent, moneyColor) {
                overlaps.append("\(intentName) == \(moneyName)")
            }
        }
        #expect(overlaps == ["danger == loss"], "intent/money overlap changed: \(overlaps)")
    }

    /// No intent, and no cabal tint, is ever green. This is the countable form of rule 1: a member
    /// must be able to read green on a Monaco screen as "this made money" without first working
    /// out what shape it is in.
    ///
    /// Measured as hue *distance from `profit` itself* rather than against a fixed 140–175° band.
    /// The band is the wrong instrument: it has an arbitrary edge, and sage's `onInk` teal sits at
    /// 174° — inside a 140–175° band and outside a 140–174° one, while being 20° from profit
    /// either way. The distance is the property the rule actually claims, so it is the thing
    /// asserted. The closest anything gets is sage at 20°, a blue-green teal against profit's
    /// green, and sage never draws as a P&L figure, which is the other half of the guarantee.
    @Test func nothingOutsideTheMoneyRampIsGreen() {
        var candidates: [(String, Color)] = [
            ("brand", MonacoTheme.brand),
            ("brandFill", MonacoTheme.brandFill),
            ("warning", MonacoTheme.warning),
            ("danger", MonacoTheme.danger),
            ("Ink.accent", MonacoTheme.Ink.accent),
            ("controlTint", MonacoTheme.controlTint),
        ]
        for tint in MonacoTheme.CabalTint.allCases {
            candidates.append(("\(tint.name).fill", tint.fill))
            candidates.append(("\(tint.name).cta", tint.cta))
            candidates.append(("\(tint.name).onInk", tint.onInk))
        }
        for (name, colour) in candidates {
            for scheme in [UIUserInterfaceStyle.light, .dark] {
                guard
                    let hue = Self.hue(WCAGContrast.resolve(colour, scheme)),
                    let profitHue = Self.hue(WCAGContrast.resolve(MonacoTheme.profit, scheme))
                else { continue }
                let raw = abs(hue - profitHue)
                let distance = min(raw, 360 - raw)
                #expect(
                    distance >= 18,
                    "\(name) is \(distance)° from profit (\(hue)° vs \(profitHue)°): it reads as gain"
                )
            }
        }
    }

    private func sameColour(_ lhs: Color, _ rhs: Color) -> Bool {
        for scheme in [UIUserInterfaceStyle.light, .dark] {
            let a = WCAGContrast.resolve(lhs, scheme)
            let b = WCAGContrast.resolve(rhs, scheme)
            if abs(a.red - b.red) > 0.001 || abs(a.green - b.green) > 0.001 || abs(a.blue - b.blue) > 0.001 {
                return false
            }
        }
        return true
    }

    /// Hue in degrees, or nil for a colour with no chroma to speak of.
    static func hue(_ rgba: WCAGContrast.RGBA) -> Double? {
        let maximum = max(rgba.red, rgba.green, rgba.blue)
        let minimum = min(rgba.red, rgba.green, rgba.blue)
        let delta = maximum - minimum
        guard delta > 0.02 else { return nil }
        var hue: Double
        if maximum == rgba.red {
            hue = 60 * (((rgba.green - rgba.blue) / delta).truncatingRemainder(dividingBy: 6))
        } else if maximum == rgba.green {
            hue = 60 * ((rgba.blue - rgba.red) / delta + 2)
        } else {
            hue = 60 * ((rgba.red - rgba.green) / delta + 4)
        }
        if hue < 0 { hue += 360 }
        return hue
    }
}
