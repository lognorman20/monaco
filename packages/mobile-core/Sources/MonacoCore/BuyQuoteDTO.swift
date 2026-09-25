import Foundation

public struct QuoteProviderDTO: Codable, Equatable, Hashable, Sendable {
    public let issuer: String?
    public let issuerName: String?
}

public struct BuyQuoteDTO: Codable, Equatable, Sendable {
    public let symbol: String
    /// Buy or sell (`"buy"` / `"sell"`).
    public let kind: String?
    public let usdcMicros: String?
    public let tokenAmount: String?
    public let routable: Bool
    public let outputAmount: String?
    public let outputUsdcMicros: String?
    public let priceUsdcMicros: String?
    public let assetKind: AssetKind?
    public let tokenDecimals: Int?
    public let premiumBps: Int?
    public let uiAmountMultiplier: String?
    public let provider: QuoteProviderDTO?

    public var resolvedAssetKind: AssetKind { assetKind ?? .stock }
    public var resolvedDecimals: Int { tokenDecimals ?? AssetCatalogDefaults.decimals }
    public var resolvedUiMultiplier: Decimal {
        guard let uiAmountMultiplier, let value = Decimal(string: uiAmountMultiplier, locale: Locale(identifier: "en_US_POSIX")), value > 0 else {
            return 1
        }
        return value
    }

    public init(
        symbol: String,
        routable: Bool,
        kind: String? = nil,
        usdcMicros: String? = nil,
        tokenAmount: String? = nil,
        outputAmount: String? = nil,
        outputUsdcMicros: String? = nil,
        priceUsdcMicros: String? = nil,
        assetKind: AssetKind? = nil,
        tokenDecimals: Int? = nil,
        premiumBps: Int? = nil,
        uiAmountMultiplier: String? = nil,
        provider: QuoteProviderDTO? = nil
    ) {
        self.symbol = symbol
        self.kind = kind
        self.usdcMicros = usdcMicros
        self.tokenAmount = tokenAmount
        self.routable = routable
        self.outputAmount = outputAmount
        self.outputUsdcMicros = outputUsdcMicros
        self.priceUsdcMicros = priceUsdcMicros
        self.assetKind = assetKind
        self.tokenDecimals = tokenDecimals
        self.premiumBps = premiumBps
        self.uiAmountMultiplier = uiAmountMultiplier
        self.provider = provider
    }
}

public struct ProposalSubmitGate {
    public init() {}

    /// Returns false when quote is not routable — proposal POST must not fire.
    public func maySubmitProposal(quote: BuyQuoteDTO) -> Bool {
        quote.routable
    }
}
