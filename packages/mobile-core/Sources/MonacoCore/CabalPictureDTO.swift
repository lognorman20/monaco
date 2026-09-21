import Foundation

/// The cabal's picture after a write, from `POST`/`DELETE /v1/groups/{id}/picture`.
public struct CabalPictureDTO: Codable, Equatable, Sendable {
    public let groupId: String
    /// Nil once the picture is removed, which is how the mark knows to fall
    /// back to the cabal's tinted initials.
    public let pictureUrl: String?

    public init(groupId: String, pictureUrl: String?) {
        self.groupId = groupId
        self.pictureUrl = pictureUrl
    }

    /// The server already sends null rather than an empty string, but a blank
    /// value would otherwise become a URL the image loader fails on forever.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        groupId = try container.decode(String.self, forKey: .groupId)
        let rawPicture = try container.decodeIfPresent(String.self, forKey: .pictureUrl)
        let trimmed = rawPicture?.trimmingCharacters(in: .whitespacesAndNewlines)
        pictureUrl = (trimmed?.isEmpty ?? true) ? nil : trimmed
    }

    enum CodingKeys: String, CodingKey {
        case groupId
        case pictureUrl
    }
}
