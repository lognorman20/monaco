import SwiftUI

/// Shared visual tokens. Orbix food-app grammar on a social-investing palette:
/// paper-white canvas, kiln wash at the top, ink pills. Sibling screens import
/// these; they do not invent local colors.
///
/// - `background` / `canvas` — paper white sheet
/// - `canvasWash` — peach bloom behind the status bar / nav (not a full fill)
/// - `surface` — cards, sheets, tab bar, list rows
/// - `primaryText` / `ink` — headings and body
/// - `secondaryText` / `muted` — captions
/// - `border` / `hairline` — 1pt separators
/// - `primaryButtonFill` — ink pill fill
/// - `accent` — kiln clay for chips and links (not teal, not purple)
enum MonacoTheme {
    static let background = Color.adaptive(
        light: rgb(0.992, 0.990, 0.986), // #FDFCFB paper white
        dark: rgb(0.090, 0.078, 0.067)    // #171410
    )

    static let canvas = background

    /// Soft peach at the top of the sheet; fades to `canvas`. Kiln family, not purple.
    static let canvasWash = Color.adaptive(
        light: rgb(0.980, 0.890, 0.800), // #FAE3CC
        dark: rgb(0.220, 0.120, 0.070)
    )

    static let surface = Color.adaptive(
        light: rgb(1.000, 1.000, 1.000), // #FFFFFF cards on paper
        dark: rgb(0.145, 0.129, 0.114)   // #25211D
    )

    static let primaryText = Color.adaptive(
        light: rgb(0.110, 0.094, 0.078), // #1C1814
        dark: rgb(0.965, 0.945, 0.918)
    )

    static let ink = primaryText

    static let secondaryText = Color.adaptive(
        light: rgb(0.420, 0.380, 0.345), // #6B6158
        dark: rgb(0.720, 0.680, 0.640)
    )

    static let muted = secondaryText

    static let border = Color.adaptive(
        light: rgb(0.855, 0.820, 0.780), // #DAD1C7
        dark: rgb(0.280, 0.250, 0.220)
    )

    static let hairline = border

    static let primaryButtonFill = Color.adaptive(
        light: rgb(0.110, 0.094, 0.078),
        dark: rgb(0.965, 0.945, 0.918)
    )

    static let primaryButtonLabel = Color.adaptive(
        light: rgb(0.988, 0.976, 0.961),
        dark: rgb(0.110, 0.094, 0.078)
    )

    static let secondaryButtonFill = surface

    static let secondaryButtonLabel = primaryText

    static let destructive = Color.adaptive(
        light: rgb(0.706, 0.137, 0.094), // #B42318
        dark: rgb(0.976, 0.443, 0.400)
    )

    static let accent = Color.adaptive(
        light: rgb(0.769, 0.361, 0.149), // #C45C26 kiln clay
        dark: rgb(0.890, 0.545, 0.325)
    )

    static let disabled = Color.adaptive(
        light: rgb(0.620, 0.580, 0.545),
        dark: rgb(0.450, 0.420, 0.390)
    )

    static let success = Color.adaptive(
        light: rgb(0.024, 0.463, 0.278), // #067647
        dark: rgb(0.290, 0.871, 0.502)
    )

    static let warning = Color.adaptive(
        light: rgb(0.710, 0.278, 0.031), // #B54708
        dark: rgb(0.992, 0.702, 0.082)
    )

    enum Radius {
        static let chip: CGFloat = 20
        static let card: CGFloat = 24
        static let sheet: CGFloat = 28
        static let pill: CGFloat = 28
    }

    enum Space {
        static let s: CGFloat = 8
        static let m: CGFloat = 16
        static let l: CGFloat = 24
    }

    enum TypeRole {
        static let display = Font.custom("AvenirNext-Bold", size: 28)
        static let title = Font.custom("AvenirNext-DemiBold", size: 20)
        static let body = Font.system(.body)
        static let caption = Font.system(.footnote)
    }

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
