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
    ///
    /// **This is a bridge and it is meant to be deleted.** It buys the right colours by lying
    /// about the scheme, and the lie has two edges: anything inside that reads `\.colorScheme` for
    /// logic rather than for colour is told the device is in dark mode when it is not, and it does
    /// not reach `UITextView`'s keyboard appearance, which is UIKit's and reads the real trait.
    /// Nothing inside the four call sites does either today — they are marks, figures and one
    /// `MonacoTextField` — which is why it ships.
    ///
    /// The root fix belongs to the chunks that own the components: `AmountEntry` (Chunk C) and
    /// `StockMark` (Chunk A / `MonacoMarks`, Chunk D) read `\.monacoWorld` and pick their own ink
    /// pair, the way `MonacoRow` and `PnLBadge` already do. When they do, this file and its four
    /// call sites — `ProposeInkReceipt`, the sell amount band, and the two receipts — delete
    /// together, and no `\.colorScheme` is overridden anywhere in the app. **It must not survive
    /// into the integration branch**; it is a follow-up on Chunk E's PR, not a paragraph in it.
    func monacoInkScheme() -> some View {
        environment(\.colorScheme, .dark)
    }
}
