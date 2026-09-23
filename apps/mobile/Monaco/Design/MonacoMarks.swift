import MonacoCore
import SwiftUI

/// A cabal's identity: saturated tile with 1–2 initials in white.
///
/// The tint is never the only identity signal — the initials are always drawn with it, and a
/// caller putting two marks side by side owes them names as well. Where the viewer's own cabal
/// list is in hand, pass the tint `CabalTintAssignment.resolve` handed back rather than the group
/// id: hashing alone lets two of your own cabals land on the same colour.
struct CabalMark: View {
    private let tint: MonacoTheme.CabalTint
    private let initials: String
    private let name: String
    private let size: CGFloat
    private let onInk: Bool
    private let animatesIdentity: Bool

    /// `onInk` brightens the tile and drops the initials to deep ink, so the mark still
    /// carries the cabal's identity on a deep ink hero card.
    init(
        groupId: String,
        name: String,
        size: CGFloat = 44,
        onInk: Bool = false,
        animatesIdentity: Bool = false
    ) {
        self.init(
            tint: .forGroupId(groupId),
            name: name,
            size: size,
            onInk: onInk,
            animatesIdentity: animatesIdentity
        )
    }

    /// The resolved-tint entry point. Every surface that already knows which cabal it belongs to
    /// — the hero, the strip card, the chat toolbar — comes through here.
    ///
    /// `animatesIdentity` is the live recolour of §4 moment #22, and it is **off by default**.
    /// §5.11.1 asks for it on exactly one screen — the create form, where a founder watches the
    /// mark settle as they type — and a mark that crossfades whenever its tint or its initials
    /// change is wrong everywhere else: in a `LazyVStack` or `LazyHStack` a recycled row is handed
    /// a different cabal, and the strip, the leaderboard and the chat toolbar would crossfade one
    /// cabal into another on scroll.
    init(
        tint: MonacoTheme.CabalTint,
        name: String,
        size: CGFloat = 44,
        onInk: Bool = false,
        animatesIdentity: Bool = false
    ) {
        self.tint = tint
        initials = CabalMark.initials(for: name)
        self.name = name
        self.size = size
        self.onInk = onInk
        self.animatesIdentity = animatesIdentity
    }

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        RoundedRectangle(cornerRadius: MarkGeometry.radius(for: size), style: .continuous)
            .fill(onInk ? tint.onInk : tint.fill)
            .frame(width: size, height: size)
            .overlay {
                Text(initials)
                    // SF Pro Bold, not Avenir Next. The display voice is SF Pro's width axis, and
                    // a second typeface at tile scale was the loudest templated signal left.
                    .font(.system(size: size * (initials.count > 1 ? 0.36 : 0.42), weight: .bold))
                    .foregroundStyle(onInk ? MonacoTheme.heroInk : tint.onFill)
                    .lineLimit(1)
                    .minimumScaleFactor(0.5)
                    .padding(size * 0.08)
                    .contentTransition(animatesIdentity ? .opacity : .identity)
            }
            // Recolours live while a founder types a cabal's name, so the tint system is a visible
            // feature at the moment it is first met. Instant swap under Reduce Motion, and off
            // entirely anywhere a mark can be recycled onto a different cabal.
            .animation(animatesIdentity ? MonacoMotion.glide.reduced(reduceMotion) : nil, value: tint)
            .animation(animatesIdentity ? MonacoMotion.glide.reduced(reduceMotion) : nil, value: initials)
            .accessibilityHidden(true)
    }

    /// Connectors and articles never become initials ("Semis or bust" → "SB", not "SO").
    static let skippedWords: Set<String> = ["or", "of", "the", "and", "a", "an", "&", "+", "to", "in", "on", "for", "with", "at", "by"]

    /// First letters of the first and last significant words, uppercased: "Weekend investors" → "WI",
    /// "Semis or bust" → "SB". One significant word gives one letter ("Rent" → "R").
    /// Words that do not start with a letter or digit are ignored; emoji- or punctuation-only names fall
    /// back to their first grapheme. Empty names render an empty tile.
    static func initials(for name: String) -> String {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let words = trimmed
            .split(whereSeparator: { $0.isWhitespace })
            .filter { word in word.first.map { $0.isLetter || $0.isNumber } ?? false }
        let significant = words.filter { !skippedWords.contains($0.lowercased()) }
        let pool = significant.isEmpty ? words : significant
        guard let first = pool.first?.first else {
            return trimmed.first.map { String($0) } ?? ""
        }
        guard pool.count > 1, let last = pool.last?.first else {
            return String(first).uppercased()
        }
        return (String(first) + String(last)).uppercased()
    }
}

