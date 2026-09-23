import SwiftUI

extension View {
    /// Resolves the adaptive *paper* tokens inside this subtree at their dark values.
    ///
    /// Ink is dark in both colour schemes (§1.5) — that is the whole point of it. A component
    /// built from the paper ramp is therefore only correct on ink when it resolves dark: in light
    /// mode a `StockMark` tile is `#EDF1F7` with near-black type, which on a `#0B1220` band is a
    /// bright rectangle with invisible letters in it. Every paper token is a dynamic `UIColor`
    /// (`Color.adaptive`), so overriding the scheme for the subtree resolves them the one way the
    /// surface can carry. The fixed `Ink.*` and `Color(hex:)` values are unaffected, and so are
    /// the money and intent ramps' on-ink pairs.
    ///
    /// Use it only inside `.monacoInkBand()` / `.monacoInkSlab()`, and only for a component this
    /// chunk does not own — a component that reads `\.monacoWorld` needs nothing from this.
    func monacoInkScheme() -> some View {
        environment(\.colorScheme, .dark)
    }
}
