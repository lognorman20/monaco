import MonacoCore
import SwiftUI

/// A cabal's identity: saturated tile (tint from the group id) with 1–2 initials in white.
struct CabalMark: View {
    private let tint: MonacoTheme.CabalTint
    private let initials: String
    private let name: String
    private let size: CGFloat
    private let onInk: Bool

    /// `onInk` brightens the tile and drops the initials to deep ink, so the mark still
    /// carries the cabal's identity on a deep ink hero card.
    init(groupId: String, name: String, size: CGFloat = 44, onInk: Bool = false) {
        tint = .forGroupId(groupId)
        initials = CabalMark.initials(for: name)
        self.name = name
        self.size = size
        self.onInk = onInk
    }

    var body: some View {
        RoundedRectangle(cornerRadius: MarkGeometry.radius(for: size), style: .continuous)
            .fill(onInk ? tint.onInk : tint.fill)
            .frame(width: size, height: size)
            .overlay {
                Text(initials)
                    .font(.custom("AvenirNext-DemiBold", fixedSize: size * (initials.count > 1 ? 0.36 : 0.42)))
                    .foregroundStyle(onInk ? MonacoTheme.heroInk : tint.onFill)
                    .lineLimit(1)
                    .minimumScaleFactor(0.5)
                    .padding(size * 0.08)
            }
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

/// A stock's tile: the company's logo when the catalogue knows one, and the ticker
/// on a sunken fill when it does not. "USDC" (cash) shows a dollar sign.
///
/// The ticker tile is the resting state, not a placeholder: it is drawn
/// immediately, a logo replaces it only once one has been decoded, and a logo that
/// 404s or is not an image leaves the tile in place. A row is therefore readable on
/// its first frame whatever the network is doing — which is the whole reason the
/// logo is not an `AsyncImage`.
struct StockMark: View {
    private enum Content {
        case letter(String)
        case symbol(String)
    }

    private let content: Content
    private let size: CGFloat
    private let logoURL: URL?

    init(symbol: String, size: CGFloat = 44, logoURL: URL? = nil) {
        let ticker = AssetSymbolFormatter.display(symbol)
        if ticker.uppercased() == "USDC" {
            content = .symbol("dollarsign")
        } else {
            content = .letter(StockMark.tileText(forTicker: ticker))
        }
        self.size = size
        self.logoURL = logoURL
    }

    /// The whole ticker, up to four characters. One letter is not an identity: nine tickers in
    /// the catalog start with "A", so Apple, Amazon and Broadcom were three identical grey tiles.
    ///
    /// A class separator is dropped rather than left hanging: "BRK.B" cut at four characters is
    /// "BRK." reading as an abbreviation of itself.
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
    static func textScale(for text: String) -> CGFloat {
        switch text.count {
        case 0, 1: return 0.42
        case 2: return 0.34
        case 3: return 0.28
        default: return 0.23
        }
    }

    /// For non-stock rows, e.g. `"cpu"` for a trading bot.
    init(systemImage: String, size: CGFloat = 44) {
        content = .symbol(systemImage)
        self.size = size
        logoURL = nil
    }

    @State private var logo: UIImage?

    private var shape: RoundedRectangle {
        RoundedRectangle(cornerRadius: MarkGeometry.radius(for: size), style: .continuous)
    }

    var body: some View {
        shape
            .fill(MonacoTheme.surfaceSunken)
            .frame(width: size, height: size)
            .overlay {
                shape.strokeBorder(MonacoTheme.hairline, lineWidth: 1)
            }
            .overlay {
                if let logo = logo ?? logoURL.flatMap({ MonacoRemoteImageStore.stockLogos.cachedImage(for: $0) }) {
                    Image(uiImage: logo)
                        .resizable()
                        .scaledToFill()
                        .frame(width: size, height: size)
                        .clipShape(shape)
                } else {
                    tileGlyph
                }
            }
            .accessibilityHidden(true)
            .task(id: logoURL) { await loadLogo() }
    }

    @ViewBuilder
    private var tileGlyph: some View {
        switch content {
        case .letter(let letter):
            Text(letter)
                .font(.system(size: size * StockMark.textScale(for: letter), weight: .semibold))
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.5)
                .padding(.horizontal, size * 0.08)
        case .symbol(let name):
            Image(systemName: name)
                .font(.system(size: size * 0.40, weight: .semibold))
                .foregroundStyle(MonacoTheme.ink)
        }
    }

    private func loadLogo() async {
        logo = nil
        guard let logoURL else { return }
        let store = MonacoRemoteImageStore.stockLogos
        if let cached = store.cachedImage(for: logoURL) {
            logo = cached
            return
        }
        let image = await store.image(for: logoURL)
        guard !Task.isCancelled, let image else { return }
        logo = image
    }
}

private enum MarkGeometry {
    /// `Radius.tile` at 44pt, proportional elsewhere.
    static func radius(for size: CGFloat) -> CGFloat {
        size * MonacoTheme.Radius.tile / 44
    }
}
