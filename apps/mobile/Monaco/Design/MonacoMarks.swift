import MonacoCore
import SwiftUI

/// A cabal's identity: its picture when it has one, otherwise a saturated tile
/// (tint from the group id) with 1–2 initials in white.
///
/// The tinted initials are not a spinner, they are the answer for a cabal with
/// no picture — so they are also what a loading picture shows. The tile is a
/// fixed `size × size` either way, so an arriving picture never moves anything.
struct CabalMark: View {
    private let tint: MonacoTheme.CabalTint
    private let initials: String
    private let name: String
    private let size: CGFloat
    private let onInk: Bool
    private let pictureURL: URL?
    private let accessibilityLabel: String?

    @State private var loadedPicture: UIImage?
    @State private var pictureFailed = false

    /// `onInk` brightens the tile and drops the initials to deep ink, so the mark still
    /// carries the cabal's identity on a deep ink hero card.
    ///
    /// `pictureUrl` is the cabal's picture; blank or unparseable falls back to the
    /// initials. `accessibilityLabel` makes the mark its own VoiceOver element —
    /// pass it where the mark stands alone, and leave it off in a row whose own
    /// label already reads the cabal's name.
    init(
        groupId: String,
        name: String,
        size: CGFloat = 44,
        onInk: Bool = false,
        pictureUrl: String? = nil,
        accessibilityLabel: String? = nil
    ) {
        tint = .forGroupId(groupId)
        initials = CabalMark.initials(for: name)
        self.name = name
        self.size = size
        self.onInk = onInk
        self.pictureURL = CabalMark.resolvedURL(pictureUrl)
        self.accessibilityLabel = accessibilityLabel
    }

    /// Trims and rejects blanks, so an empty string never becomes a URL the
    /// image store retries forever.
    static func resolvedURL(_ raw: String?) -> URL? {
        guard let trimmed = raw?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty else {
            return nil
        }
        return URL(string: trimmed)
    }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: MarkGeometry.radius(for: size), style: .continuous)
        return shape
            .fill(onInk ? tint.onInk : tint.fill)
            .frame(width: size, height: size)
            .overlay {
                // A picture already in the cache draws on the first pass, so a mark
                // scrolling back into a lazy stack never flashes its initials.
                if let picture = loadedPicture ?? cachedPicture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFill()
                        .frame(width: size, height: size)
                        .clipShape(shape)
                        .transition(.opacity)
                } else {
                    initialsLabel
                }
            }
            .clipShape(shape)
            .modifier(CabalMarkAccessibility(label: accessibilityLabel))
            .task(id: pictureURL) { await loadPicture() }
    }

    private var initialsLabel: some View {
        Text(initials)
            .font(.custom("AvenirNext-DemiBold", fixedSize: size * (initials.count > 1 ? 0.36 : 0.42)))
            .foregroundStyle(onInk ? MonacoTheme.heroInk : tint.onFill)
            .lineLimit(1)
            .minimumScaleFactor(0.5)
            .padding(size * 0.08)
    }

    private var cachedPicture: UIImage? {
        pictureURL.flatMap { MonacoAvatarImageStore.shared.cachedImage(for: $0) }
    }

    /// Shares `MonacoAvatarImageStore` with member avatars: it is keyed by URL
    /// and knows nothing about what it is holding, and cabal pictures get a
    /// fresh object key per upload exactly as profile photos do.
    private func loadPicture() async {
        loadedPicture = nil
        pictureFailed = false
        guard let pictureURL else { return }

        let store = MonacoAvatarImageStore.shared
        if let cached = store.cachedImage(for: pictureURL) {
            loadedPicture = cached
            return
        }
        let fetched = await store.image(for: pictureURL)
        guard !Task.isCancelled else { return }
        if let fetched {
            withAnimation(.easeOut(duration: 0.2)) { loadedPicture = fetched }
        } else {
            pictureFailed = true
        }
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

/// A mark with no label of its own stays hidden from VoiceOver, because the row
/// around it already reads the cabal's name. One that was given a label becomes
/// an image element carrying it.
private struct CabalMarkAccessibility: ViewModifier {
    let label: String?

    func body(content: Content) -> some View {
        if let label {
            content
                .accessibilityElement(children: .ignore)
                .accessibilityAddTraits(.isImage)
                .accessibilityLabel(label)
        } else {
            content.accessibilityHidden(true)
        }
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
    }

    var body: some View {
        RoundedRectangle(cornerRadius: MarkGeometry.radius(for: size), style: .continuous)
            .fill(MonacoTheme.surfaceSunken)
            .frame(width: size, height: size)
            .overlay {
                RoundedRectangle(cornerRadius: MarkGeometry.radius(for: size), style: .continuous)
                    .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
            }
            .overlay {
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
            .accessibilityHidden(true)
    }
}

private enum MarkGeometry {
    /// `Radius.tile` at 44pt, proportional elsewhere.
    static func radius(for size: CGFloat) -> CGFloat {
        size * MonacoTheme.Radius.tile / 44
    }
}
