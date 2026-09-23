import SwiftUI

/// Which of Monaco's two worlds a surface is in.
///
/// **Paper** is where friends argue about stocks: warm, faced, up to three saturated fills on
/// screen. **Ink** is where money is held: calm, exact, at most one. Dark means *this is your
/// money*; light means *these are your people*.
///
/// A component that lives in both worlds (`MonacoRow`, `PnLBadge`, `MonacoSegmented`, the button
/// styles, `MonacoSectionHeader`) reads `\.monacoWorld` and picks its pair through
/// `MonacoPalette`. A component that only ever lives in one world reads that world's statics
/// directly — there is no reason to pay for a lookup that can only return one answer.
enum MonacoWorld: Equatable, Sendable {
    case paper
    case ink
}

/// The surface, edge and foreground tokens for a world.
///
/// Every value here is an alias of a `MonacoTheme` token — the palette resolves *which* token,
/// never invents a colour. `MonacoContrastTests` measures the underlying tokens, so a pair that
/// passes there passes here.
struct MonacoPalette: Equatable {
    let world: MonacoWorld

    /// The canvas a surface in this world sits on.
    let background: Color

    /// A card in this world.
    let raised: Color

    /// A well recessed into a surface in this world: a chart plot area, an inset row.
    let sunken: Color

    /// An inert raised control fill: a segmented track, a field, a skeleton, an idle chip.
    let quietFill: Color

    /// 1pt separators and card strokes.
    let line: Color

    /// The heavier edge: E2 in dark, a pending vote ring.
    let lineStrong: Color

    let fgPrimary: Color
    let fgMuted: Color
    let fgSubtle: Color

    /// The interactive accent. Blue means tap in both worlds; on ink it is the lighter pair,
    /// because `brand` measures 2.3:1 on `Ink.base` and cannot carry a tappable label there.
    let accent: Color

    /// Carets, spinners and pickers. Never the accent — see `MonacoTheme.controlTint`.
    let controlTint: Color

    /// The wash behind amber text in this world.
    let warningWash: Color

    /// Amber text drawn on `warningWash`.
    let warningOnWash: Color

    static let paper = MonacoPalette(
        world: .paper,
        background: MonacoTheme.bgBase,
        raised: MonacoTheme.bgRaised,
        sunken: MonacoTheme.bgSunken,
        quietFill: MonacoTheme.fillQuiet,
        line: MonacoTheme.line,
        lineStrong: MonacoTheme.lineStrong,
        fgPrimary: MonacoTheme.fgPrimary,
        fgMuted: MonacoTheme.fgMuted,
        fgSubtle: MonacoTheme.fgSubtle,
        accent: MonacoTheme.brand,
        controlTint: MonacoTheme.controlTint,
        warningWash: MonacoTheme.warningWash,
        warningOnWash: MonacoTheme.warningOnWash
    )

    static let ink = MonacoPalette(
        world: .ink,
        background: MonacoTheme.Ink.base,
        raised: MonacoTheme.Ink.raised,
        sunken: MonacoTheme.Ink.sunken,
        // Ink has no inert control fill of its own: a segmented track on ink is a *well*, so it
        // takes `Ink.sunken`. Pointing this at a lighter fill would put a raised control inside a
        // recessed band, which is the exact confusion the paper split just undid.
        quietFill: MonacoTheme.Ink.sunken,
        line: MonacoTheme.Ink.line,
        lineStrong: MonacoTheme.Ink.lineStrong,
        fgPrimary: MonacoTheme.Ink.fgPrimary,
        fgMuted: MonacoTheme.Ink.fgMuted,
        fgSubtle: MonacoTheme.Ink.fgSubtle,
        accent: MonacoTheme.Ink.accent,
        controlTint: MonacoTheme.Ink.fgPrimary,
        warningWash: MonacoTheme.warningWashOnInk,
        warningOnWash: MonacoTheme.warningOnInk
    )

    static func palette(for world: MonacoWorld) -> MonacoPalette {
        switch world {
        case .paper: return .paper
        case .ink: return .ink
        }
    }
}

extension MonacoWorld {
    var palette: MonacoPalette { MonacoPalette.palette(for: self) }

    /// How many saturated fills (a brand fill, a `CabalTint.fill`, a P&L wash, `warningWash` or
    /// `dangerWash`, at 24×24pt or larger) a surface in this world may carry.
    ///
    /// Ink gets one. Paper gets three — but a paper surface that is about *money* rather than
    /// people (a balance card, a receipt, an amount entry) is held to the ink budget, which is
    /// why this takes the question rather than answering it from the world alone.
    func loudnessBudget(isMoneySurface: Bool) -> Int {
        switch self {
        case .ink: return 1
        case .paper: return isMoneySurface ? 1 : 3
        }
    }
}

private struct MonacoWorldKey: EnvironmentKey {
    static let defaultValue: MonacoWorld = .paper
}

extension EnvironmentValues {
    /// The world the surrounding surface is in. Defaults to `.paper`: the app opens into daylight
    /// and ink is the exception, so a component that is never told is on paper.
    var monacoWorld: MonacoWorld {
        get { self[MonacoWorldKey.self] }
        set { self[MonacoWorldKey.self] = newValue }
    }

    /// Convenience for the common `MonacoPalette.palette(for: monacoWorld)` read.
    var monacoPalette: MonacoPalette { monacoWorld.palette }
}

extension View {
    /// Declares the world for this subtree. `.monacoInkBand()` and `.monacoInkSlab()` already do
    /// this; call it directly only for an ink surface built by hand.
    func monacoWorld(_ world: MonacoWorld) -> some View {
        environment(\.monacoWorld, world)
    }
}
