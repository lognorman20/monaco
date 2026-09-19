import SwiftUI

/// The Monaco mark, drawn live: three equal circles rising left to right, overlaps knocked out,
/// the top-right one in profit green. Same geometry as the app icon and `LaunchMark`
/// (scripts/design/render-app-icon.swift). Ink on paper in light mode, paper on ink in dark.
struct MonacoMark: View {
    var size: CGFloat = 88

    @Environment(\.colorScheme) private var colorScheme

    /// Icon geometry in unit space (spec §4): diameter 0.27, centres (0.33, 0.60) (0.50, 0.50) (0.67, 0.40).
    private static let diameter: CGFloat = 0.27
    private static let centres: [CGPoint] = [CGPoint(x: 0.33, y: 0.60), CGPoint(x: 0.50, y: 0.50), CGPoint(x: 0.67, y: 0.40)]
    private static let gap: CGFloat = 14.0 / 1024.0
    /// Width of the mark's bounds in unit space, plus 2% breathing room each side (matches LaunchMark).
    private static let markWidth: CGFloat = (0.67 - 0.33 + diameter) * 1.04

    var body: some View {
        Canvas { context, canvasSize in
            let side = min(canvasSize.width, canvasSize.height)
            let unit = side / Self.markWidth
            let origin = CGPoint(x: canvasSize.width / 2 - unit / 2, y: canvasSize.height / 2 - unit / 2)
            let d = Self.diameter * unit
            let g = Self.gap * unit
            let circle = colorScheme == .dark ? Color(red: 0xF4 / 255, green: 0xF3 / 255, blue: 0xEF / 255) : Color(red: 0x16 / 255, green: 0x16 / 255, blue: 0x13 / 255)
            let green = Color(red: 0x3C / 255, green: 0xCB / 255, blue: 0x7F / 255)
            context.drawLayer { layer in
                for (index, c) in Self.centres.enumerated() {
                    let centre = CGPoint(x: origin.x + c.x * unit, y: origin.y + c.y * unit)
                    if index > 0 {
                        var knock = layer
                        knock.blendMode = .clear
                        knock.fill(Path(ellipseIn: CGRect(x: centre.x - d / 2 - g, y: centre.y - d / 2 - g, width: d + 2 * g, height: d + 2 * g)), with: .color(.black))
                    }
                    layer.fill(Path(ellipseIn: CGRect(x: centre.x - d / 2, y: centre.y - d / 2, width: d, height: d)),
                               with: .color(index == 2 ? green : circle))
                }
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

#Preview {
    VStack(spacing: 24) {
        MonacoMark(size: 88)
        MonacoMark(size: 24)
    }
    .padding()
}
