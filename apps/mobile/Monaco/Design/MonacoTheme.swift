import SwiftUI

/// Shared visual tokens. Grayscale paper sheet, gray wash at the top, ink pills.
/// Green and red are reserved for profit and loss figures.
///
/// - `background` / `canvas` — light gray sheet
/// - `canvasWash` — gray bloom behind the status bar / nav (not a full fill)
/// - `surface` — cards, sheets, tab bar, list rows
/// - `primaryText` / `ink` — headings and body
/// - `secondaryText` / `muted` — captions
/// - `border` / `hairline` — 1pt separators
/// - `primaryButtonFill` — ink pill fill
/// - `accent` — ink for chips, icons, and carets
/// - `profit` / `loss` — signed P&L and percent figures only
enum MonacoTheme {
    static let background = Color.adaptive(
        light: rgb(0.957, 0.957, 0.957), // #F4F4F4
        dark: rgb(0.090, 0.090, 0.090)    // #171717
    )

    static let canvas = background

    /// Soft gray at the top of the sheet; fades to `canvas`.
    static let canvasWash = Color.adaptive(
        light: rgb(0.784, 0.784, 0.784), // #C8C8C8
        dark: rgb(0.220, 0.220, 0.220)
    )

    static let surface = Color.adaptive(
        light: rgb(0.980, 0.980, 0.980), // #FAFAFA
        dark: rgb(0.145, 0.145, 0.145)   // #252525
    )

    static let primaryText = Color.adaptive(
        light: rgb(0.086, 0.086, 0.086), // #161616
        dark: rgb(0.949, 0.949, 0.949)
    )

    static let ink = primaryText

    static let secondaryText = Color.adaptive(
        light: rgb(0.431, 0.431, 0.431), // #6E6E6E
        dark: rgb(0.639, 0.639, 0.639)
    )

    static let muted = secondaryText

    static let border = Color.adaptive(
        light: rgb(0.831, 0.831, 0.831), // #D4D4D4
        dark: rgb(0.227, 0.227, 0.227)
    )

    static let hairline = border

    static let primaryButtonFill = Color.adaptive(
        light: rgb(0.086, 0.086, 0.086),
        dark: rgb(0.949, 0.949, 0.949)
    )

    static let primaryButtonLabel = Color.adaptive(
        light: rgb(0.980, 0.980, 0.980),
        dark: rgb(0.086, 0.086, 0.086)
    )

    static let secondaryButtonFill = surface

    static let secondaryButtonLabel = primaryText

    static let destructive = Color.adaptive(
        light: rgb(0.706, 0.137, 0.094), // #B42318
        dark: rgb(0.976, 0.443, 0.400)
    )

    static let accent = ink

    static let disabled = Color.adaptive(
        light: rgb(0.620, 0.620, 0.620),
        dark: rgb(0.450, 0.450, 0.450)
    )

    static let profit = Color.adaptive(
        light: rgb(0.024, 0.463, 0.278), // #067647
        dark: rgb(0.290, 0.871, 0.502)
    )

    static let loss = Color.adaptive(
        light: rgb(0.706, 0.137, 0.094), // #B42318
        dark: rgb(0.976, 0.443, 0.400)
    )

    static let success = profit

    static let warning = Color.adaptive(
        light: rgb(0.349, 0.349, 0.349),
        dark: rgb(0.753, 0.753, 0.753)
    )

    /// Green for gains, red for losses, muted for zero / missing.
    static func signed(_ raw: String?) -> Color {
        let trimmed = raw?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if trimmed.isEmpty || trimmed == "—" {
            return muted
        }
        if trimmed.hasPrefix("-") {
            return loss
        }
        if trimmed.hasPrefix("+") {
            let magnitude = trimmed.drop(while: { !$0.isNumber && $0 != "." })
            if magnitude.isEmpty || magnitude.allSatisfy({ $0 == "0" || $0 == "." }) {
                return muted
            }
            return profit
        }
        let numeric = trimmed
            .replacingOccurrences(of: "%", with: "")
            .replacingOccurrences(of: ",", with: "")
        if let value = Double(numeric) {
            if value > 0 { return profit }
            if value < 0 { return loss }
        }
        return muted
    }

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
