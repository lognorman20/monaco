import Foundation

/// A kind of notification the member can switch off in Settings. The raw values are the keys
/// the backend stores under `notifications` in `GET /v1/me/preferences`.
public enum NotificationCategory: String, CaseIterable, Codable, Sendable, Identifiable {
    case proposals
    case results
    case chat
    case money

    public var id: String { rawValue }
}

/// `notifications` in the member's preferences. A key the server leaves out reads as on:
/// every category is on until the member turns it off.
public struct NotificationPreferencesDTO: Codable, Equatable, Sendable {
    public var proposals: Bool
    public var results: Bool
    public var chat: Bool
    public var money: Bool

    public static let allOn = NotificationPreferencesDTO(proposals: true, results: true, chat: true, money: true)

    public init(proposals: Bool, results: Bool, chat: Bool, money: Bool) {
        self.proposals = proposals
        self.results = results
        self.chat = chat
        self.money = money
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        proposals = try container.decodeIfPresent(Bool.self, forKey: .proposals) ?? true
        results = try container.decodeIfPresent(Bool.self, forKey: .results) ?? true
        chat = try container.decodeIfPresent(Bool.self, forKey: .chat) ?? true
        money = try container.decodeIfPresent(Bool.self, forKey: .money) ?? true
    }

    public subscript(category: NotificationCategory) -> Bool {
        get {
            switch category {
            case .proposals: return proposals
            case .results: return results
            case .chat: return chat
            case .money: return money
            }
        }
        set {
            switch category {
            case .proposals: proposals = newValue
            case .results: results = newValue
            case .chat: chat = newValue
            case .money: money = newValue
            }
        }
    }
}

/// `GET` and `PATCH /v1/me/preferences`: the whole document, every key present.
public struct PreferencesDTO: Codable, Equatable, Sendable {
    public var notifications: NotificationPreferencesDTO

    public static let defaults = PreferencesDTO(notifications: .allOn)

    public init(notifications: NotificationPreferencesDTO) {
        self.notifications = notifications
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        notifications = try container.decodeIfPresent(NotificationPreferencesDTO.self, forKey: .notifications) ?? .allOn
    }
}

/// `PATCH /v1/me/preferences` body: a merge patch, so only the switches that changed.
public struct PreferencesPatchDTO: Encodable, Equatable, Sendable {
    public let notifications: [String: Bool]

    public init(notifications: [NotificationCategory: Bool]) {
        self.notifications = Dictionary(uniqueKeysWithValues: notifications.map { ($0.key.rawValue, $0.value) })
    }
}
