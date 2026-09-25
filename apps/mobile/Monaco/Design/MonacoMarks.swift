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
        pictureURL.flatMap { MonacoRemoteImageStore.avatars.cachedImage(for: $0) }
    }

    /// Shares `MonacoRemoteImageStore.avatars` with member avatars: it is keyed by URL
    /// and knows nothing about what it is holding, and cabal pictures get a
    /// fresh object key per upload exactly as profile photos do.
    private func loadPicture() async {
        loadedPicture = nil
        pictureFailed = false
        guard let pictureURL else { return }

        let store = MonacoRemoteImageStore.avatars
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

    init(symbol: String, displayName: String? = nil, assetKind: AssetKind = .stock, size: CGFloat = 44, logoURL: URL? = nil) {
        let ticker = AssetSymbolFormatter.display(symbol, kind: assetKind)
        if ticker.uppercased() == "USDC" {
            content = .symbol("dollarsign")
        } else if assetKind == .preIpo, let displayName, let first = displayName.first {
            content = .letter(String(first).uppercased())
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

    /// A coin, not a tile. These are tokenised stocks, and a disc reads as one at a glance.
    private var shape: Circle { Circle() }

    /// The coin's face. Gold, because these are tokens — a grey disc read as a disabled
    /// control, and the warm face also separates a stock from a cabal's tinted tile at a
    /// glance. Lit from the top left, so the face has a direction and does not read as a flat
    /// swatch. Dark mode drops the luminance rather than the hue so it still reads as metal.
    private static var coinFace: LinearGradient {
        LinearGradient(
            colors: [
                Color.adaptive(light: 0xFCF5E4, dark: 0x4A4030),
                Color.adaptive(light: 0xEBD9A8, dark: 0x2E2719),
            ],
            startPoint: .topLeading,
            endPoint: .bottomTrailing
        )
    }

    /// The rim. Metal is not one colour: a real rim catches the light at two points and falls
    /// into shadow at the two between them, which is what makes a disc read as struck rather
    /// than drawn. An angular sweep around the circle is the cheapest honest way to say that —
    /// highlight at the top left, shadow at the top right and bottom left, a second, weaker
    /// highlight at the bottom right where the light bounces back.
    private static var coinRim: AngularGradient {
        AngularGradient(
            stops: [
                .init(color: Color.adaptive(light: 0xFFF6DC, dark: 0x8A7648), location: 0.00),
                .init(color: Color.adaptive(light: 0xB08E3E, dark: 0x4A3F26), location: 0.20),
                .init(color: Color.adaptive(light: 0xE8CE86, dark: 0x6F5F3A), location: 0.42),
                .init(color: Color.adaptive(light: 0xA8873A, dark: 0x453A22), location: 0.62),
                .init(color: Color.adaptive(light: 0xF3E4B4, dark: 0x7D6B42), location: 0.82),
                .init(color: Color.adaptive(light: 0xFFF6DC, dark: 0x8A7648), location: 1.00),
            ],
            center: .center,
            angle: .degrees(-135)
        )
    }

    /// Thinner than a hairline separator on purpose: at 0.33pt the rim is a single device pixel
    /// on a 3x screen, so it describes the coin's edge without drawing a ring around the logo.
    private static let rimWidth: CGFloat = 0.33

    /// The issuer's artwork frames every company logo with four grey arrows that reach about
    /// 16% in from each edge, so drawn whole they show as triangles poking out around the mark.
    /// Showing the middle of the image crops the frame away.
    ///
    /// 0.70 is measured, not guessed: the arrows on the tightest logo (Alphabet) end at 16%,
    /// and Amazon's swoosh starts being clipped below about 0.70. It applies to the remote
    /// artwork only — a logo from anywhere else is drawn as it comes.
    private static let issuerArtworkVisibleFraction: CGFloat = 0.70

    /// How much of the disc the mark itself occupies. Most issuer logos are solid squares —
    /// Apple's black tile, Tesla's red one — not marks on transparency, so they are clipped to
    /// the coin and this cannot usefully exceed the largest square a circle holds (0.707).
    /// 0.72 fills the face to its edge and lets the rim, not a ring of fill, be the border.
    private static let markInset: CGFloat = 0.72

    /// Centre-crops the issuer's arrow frame away. Done once when the image loads, not on every
    /// frame. An image too small to crop is returned untouched rather than upscaled.
    static func croppedToMark(_ image: UIImage) -> UIImage {
        let side = min(image.size.width, image.size.height) * issuerArtworkVisibleFraction
        guard side > 1, let cgImage = image.cgImage else { return image }
        let scale = image.scale
        let rect = CGRect(
            x: ((image.size.width - side) / 2) * scale,
            y: ((image.size.height - side) / 2) * scale,
            width: side * scale,
            height: side * scale
        )
        guard let cropped = cgImage.cropping(to: rect) else { return image }
        return UIImage(cgImage: cropped, scale: scale, orientation: image.imageOrientation)
    }

    var body: some View {
        shape
            .fill(StockMark.coinFace)
            .frame(width: size, height: size)
            .overlay {
                shape.strokeBorder(StockMark.coinRim, lineWidth: StockMark.rimWidth)
            }
            .overlay {
                if let logo = logo ?? logoURL.flatMap({ MonacoRemoteImageStore.stockLogos.cachedImage(for: $0) }) {
                    // Fitted, not filled. Filling a disc with a square mark (Microsoft's four
                    // tiles) slices its corners off; fitting keeps every logo whole and lets the
                    // coin's face be the frame around it.
                    Image(uiImage: StockMark.croppedToMark(logo))
                        .resizable()
                        .scaledToFit()
                        .frame(width: size * StockMark.markInset, height: size * StockMark.markInset)
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
