import MonacoCore
import SwiftUI

/// A cabal's identity: pastel tile (tint from the group id) with 1–2 initials in ink.
struct CabalMark: View {
    private let tint: MonacoTheme.CabalTint
    private let initials: String
    private let name: String
    private let size: CGFloat

    init(groupId: String, name: String, size: CGFloat = 44) {
        tint = .forGroupId(groupId)
        initials = CabalMark.initials(for: name)
        self.name = name
        self.size = size
    }

    var body: some View {
        RoundedRectangle(cornerRadius: MarkGeometry.radius(for: size), style: .continuous)
            .fill(tint.fill)
            .frame(width: size, height: size)
            .overlay {
                Text(initials)
                    .font(.custom("AvenirNext-DemiBold", fixedSize: size * (initials.count > 1 ? 0.36 : 0.42)))
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.5)
                    .padding(size * 0.08)
            }
            .accessibilityHidden(true)
    }

    /// First grapheme of the first two words that start with a letter or digit, uppercased.
    /// Emoji- or punctuation-only names fall back to their first grapheme. Empty names render an empty tile.
    static func initials(for name: String) -> String {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let words = trimmed.split(whereSeparator: { $0.isWhitespace })
        let letters = words
            .compactMap { $0.first }
            .filter { $0.isLetter || $0.isNumber }
            .prefix(2)
        if !letters.isEmpty {
            return letters.map { String($0).uppercased() }.joined()
        }
        return trimmed.first.map { String($0) } ?? ""
    }
}

/// A stock's tile: sunken fill with the ticker's first letter. "USDC" (cash) shows a dollar sign.
struct StockMark: View {
    private enum Content {
        case letter(String)
        case symbol(String)
    }

    private let content: Content
    private let size: CGFloat

    init(symbol: String, size: CGFloat = 44) {
        let ticker = AssetSymbolFormatter.display(symbol)
        if ticker.uppercased() == "USDC" {
            content = .symbol("dollarsign")
        } else {
            content = .letter(ticker.first.map { String($0).uppercased() } ?? "")
        }
        self.size = size
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
                switch content {
                case .letter(let letter):
                    Text(letter)
                        .font(.system(size: size * 0.42, weight: .semibold))
                        .foregroundStyle(MonacoTheme.ink)
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
