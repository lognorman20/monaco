import SwiftUI

/// Four steps, and one rule: **light carries elevation with shadow, dark carries it with stroke.**
///
/// Swapping the light-mode hairline for a shadow is what separates a card from a box, and it is
/// the single change that stops a screen reading as "one card, forty times". In dark a shadow is
/// invisible, so the edge does the work instead.
///
/// There is no second raised fill colour. E2 is the *same* fill with a stronger edge — a heavier
/// shadow in light, `lineStrong` in dark. That removes a token, and removes the collision it would
/// have had with `fillQuiet`.
///
/// Shadows exist in exactly three places app-wide: E1, E2 and the toast. Nowhere else.
enum MonacoElevation {
    /// E1 — every card and grouped list.
    case card
    /// E2 — the one object on a screen that outranks the others. If two things on a screen are
    /// E2, neither of them is.
    case raised

    var shadowColor: Color {
        switch self {
        case .card: return .black.opacity(0.05)
        case .raised: return .black.opacity(0.10)
        }
    }

    var shadowRadius: CGFloat {
        switch self {
        case .card: return 12
        case .raised: return 24
        }
    }

    var shadowOffsetY: CGFloat {
        switch self {
        case .card: return 3
        case .raised: return 8
        }
    }

    /// The dark-mode edge. `line` for a card, `lineStrong` for a raised object.
    var darkStroke: Color {
        switch self {
        case .card: return MonacoTheme.line
        case .raised: return MonacoTheme.lineStrong
        }
    }

    /// The ink-world edge, for an elevated card sitting inside an ink band.
    var inkStroke: Color {
        switch self {
        case .card: return MonacoTheme.Ink.line
        case .raised: return MonacoTheme.Ink.lineStrong
        }
    }
}

extension View {
    /// Gives this content a raised surface at the given step.
    ///
    /// The fill, the radius, the shadow and the stroke all come from here, so a card never grows
    /// a hand-rolled hairline of its own. The shadow is drawn on the card's shape only, never on
    /// clipped content, so a long lazy list does not pay for offscreen rendering.
    func monacoElevation(
        _ elevation: MonacoElevation,
        radius: CGFloat = MonacoTheme.Radius.container
    ) -> some View {
        modifier(MonacoElevationModifier(elevation: elevation, radius: radius))
    }
}

private struct MonacoElevationModifier: ViewModifier {
    let elevation: MonacoElevation
    let radius: CGFloat

    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.monacoWorld) private var world

    private var shape: RoundedRectangle {
        RoundedRectangle(cornerRadius: radius, style: .continuous)
    }

    /// Ink is dark in both schemes, so a card inside an ink band takes the stroke treatment even
    /// in light mode. Asking `colorScheme` alone would put a black shadow on a black band.
    private var carriesShadow: Bool {
        world == .paper && colorScheme == .light
    }

    private var stroke: Color? {
        switch world {
        case .ink: return elevation.inkStroke
        case .paper: return colorScheme == .dark ? elevation.darkStroke : nil
        }
    }

    func body(content: Content) -> some View {
        content
            .background {
                shape
                    .fill(world == .ink ? MonacoTheme.Ink.raised : MonacoTheme.bgRaised)
                    .shadow(
                        color: carriesShadow ? elevation.shadowColor : .clear,
                        radius: carriesShadow ? elevation.shadowRadius : 0,
                        y: carriesShadow ? elevation.shadowOffsetY : 0
                    )
            }
            .overlay {
                if let stroke {
                    shape.strokeBorder(stroke, lineWidth: 1)
                }
            }
    }
}
