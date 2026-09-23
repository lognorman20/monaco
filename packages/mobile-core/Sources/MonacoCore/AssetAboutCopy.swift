import Foundation

/// What a B20 token actually is, and the disclosure that has to sit under it.
///
/// There is no vendor description behind this. The catalog exposes a symbol, a name,
/// a token address and a decimal count and nothing else, so rather than shipping a
/// field the backend would always send empty — or worse, inventing a paragraph of
/// company blurb — the section is built from facts the app actually holds: what the
/// token is, what it tracks, where it trades, and what holding it does and does not
/// give you.
///
/// The disclosure is the part that matters and is never optional. `product.md`
/// requires it, and it is the difference between a tokenized tracker and a share.
///
/// Every sentence here is checked against what a B20 token does, not against what a
/// tokenized stock is assumed to do. The pre-Base wording said holding one gives
/// "no dividend", which is not true of these tokens and is the worst possible place
/// to be wrong: Coinbase's registry converts a cash dividend into shares of the
/// underlying and raises the token's **multiplier**, and Chainlink's total-return
/// feed — the mark this whole screen is priced from — carries that multiplier. The
/// distribution is not paid out in cash; it is reflected in what the token is worth.
/// Saying "no dividend" would tell a member the money went nowhere.
public struct AssetAboutCopy: Equatable, Sendable {
    /// "About AAPLc".
    public let title: String
    /// The body, in sentences. Clamped in the view, not here.
    public let body: String
    /// "AAPLc tracks Apple stock. It is not a share." — never folded into `body`,
    /// because a clamped paragraph must not be able to hide it.
    public let disclosure: String
    /// Facts with a source: the chain, the token address, the routing venue.
    public let facts: [Fact]

    public struct Fact: Equatable, Sendable, Identifiable {
        public let id: String
        public let label: String
        public let value: String
        /// True for a value that should be drawn in a monospace, truncating middle
        /// — a contract address, not a word.
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
        tokenAddress: String,
        liquidityLabel: String? = nil
    ) -> AssetAboutCopy {
        let tokenTicker = AssetSymbolFormatter.format(symbol)
        let underlying = AssetSymbolFormatter.display(symbol)
        let company = resolvedCompany(name: name, underlying: underlying)

        var facts: [Fact] = [
            Fact(id: "tracks", label: "Tracks", value: "\(company) (\(underlying))"),
            Fact(id: "chain", label: "Chain", value: "Base"),
        ]
        let address = tokenAddress.trimmingCharacters(in: .whitespacesAndNewlines)
        if !address.isEmpty {
            facts.append(Fact(id: "token", label: "Token address", value: address, isAddress: true))
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

    /// "AAPLc tracks Apple stock. It is not a share."
    ///
    /// Two short sentences on purpose. The second one is the whole point and must
    /// survive being read on its own, at the end of a scroll, by someone who is
    /// about to propose a buy.
    ///
    /// What it claims is narrow and true: no ownership, no vote. It does not claim
    /// there is no dividend, because there is one — it arrives as an increase in the
    /// token's value rather than as cash, and `dividendNote` says so in its own
    /// sentence rather than burying it in a denial.
    static func disclosure(tokenTicker: String, company: String) -> String {
        "\(tokenTicker) tracks \(company) stock. It is not a share: holding it gives you no ownership "
            + "of \(company) and no vote in it."
    }

    /// Where a dividend goes. Its own sentence, because it is the fact most likely
    /// to be assumed wrongly in either direction.
    static func dividendNote(tokenTicker: String) -> String {
        "A cash dividend is not paid out to holders: it is reinvested into the stock behind the token, "
            + "which raises what one \(tokenTicker) is worth. Splits move it the same way."
    }

    private static func body(tokenTicker: String, underlying: String, company: String) -> String {
        "\(tokenTicker) is a tokenized tracker for \(company). Its price follows \(underlying) on the "
            + "underlying exchange, and it settles on Base, so a cabal can buy and sell it outside "
            + "exchange hours — including when the exchange behind it is shut. Supply and demand "
            + "on-chain move it a little either side of its mark; the stock-vs-token card above shows "
            + "by how much. \(dividendNote(tokenTicker: tokenTicker))"
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
