import Foundation

/// Signed-in profile from `GET /v1/me`, `PATCH /v1/me`, `POST /v1/me/profile-photo`,
/// and `POST /v1/auth/session` (one shape for all four).
public struct MeDTO: Codable, Equatable, Sendable {
    public let userId: String
    /// Empty when the user has never set a name.
    public let displayName: String
    public let memberWalletAddress: String
    public let profilePhotoUrl: String?
    /// Account creation time (UTC). Nil when talking to a server that predates the field.
    public let createdAt: Date?

    public init(
        userId: String,
        displayName: String,
        memberWalletAddress: String,
        profilePhotoUrl: String? = nil,
        createdAt: Date? = nil
    ) {
        self.userId = userId
        self.displayName = displayName
        self.memberWalletAddress = memberWalletAddress
        self.profilePhotoUrl = profilePhotoUrl
        self.createdAt = createdAt
    }

    enum CodingKeys: String, CodingKey {
        case userId
        case displayName
        case memberWalletAddress
        case profilePhotoUrl
        case createdAt
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        userId = try container.decode(String.self, forKey: .userId)
        displayName = try container.decodeIfPresent(String.self, forKey: .displayName) ?? ""
        memberWalletAddress = try container.decodeIfPresent(String.self, forKey: .memberWalletAddress) ?? ""
        let photo = try container.decodeIfPresent(String.self, forKey: .profilePhotoUrl)?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        profilePhotoUrl = (photo?.isEmpty ?? true) ? nil : photo
        let rawCreatedAt = try container.decodeIfPresent(String.self, forKey: .createdAt) ?? ""
        createdAt = MonacoISO8601.date(from: rawCreatedAt)
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(userId, forKey: .userId)
        try container.encode(displayName, forKey: .displayName)
        try container.encode(memberWalletAddress, forKey: .memberWalletAddress)
        try container.encode(profilePhotoUrl, forKey: .profilePhotoUrl)
        try container.encode(createdAt.map(MonacoISO8601.string(from:)), forKey: .createdAt)
    }

    /// Copy with a different display name (optimistic UI).
    public func withDisplayName(_ name: String) -> MeDTO {
        MeDTO(
            userId: userId,
            displayName: name,
            memberWalletAddress: memberWalletAddress,
            profilePhotoUrl: profilePhotoUrl,
            createdAt: createdAt
        )
    }
}

/// `PATCH /v1/me` body.
public struct UpdateProfileRequestDTO: Codable, Equatable, Sendable {
    public let displayName: String

    public init(displayName: String) {
        self.displayName = displayName
    }
}

/// RFC 3339 timestamps as the backend writes them (with or without fractional seconds).
public enum MonacoISO8601 {
    public static func date(from raw: String) -> Date? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        return SharedFormatters.iso8601Date(from: trimmed)
    }

    public static func string(from date: Date) -> String {
        SharedFormatters.iso8601WholeSeconds.string(from: date)
    }
}
