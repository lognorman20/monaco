import Foundation

/// What an xStock actually is, and the disclosure that has to sit under it.
///
/// There is no vendor description behind this. The xStocks public API exposes a
/// symbol, a name and the chain deployments and nothing else (see the catalog
/// node in `internal/xstocks/catalog.go`), so rather than shipping a field the
/// backend would always send empty — or worse, inventing a paragraph of company
/// blurb — the section is built from facts the app actually holds: what the token
/// is, what it tracks, where it trades, and what holding it does not give you.
///
/// The disclosure is the part that matters and is never optional. `product.md`
/// requires it, and it is the difference between a tokenized tracker and a share.
public struct AssetAboutCopy: Equatable, Sendable {
    /// "About AAPLx".
    public let title: String
    /// The body, in sentences. Clamped in the view, not here.
    public let body: String
    /// "AAPLx tracks Apple stock. It is not a share." — never folded into `body`,
    /// because a clamped paragraph must not be able to hide it.
    public let disclosure: String
    /// Facts with a source: the chain, the mint, the routing venue.
    public let facts: [Fact]

    public struct Fact: Equatable, Sendable, Identifiable {
        public let id: String
        public let label: String
        public let value: String
        /// True for a value that should be drawn in a monospace, truncating middle
        /// — a Solana mint, not a word.
        public let isAddress: Bool

        public init(id: String, label: String, value: String, isAddress: Bool = false) {
            self.id = id
            self.label = label
            self.value = value
            self.isAddress = isAddress
        }
    }

    public static func make(
        symbol: String,
        name: String,
        solanaMint: String,
        liquidityLabel: String? = nil
    ) -> AssetAboutCopy {
        let tokenTicker = AssetSymbolFormatter.format(symbol)
        let underlying = AssetSymbolFormatter.display(symbol)
        let company = resolvedCompany(name: name, underlying: underlying)

        var facts: [Fact] = [
            Fact(id: "tracks", label: "Tracks", value: "\(company) (\(underlying))"),
            Fact(id: "chain", label: "Chain", value: "Solana"),
        ]
        let mint = solanaMint.trimmingCharacters(in: .whitespacesAndNewlines)
        if !mint.isEmpty {
            facts.append(Fact(id: "mint", label: "Token mint", value: mint, isAddress: true))
        }
        if let liquidityLabel, !liquidityLabel.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            facts.append(Fact(id: "routing", label: "Traded", value: liquidityLabel))
        }

        return AssetAboutCopy(
            title: "About \(tokenTicker)",
            body: body(tokenTicker: tokenTicker, underlying: underlying, company: company),
            disclosure: disclosure(tokenTicker: tokenTicker, company: company),
            facts: facts
        )
    }

    /// "AAPLx tracks Apple stock. It is not a share."
    ///
    /// Two short sentences on purpose. The second one is the whole point and must
    /// survive being read on its own, at the end of a scroll, by someone who is
    /// about to propose a buy.
    static func disclosure(tokenTicker: String, company: String) -> String {
        "\(tokenTicker) tracks \(company) stock. It is not a share: holding it gives you no ownership, "
            + "no dividend and no vote at \(company)."
    }

    private static func body(tokenTicker: String, underlying: String, company: String) -> String {
        "\(tokenTicker) is a tokenized tracker for \(company). Its price follows \(underlying) on the "
            + "underlying exchange, and it settles on Solana, so a cabal can buy and sell it at any hour "
            + "— including when the exchange behind it is shut. Supply and demand on-chain move it a "
            + "little either side of the stock; the stock-vs-token card above shows by how much."
    }

    /// The catalog name when there is one, and the ticker when there is not. Never a
    /// guess at a company name: "Unknown stock" is honest and a made-up name is not.
    private static func resolvedCompany(name: String, underlying: String) -> String {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty, trimmed.caseInsensitiveCompare(underlying) != .orderedSame {
            return CatalogAssetNameFormatter.format(trimmed)
        }
        if let known = AssetDisplayNames.name(forSymbol: underlying) { return known }
        return underlying
    }
}
