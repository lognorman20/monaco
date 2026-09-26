import Foundation

/// Which side of its line an alert waits for.
public enum PriceAlertDirection: String, Codable, CaseIterable, Sendable, Identifiable {
    /// Fires once the price is at or above the line.
    case above
    /// Fires once the price is at or below the line.
    case below

    public var id: String { rawValue }

    /// Whether a price has already reached `line` from this side — the server's rule, so a
    /// sheet can say "already there" before the member taps Save.
    public func isReached(priceUsdcMicros: Int64, lineUsdcMicros: Int64) -> Bool {
        switch self {
        case .above: return priceUsdcMicros >= lineUsdcMicros
        case .below: return priceUsdcMicros <= lineUsdcMicros
        }
    }
}

/// One price alert. `priceUsdcMicros` is the line the member set; `triggeredAt` and
/// `triggeredPriceUsdcMicros` say when it fired and on what price.
public struct PriceAlertDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let symbol: String
    public let direction: PriceAlertDirection
    public let priceUsdcMicros: Int64
    public let active: Bool
    public let createdAt: Date
    public let triggeredAt: Date?
    public let triggeredPriceUsdcMicros: Int64?

    public var hasFired: Bool { triggeredAt != nil }

    public init(
        id: String,
        symbol: String,
        direction: PriceAlertDirection,
        priceUsdcMicros: Int64,
        active: Bool = true,
        createdAt: Date,
        triggeredAt: Date? = nil,
        triggeredPriceUsdcMicros: Int64? = nil
    ) {
        self.id = id
        self.symbol = symbol
        self.direction = direction
        self.priceUsdcMicros = priceUsdcMicros
        self.active = active
        self.createdAt = createdAt
        self.triggeredAt = triggeredAt
        self.triggeredPriceUsdcMicros = triggeredPriceUsdcMicros
    }

    private enum CodingKeys: String, CodingKey {
        case id, symbol, direction, priceUsdcMicros, active, createdAt, triggeredAt, triggeredPriceUsdcMicros
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        symbol = try container.decode(String.self, forKey: .symbol)
        direction = try container.decode(PriceAlertDirection.self, forKey: .direction)
        priceUsdcMicros = try container.decode(Int64.self, forKey: .priceUsdcMicros)
        createdAt = try container.decode(MonacoTimestamp.self, forKey: .createdAt).date
        triggeredAt = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .triggeredAt)?.date
        triggeredPriceUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .triggeredPriceUsdcMicros)
        // A fired alert is never active, whatever an older payload says.
        active = (try container.decodeIfPresent(Bool.self, forKey: .active) ?? true) && triggeredAt == nil
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(symbol, forKey: .symbol)
        try container.encode(direction, forKey: .direction)
        try container.encode(priceUsdcMicros, forKey: .priceUsdcMicros)
        try container.encode(active, forKey: .active)
        try container.encode(MonacoTimestamp(date: createdAt), forKey: .createdAt)
        try container.encodeIfPresent(triggeredAt.map(MonacoTimestamp.init(date:)), forKey: .triggeredAt)
        try container.encodeIfPresent(triggeredPriceUsdcMicros, forKey: .triggeredPriceUsdcMicros)
    }
}

/// `GET /v1/me/alerts`: the alerts (active first, newest first; then the ones that fired in
/// the last 30 days) and one market row per stock they name, for that stock's price.
public struct PriceAlertsResponseDTO: Codable, Equatable, Sendable {
    public let alerts: [PriceAlertDTO]
    public let assets: [MarketAssetDTO]
    public let market: MarketStatusDTO?

    public init(alerts: [PriceAlertDTO], assets: [MarketAssetDTO] = [], market: MarketStatusDTO? = nil) {
        self.alerts = alerts
        self.assets = assets
        self.market = market
    }

    private enum CodingKeys: String, CodingKey {
        case alerts, assets, market
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        alerts = try container.decodeIfPresent([PriceAlertDTO].self, forKey: .alerts) ?? []
        assets = try container.decodeIfPresent([MarketAssetDTO].self, forKey: .assets) ?? []
        market = try container.decodeIfPresent(MarketStatusDTO.self, forKey: .market)
    }
}

/// `POST /v1/me/alerts` body.
public struct CreatePriceAlertRequestDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let direction: PriceAlertDirection
    public let priceUsdcMicros: Int64

    public init(symbol: String, direction: PriceAlertDirection, priceUsdcMicros: Int64) {
        self.symbol = symbol
        self.direction = direction
        self.priceUsdcMicros = priceUsdcMicros
    }
}

/// How creating an alert failed, in the terms the sheet acts on.
public enum PriceAlertWriteFailure: Equatable, Sendable {
    /// 409: twenty alerts are already waiting.
    case limitReached
    /// 422: the price moved past the line between the sheet and the server.
    case alreadyReached
    /// 404: no such stock, or (on a delete) no such alert.
    case notFound
    case other

    public init(_ error: Error) {
        guard let api = error as? MonacoAPIError, let status = api.statusCode else {
            self = .other
            return
        }
        switch status {
        case 409: self = .limitReached
        case 422: self = .alreadyReached
        case 404: self = .notFound
        default: self = .other
        }
    }
}

/// The alerts page's shape: one group per stock, in the order the stocks first appear in the
/// server's list (so the stock with the newest waiting alert leads), each carrying its market
/// row when the server sent one, then its waiting alerts and the ones that fired.
public struct PriceAlertGroup: Equatable, Sendable, Identifiable {
    public let symbol: String
    public let asset: MarketAssetDTO?
    public let waiting: [PriceAlertDTO]
    public let fired: [PriceAlertDTO]

    public var id: String { symbol }
    public var alerts: [PriceAlertDTO] { waiting + fired }

    public init(symbol: String, asset: MarketAssetDTO?, waiting: [PriceAlertDTO], fired: [PriceAlertDTO]) {
        self.symbol = symbol
        self.asset = asset
        self.waiting = waiting
        self.fired = fired
    }

    public static func make(_ response: PriceAlertsResponseDTO) -> [PriceAlertGroup] {
        make(alerts: response.alerts, assets: response.assets)
    }

    public static func make(alerts: [PriceAlertDTO], assets: [MarketAssetDTO]) -> [PriceAlertGroup] {
        var order: [String] = []
        var bySymbol: [String: [PriceAlertDTO]] = [:]
        var spelling: [String: String] = [:]
        for alert in alerts {
            let key = alert.symbol.uppercased()
            if bySymbol[key] == nil {
                order.append(key)
                spelling[key] = alert.symbol
            }
            bySymbol[key, default: []].append(alert)
        }
        var rows: [String: MarketAssetDTO] = [:]
        for asset in assets where rows[asset.symbol.uppercased()] == nil {
            rows[asset.symbol.uppercased()] = asset
        }
        return order.map { key in
            let group = bySymbol[key] ?? []
            return PriceAlertGroup(
                symbol: spelling[key] ?? key,
                asset: rows[key],
                waiting: group.filter { !$0.hasFired }.sorted { $0.createdAt > $1.createdAt },
                fired: group.filter(\.hasFired).sorted { ($0.triggeredAt ?? .distantPast) > ($1.triggeredAt ?? .distantPast) }
            )
        }
    }
}
