import Foundation

struct BuyQuoteDTO: Codable, Equatable {
    let symbol: String
    let usdcMicros: String
    let routable: Bool
    let outputAmount: String?
    let priceUsdcMicros: String?
}

struct CreateProposalResponse: Codable, Equatable {
    let proposalId: String
}
