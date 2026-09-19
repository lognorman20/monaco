import Foundation

struct BuyQuoteDTO: Codable, Equatable {
    let symbol: String
    let kind: String?
    let usdcMicros: String?
    let tokenAmount: String?
    let routable: Bool
    let outputAmount: String?
    let outputUsdcMicros: String?
    let priceUsdcMicros: String?
}

struct CreateProposalResponse: Codable, Equatable {
    let proposalId: String
}
