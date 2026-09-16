import Foundation

struct BuyQuoteDTO: Codable, Equatable {
    let symbol: String
    let usdcMicros: String
    let routable: Bool
}

struct CreateProposalResponse: Codable, Equatable {
    let proposalId: String
}
