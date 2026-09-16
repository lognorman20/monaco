import Foundation

struct ProposalDTO: Codable, Equatable, Identifiable {
    let id: String
    let symbol: String
    let usdcMicros: String
    let status: String
    let canVote: Bool?

    enum CodingKeys: String, CodingKey {
        case id
        case symbol
        case usdcMicros
        case status
        case canVote
    }
}

enum ProposalStatusChipStyle: String {
    case open
    case passed
    case failed
    case expired

    init?(status: String) {
        self.init(rawValue: status.lowercased())
    }

    var label: String {
        switch self {
        case .open: "Open"
        case .passed: "Passed"
        case .failed: "Failed"
        case .expired: "Expired"
        }
    }
}