/// A stock's tile: sunken fill with a hairline and the ticker. "USDC" (cash) shows a dollar sign.
struct StockMark: View {
    enum Content: Equatable {
        case letter(String)
        case symbol(String)
    }

    private let content: Content
    private let size: CGFloat

    init(symbol: String, size: CGFloat = 44) {
        content = StockMark.content(forSymbol: symbol)
        self.size = size
    }

    /// What the tile draws for a symbol as callers hold it. Most rows pass the wire symbol
    /// (`AAPLc`); a few pass the display ticker already. Both go through
    /// `AssetSymbolFormatter.display` here, once, so the token suffix never reaches the tile and
    /// `display` being idempotent makes the already-normalised callers read the same.
    static func content(forSymbol symbol: String) -> Content {
        let ticker = AssetSymbolFormatter.display(symbol)
        if ticker.uppercased() == "USDC" {
            return .symbol("dollarsign")
        }
        return .letter(tileText(forTicker: ticker))
    }

    /// The whole ticker, up to four characters. One letter is not an identity: nine tickers in
    /// the catalog start with "A", so Apple, Amazon and Broadcom were three identical grey tiles.
    ///
    /// A class separator is dropped rather than left hanging: "BRK.B" cut at four characters is
    /// "BRK." reading as an abbreviation of itself.
    ///
    /// Takes the display ticker, not the wire symbol: `content(forSymbol:)` strips the token
    /// suffix before it gets here.
    static func tileText(forTicker ticker: String) -> String {
        let trimmed = ticker.trimmingCharacters(in: .whitespacesAndNewlines).uppercased()
        var tile = String(trimmed.prefix(4))
        while let last = tile.last, last == "." || last == "-" {
            tile.removeLast()
        }
        return tile
    }

    /// Fraction of the tile the text is set at. Longer tickers are set smaller so the tile keeps
    /// its weight; `minimumScaleFactor` takes the rest.
    ///
    /// Retuned for SF Pro **Expanded** Bold, whose glyphs are wider than the semibold standard
    /// width this used to draw: the three-character ticker — the modal case in the catalog —
    /// lands on the spec's 0.30 × tile, and the ladder stays strictly descending either side of
    /// it so a four-character ticker still fits without leaning on the scale factor.
    static func textScale(for text: String) -> CGFloat {
        switch text.count {
        case 0, 1: return 0.40
        case 2: return 0.34
        case 3: return 0.30
        default: return 0.24
        }
    }

    /// For non-stock rows, e.g. `"cpu"` for a trading bot.
    init(systemImage: String, size: CGFloat = 44) {
        content = .symbol(systemImage)
        self.size = size
    }

    /// The tile lives in both worlds — a holdings row on paper, a proposal deck card on ink — so
    /// it resolves its fill, its edge and its ticker colour from the world it was placed in
    /// rather than assuming paper and drawing a light grey square on a deep ink band.
    @Environment(\.monacoPalette) private var palette

    var body: some View {
        RoundedRectangle(cornerRadius: MarkGeometry.radius(for: size), style: .continuous)
            .fill(palette.quietFill)
            .frame(width: size, height: size)
            .overlay {
                RoundedRectangle(cornerRadius: MarkGeometry.radius(for: size), style: .continuous)
                    .strokeBorder(palette.line, lineWidth: 1)
            }
            .overlay {
                switch content {
                case .letter(let letter):
                    Text(letter)
                        // Expanded bold: a four-character ticker on a quiet fill is a handsome
                        // resting state, and it is the same width axis the display voice uses.
                        .font(
                            .system(size: size * StockMark.textScale(for: letter), weight: .bold)
                                .width(.expanded)
                        )
                        .foregroundStyle(palette.fgPrimary)
                        .lineLimit(1)
                        .minimumScaleFactor(0.5)
                        .padding(.horizontal, size * 0.06)
                case .symbol(let name):
                    Image(systemName: name)
                        .font(.system(size: size * 0.40, weight: .semibold))
                        .foregroundStyle(palette.fgPrimary)
                }
            }
            .accessibilityHidden(true)
    }
}

private enum MarkGeometry {
    /// `Radius.tile` at 44pt, proportional elsewhere.
    static func radius(for size: CGFloat) -> CGFloat {
        size * MonacoTheme.Radius.tile / 44
    }
}
