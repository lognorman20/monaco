import Foundation

public struct BuyQuoteDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let kind: String?
    public let usdcMicros: String?
    public let tokenAmount: String?
    public let routable: Bool
    public let outputAmount: String?
    public let outputUsdcMicros: String?
    public let priceUsdcMicros: String?

    public init(
        symbol: String,
        routable: Bool,
        kind: String? = nil,
        usdcMicros: String? = nil,
        tokenAmount: String? = nil,
        outputAmount: String? = nil,
        outputUsdcMicros: String? = nil,
        priceUsdcMicros: String? = nil
    ) {
        self.symbol = symbol
        self.kind = kind
        self.usdcMicros = usdcMicros
        self.tokenAmount = tokenAmount
        self.routable = routable
        self.outputAmount = outputAmount
        self.outputUsdcMicros = outputUsdcMicros
        self.priceUsdcMicros = priceUsdcMicros
    }
}

public struct ProposalSubmitGate {
    public init() {}

    /// Returns false when quote is not routable — proposal POST must not fire.
    public func maySubmitProposal(quote: BuyQuoteDTO) -> Bool {
        quote.routable
    }
}
