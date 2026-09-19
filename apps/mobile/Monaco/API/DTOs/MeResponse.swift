import Foundation

struct MeResponse: Codable, Equatable {
    let userId: String
    let displayName: String
    let memberWalletAddress: String
    let profilePhotoUrl: String?
}
