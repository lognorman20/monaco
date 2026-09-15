import Foundation

public struct MeDTO: Codable, Equatable, Sendable {
    public let userID: String
    public let displayName: String?
    public let memberWalletAddress: String

    public init(userID: String, displayName: String?, memberWalletAddress: String) {
        self.userID = userID
        self.displayName = displayName
        self.memberWalletAddress = memberWalletAddress
    }

    enum CodingKeys: String, CodingKey {
        case userID = "user_id"
        case displayName = "display_name"
        case memberWalletAddress = "member_wallet_address"
    }
}
