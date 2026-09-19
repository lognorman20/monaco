import SwiftUI
import UIKit

/// Warm paper, ink, and restrained botanical accents inspired by the Orbix reference.
enum MonacoTheme {
    static let background = adaptive(0xF7F8F5, 0x141715)
    static let surface = adaptive(0xFEFFFC, 0x202522)
    static let primaryText = adaptive(0x18221D, 0xF1F4EE)
    static let secondaryText = adaptive(0x606A63, 0xABB7AE)
    static let border = adaptive(0xE2E7DF, 0x364139)
    static let primaryButtonFill = adaptive(0x18221D, 0xE4EEDD)
    static let primaryButtonLabel = adaptive(0xFEFFFC, 0x18221D)
    static let secondaryButtonFill = adaptive(0xEAF0E6, 0x2A352D)
    static let secondaryButtonLabel = primaryText
    static let accent = adaptive(0x285C43, 0xB3D9B0)
    static let disabled = adaptive(0x7B857E, 0x76857A)
    static let success = adaptive(0x216443, 0xB3D9B0)
    static let warning = adaptive(0xA34B30, 0xEEAB8E)
    static let destructive = adaptive(0xAB352D, 0xF1A095)
    static let mint = adaptive(0xE0ECD9, 0x263D2E)
    static let peach = adaptive(0xF5E5D9, 0x433227)
    static let butter = adaptive(0xF0EDD4, 0x3D3C26)

    static func display(_ size: CGFloat) -> Font {
        .custom("AvenirNext-DemiBold", size: size, relativeTo: .title)
    }

    private static func adaptive(_ light: UInt32, _ dark: UInt32) -> Color {
        Color(uiColor: UIColor { traits in
            let hex = traits.userInterfaceStyle == .dark ? dark : light
            return UIColor(red: CGFloat((hex >> 16) & 255) / 255,
                           green: CGFloat((hex >> 8) & 255) / 255,
                           blue: CGFloat(hex & 255) / 255, alpha: 1)
        })
    }
}
