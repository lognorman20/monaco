import Foundation

/// Which window of the US equities trading day the market is in.
///
/// The backend computes this from the exchange calendar, so the app never has to
/// guess from a clock or a time zone. `unknown` exists only so a session name the
/// server adds later cannot fail decoding of the whole response.
public enum MarketSession: String, Codable, Sendable, CaseIterable {
    case preMarket = "pre_market"
    case open
    case afterHours = "after_hours"
    case closed
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = MarketSession(rawValue: raw) ?? .unknown
    }

    /// True only during the regular cash session.
    public var isRegularSession: Bool { self == .open }
}

/// The market session an asset response was built in, with the next boundary.
///
/// Every timestamp is UTC — the app converts for display and never the other way
/// round. This rides on the envelope rather than on each row because it is one
/// fact about the exchange, not a property of an individual stock.
public struct MarketStatusDTO: Codable, Equatable, Sendable {
    public let session: MarketSession
    public let isOpen: Bool
    /// True whenever the regular session is not running — the moment an xStock
    /// keeps trading on Solana and the underlying equity does not.
    public let afterHours: Bool
    public let nextSession: MarketSession?
    public let nextTransition: Date?
    public let asOf: Date?
    /// Set when the market is closed for a named exchange holiday.
    public let holiday: String?
    /// True on a 1pm ET half day.
    public let earlyClose: Bool

    public init(
        session: MarketSession,
        isOpen: Bool,
        afterHours: Bool,
        nextSession: MarketSession? = nil,
        nextTransition: Date? = nil,
        asOf: Date? = nil,
        holiday: String? = nil,
        earlyClose: Bool = false
    ) {
        self.session = session
        self.isOpen = isOpen
        self.afterHours = afterHours
        self.nextSession = nextSession
        self.nextTransition = nextTransition
        self.asOf = asOf
        self.holiday = holiday
        self.earlyClose = earlyClose
    }

    private enum CodingKeys: String, CodingKey {
        case session, isOpen, afterHours, nextSession, nextTransition, asOf, holiday, earlyClose
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        session = try container.decodeIfPresent(MarketSession.self, forKey: .session) ?? .unknown
        isOpen = try container.decodeIfPresent(Bool.self, forKey: .isOpen) ?? false
        // A response that names a session but omits afterHours is still readable:
        // anything but the regular session is after hours by definition.
        if let decoded = try container.decodeIfPresent(Bool.self, forKey: .afterHours) {
            afterHours = decoded
        } else {
            afterHours = !isOpen
        }
        nextSession = try container.decodeIfPresent(MarketSession.self, forKey: .nextSession)
        nextTransition = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .nextTransition)?.date
        asOf = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .asOf)?.date
        holiday = try container.decodeIfPresent(String.self, forKey: .holiday)
        earlyClose = try container.decodeIfPresent(Bool.self, forKey: .earlyClose) ?? false
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(session, forKey: .session)
        try container.encode(isOpen, forKey: .isOpen)
        try container.encode(afterHours, forKey: .afterHours)
        try container.encodeIfPresent(nextSession, forKey: .nextSession)
        try container.encodeIfPresent(nextTransition.map(MonacoTimestamp.init(date:)), forKey: .nextTransition)
        try container.encodeIfPresent(asOf.map(MonacoTimestamp.init(date:)), forKey: .asOf)
        try container.encodeIfPresent(holiday, forKey: .holiday)
        try container.encode(earlyClose, forKey: .earlyClose)
    }
}

/// An RFC3339 timestamp as the backend writes it.
///
/// The market payloads mix timestamps with plain numbers, and the asset routes are
/// decoded with a plain `JSONDecoder`, so the parsing lives on the field rather
/// than on the decoder's `dateDecodingStrategy`. It reuses the shared parser, which
/// already accepts both the fractional and whole-second spellings Go emits.
public struct MonacoTimestamp: Codable, Equatable, Sendable {
    public let date: Date

    public init(date: Date) {
        self.date = date
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        let raw = try container.decode(String.self)
        guard let parsed = SharedFormatters.iso8601Date(from: raw) else {
            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "Expected ISO8601 date, got \(raw)"
            )
        }
        date = parsed
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(SharedFormatters.iso8601WholeSeconds.string(from: date))
    }
}
