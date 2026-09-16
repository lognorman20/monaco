import Foundation

public struct BuyQuoteDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let usdcMicros: String
    public let routable: Bool

    public init(symbol: String, usdcMicros: String, routable: Bool) {
        self.symbol = symbol
        self.usdcMicros = usdcMicros
        self.routable = routable
    }
}

public struct ProposalSubmitGate {
    public init() {}

    /// Returns false when quote is not routable — proposal POST must not fire.
    public func maySubmitProposal(quote: BuyQuoteDTO) -> Bool {
        quote.routable
    }
}
