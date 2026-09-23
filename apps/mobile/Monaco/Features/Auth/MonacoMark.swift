import SwiftUI

/// The Monaco mark, drawn live: three equal circles rising left to right, overlaps knocked out,
/// the top-right one in brand blue. Same geometry as the app icon and `LaunchMark`
/// (scripts/design/render-app-icon.swift). Ink on paper in light mode, paper on ink in dark.
///
/// - `monochrome` draws all three circles in one colour, for the mark as a watermark on ink —
///   the 20pt `#FFFFFF`@0.10 in the corner of Home's slab, where a blue circle would read as a
///   third saturated fill on a surface that is allowed one.
/// - `stagger` reveals the circles left to right, 80ms apart, on `settle`. It is the first thing
///   the app does on the sign-in screen and the only animation in the pre-auth flow. Under Reduce
///   Motion the three appear together, with no delay and no animation.
struct MonacoMark: View {
    var size: CGFloat = 88
    var monochrome: Color?
    var stagger = false

    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var revealed = false

    /// Icon geometry in unit space (spec §4): diameter 0.27, centres (0.33, 0.60) (0.50, 0.50) (0.67, 0.40).
    private static let diameter: CGFloat = 0.27
    private static let centres: [CGPoint] = [CGPoint(x: 0.33, y: 0.60), CGPoint(x: 0.50, y: 0.50), CGPoint(x: 0.67, y: 0.40)]
    private static let gap: CGFloat = 14.0 / 1024.0
    /// Width of the mark's bounds in unit space, plus 2% breathing room each side (matches LaunchMark).
    private static let markWidth: CGFloat = (0.67 - 0.33 + diameter) * 1.04

    var body: some View {
        mark
            .frame(width: size, height: size)
            .mask { staggerMask }
            .onAppear { revealed = true }
            .accessibilityHidden(true)
    }

    private var mark: some View {
        Canvas { context, canvasSize in
            let side = min(canvasSize.width, canvasSize.height)
            let unit = side / Self.markWidth
            let origin = CGPoint(x: canvasSize.width / 2 - unit / 2, y: canvasSize.height / 2 - unit / 2)
            let d = Self.diameter * unit
            let g = Self.gap * unit
            let circle = monochrome ?? (colorScheme == .dark ? Color(hex: 0xF3F6FB) : Color(hex: 0x0B1220))
            let accent = monochrome ?? (colorScheme == .dark ? Color(hex: 0x3B7BFF) : Color(hex: 0x1652F0))
            context.drawLayer { layer in
                for (index, c) in Self.centres.enumerated() {
                    let centre = CGPoint(x: origin.x + c.x * unit, y: origin.y + c.y * unit)
                    if index > 0 {
                        var knock = layer
                        knock.blendMode = .clear
                        knock.fill(Path(ellipseIn: CGRect(x: centre.x - d / 2 - g, y: centre.y - d / 2 - g, width: d + 2 * g, height: d + 2 * g)), with: .color(.black))
                    }
                    layer.fill(Path(ellipseIn: CGRect(x: centre.x - d / 2, y: centre.y - d / 2, width: d, height: d)),
                               with: .color(index == 2 ? accent : circle))
                }
            }
        }
    }

    /// Three discs over the drawn mark, each fading in on its own delay. The knockout gap means
    /// the circles never touch, so a per-circle mask has no seam to give itself away.
    @ViewBuilder
    private var staggerMask: some View {
        if stagger {
            GeometryReader { proxy in
                let side = min(proxy.size.width, proxy.size.height)
                let unit = side / Self.markWidth
                let origin = CGPoint(x: proxy.size.width / 2 - unit / 2, y: proxy.size.height / 2 - unit / 2)
                let d = Self.diameter * unit
                ForEach(Array(Self.centres.enumerated()), id: \.offset) { index, c in
                    Circle()
                        .frame(width: d + 2, height: d + 2)
                        .position(x: origin.x + c.x * unit, y: origin.y + c.y * unit)
                        .opacity(isVisible ? 1 : 0)
                        .animation(
                            MonacoMotion.settle
                                .delay(Double(index) * 0.08)
                                .reduced(reduceMotion),
                            value: isVisible
                        )
                }
            }
        } else {
            Rectangle()
        }
    }

    /// Under Reduce Motion there is nothing to wait for: the mark is simply there.
    private var isVisible: Bool { reduceMotion || revealed }
}

#Preview {
    VStack(spacing: 24) {
        MonacoMark(size: 88)
        MonacoMark(size: 24)
        MonacoMark(size: 88, monochrome: .white.opacity(0.10))
            .padding()
            .background(MonacoTheme.Ink.base)
    }
    .padding()
}
