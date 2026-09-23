import Foundation

public enum AssetKind: String, Codable, Equatable, Sendable {
    case stock
    case preIpo = "pre_ipo"

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        let raw = try container.decode(String.self).trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        self = AssetKind(rawValue: raw) ?? .stock
    }

    public init(raw: String?) {
        guard let raw else {
            self = .stock
            return
        }
        let normalized = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        self = AssetKind(rawValue: normalized) ?? .stock
    }
}

public enum AssetCatalogDefaults {
    public static let decimals = 8
}
