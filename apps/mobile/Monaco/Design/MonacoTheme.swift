import SwiftUI

/// Monaco shared color tokens — navy/teal investing palette, WCAG-minded contrast.
///
/// Import: `MonacoTheme.<token>` in any view. Prefer these over raw `.primary` / `.secondary`
/// when sibling agents touch screens.
///
/// Token list:
/// - `background` — app canvas behind screens
/// - `surface` — cards, sheets, nav bars, list rows
/// - `primaryText` — headings and body copy
/// - `secondaryText` — captions, hints, footnotes
/// - `border` — dividers, outlines, separators
/// - `primaryButtonFill` — filled button background (`.borderedProminent`)
/// - `primaryButtonLabel` — label on primary buttons
/// - `secondaryButtonFill` — secondary button background
/// - `secondaryButtonLabel` — secondary button text / outline
/// - `destructive` — delete and irreversible actions
/// - `accent` — tint, links, highlights (matches AccentColor asset)
/// - `disabled` — muted controls and placeholders
/// - `success` — positive status (optional semantic alias)
/// - `warning` — caution status (optional semantic alias)
enum MonacoTheme {
    static let background = Color.adaptive(
        light: rgb(0.957, 0.965, 0.976), // #F4F6F9
        dark: rgb(0.043, 0.086, 0.157)   // #0B1628
    )

    static let surface = Color.adaptive(
        light: rgb(1.0, 1.0, 1.0),       // #FFFFFF
        dark: rgb(0.082, 0.133, 0.220)   // #152238
    )

    static let primaryText = Color.adaptive(
        light: rgb(0.043, 0.086, 0.157), // #0B1628 — ~14:1 on background
        dark: rgb(0.957, 0.965, 0.976)   // #F4F6F9
    )

    static let secondaryText = Color.adaptive(
        light: rgb(0.239, 0.310, 0.400), // #3D4F66 — ~7:1 on background
        dark: rgb(0.659, 0.706, 0.769)   // #A8B4C4
    )

    static let border = Color.adaptive(
        light: rgb(0.773, 0.808, 0.851), // #C5CED9
        dark: rgb(0.180, 0.251, 0.345)    // #2E4058
    )

    static let primaryButtonFill = Color.adaptive(
        light: rgb(0.043, 0.333, 0.376), // #0B5560 — ~7:1 with white label
        dark: rgb(0.082, 0.569, 0.608)    // #14919B
    )

    static let primaryButtonLabel = Color.adaptive(
        light: rgb(1.0, 1.0, 1.0),
        dark: rgb(1.0, 1.0, 1.0)
    )

    static let secondaryButtonFill = Color.adaptive(
        light: rgb(0.957, 0.965, 0.976),
        dark: rgb(0.082, 0.133, 0.220)
    )

    static let secondaryButtonLabel = Color.adaptive(
        light: rgb(0.043, 0.333, 0.376),
        dark: rgb(0.659, 0.706, 0.769)
    )

    static let destructive = Color.adaptive(
        light: rgb(0.706, 0.137, 0.094), // #B42318
        dark: rgb(0.976, 0.443, 0.400)    // #F97066
    )

    static let accent = Color.adaptive(
        light: rgb(0.055, 0.455, 0.565), // #0E7490
        dark: rgb(0.133, 0.722, 0.812)   // #22B8CF
    )

    static let disabled = Color.adaptive(
        light: rgb(0.596, 0.635, 0.702), // #98A2B3
        dark: rgb(0.420, 0.447, 0.502)
    )

    static let success = Color.adaptive(
        light: rgb(0.024, 0.463, 0.278), // #067647
        dark: rgb(0.290, 0.871, 0.502)
    )

    static let warning = Color.adaptive(
        light: rgb(0.710, 0.278, 0.031), // #B54708
        dark: rgb(0.992, 0.702, 0.082)
    )

    private static func rgb(_ red: Double, _ green: Double, _ blue: Double) -> Color {
        Color(red: red, green: green, blue: blue)
    }
}

private extension Color {
    static func adaptive(light: Color, dark: Color) -> Color {
        Color(
            uiColor: UIColor { traits in
                traits.userInterfaceStyle == .dark ? UIColor(dark) : UIColor(light)
            }
        )
    }
}
