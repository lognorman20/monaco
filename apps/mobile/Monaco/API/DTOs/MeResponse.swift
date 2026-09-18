import Foundation

struct MeResponse: Codable, Equatable, Sendable {
    let userId: String
    let displayName: String
    let memberWalletAddress: String
}
