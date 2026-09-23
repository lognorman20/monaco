import SwiftUI

/// The measurements of an ink band, in one place so the band, the slab and their tests agree.
enum MonacoInkBandMetrics {
    /// Vertical padding inside the band.
    static let verticalPadding: CGFloat = 24

    /// Clearance above and below a full-bleed band, so it reads as a separate object rather than
    /// a section that happens to be dark.
    static let clearance: CGFloat = MonacoTheme.Space.band

    /// The slab's bottom corners. Paper slides up over this edge.
    static let slabBottomRadius: CGFloat = MonacoTheme.Radius.object

    /// The 1pt rule at the top and bottom of a band.
    static let edgeWidth: CGFloat = 1

    /// The radial highlight the ink surfaces share.
    static let highlightCenter = UnitPoint(x: 0.08, y: -0.05)
    static let highlightRadius: CGFloat = 340
}

extension View {
    /// A full-bleed ink section inside a paper screen.
    ///
    /// Negative gutter padding so it reaches both edges, no radius, a 1pt `#FFFFFF`@0.06 rule top
    /// and bottom, 24pt internal vertical padding, 40pt clearance above and below. Sets
    /// `\.monacoWorld` to `.ink` for everything inside, so a `MonacoRow` or a `PnLBadge` in there
    /// picks the ink pair without being told twice.
    ///
    /// **At most one band or slab per screen**, and it carries that screen's most important
    /// object. Nothing else in the app is full-bleed, which is the whole reason it reads.
    func monacoInkBand() -> some View {
        modifier(MonacoInkBandModifier())
    }

    /// The screen-opening variant: full-bleed, under the status bar, square top corners and a
    /// 28pt bottom radius, so paper slides up over its bottom edge.
    ///
    /// Home and stock detail open on this. One screenshot then says "dark is your money, light is
    /// your people" with no copy at all.
    func monacoInkSlab() -> some View {
        modifier(MonacoInkSlabModifier())
    }
}

private struct MonacoInkBandModifier: ViewModifier {
    func body(content: Content) -> some View {
        content
            .monacoWorld(.ink)
            .foregroundStyle(MonacoTheme.Ink.fgPrimary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.vertical, MonacoInkBandMetrics.verticalPadding)
            .background(InkSurface())
            .overlay(alignment: .top) { edge }
            .overlay(alignment: .bottom) { edge }
            // Full-bleed: cancel the screen gutter the band sits inside. Applied *after* the
            // background so the fill reaches the screen edge and the content does not.
            .padding(.horizontal, -MonacoTheme.Space.gutter)
            .padding(.vertical, MonacoInkBandMetrics.clearance)
    }

    private var edge: some View {
        Rectangle()
            .fill(MonacoTheme.Ink.edge)
            .frame(height: MonacoInkBandMetrics.edgeWidth)
    }
}

private struct MonacoInkSlabModifier: ViewModifier {
    private var shape: UnevenRoundedRectangle {
        UnevenRoundedRectangle(
            bottomLeadingRadius: MonacoInkBandMetrics.slabBottomRadius,
            bottomTrailingRadius: MonacoInkBandMetrics.slabBottomRadius,
            style: .continuous
        )
    }

    func body(content: Content) -> some View {
        content
            .monacoWorld(.ink)
            .foregroundStyle(MonacoTheme.Ink.fgPrimary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.bottom, MonacoInkBandMetrics.verticalPadding)
            .background {
                InkSurface(shape: AnyShape(shape))
                    // The slab runs behind the status bar: its fill ignores the top safe area
                    // while its content stays inside it.
                    .ignoresSafeArea(edges: .top)
            }
            .padding(.horizontal, -MonacoTheme.Space.gutter)
    }
}

/// `Ink.base`, the shared radial highlight, and a hairline edge. The one place the ink surface is
/// drawn, so the band, the slab and any hand-built ink card cannot drift apart.
struct InkSurface: View {
    var shape: AnyShape = AnyShape(Rectangle())

    var body: some View {
        shape
            .fill(MonacoTheme.Ink.base)
            .overlay {
                shape.fill(
                    RadialGradient(
                        colors: [MonacoTheme.Ink.highlight, .clear],
                        center: MonacoInkBandMetrics.highlightCenter,
                        startRadius: 0,
                        endRadius: MonacoInkBandMetrics.highlightRadius
                    )
                )
            }
            .overlay {
                shape.stroke(Color.white.opacity(0.07), lineWidth: MonacoInkBandMetrics.edgeWidth)
            }
    }
}
