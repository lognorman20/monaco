import Foundation

/// `GET /v1/me/watchlist`: the member's watchlist as market rows, in their order, in the
/// same envelope as `GET /v1/assets/popular` — so a watched stock draws exactly like every
/// other market row.
public struct WatchlistResponseDTO: Codable, Equatable, Sendable {
    public let assets: [MarketAssetDTO]
    public let market: MarketStatusDTO?

    public init(assets: [MarketAssetDTO], market: MarketStatusDTO? = nil) {
        self.assets = assets
        self.market = market
    }

    private enum CodingKeys: String, CodingKey {
        case assets, market
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        // An empty watchlist is `[]`; a null is read the same way rather than as a failure.
        assets = try container.decodeIfPresent([MarketAssetDTO].self, forKey: .assets) ?? []
        market = try container.decodeIfPresent(MarketStatusDTO.self, forKey: .market)
    }
}

/// `PUT /v1/me/watchlist/{symbol}`: where the stock sits now. `created` is the status the
/// server answered with: true for 201 (just added), false for 200 (it was already there).
public struct WatchlistEntryDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let position: Int
    public let createdAt: Date?

    public init(symbol: String, position: Int, createdAt: Date? = nil) {
        self.symbol = symbol
        self.position = position
        self.createdAt = createdAt
    }

    private enum CodingKeys: String, CodingKey {
        case symbol, position, createdAt
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        symbol = try container.decode(String.self, forKey: .symbol)
        position = try container.decodeIfPresent(Int.self, forKey: .position) ?? 0
        createdAt = try container.decodeIfPresent(String.self, forKey: .createdAt)
            .flatMap(SharedFormatters.iso8601Date(from:))
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(symbol, forKey: .symbol)
        try container.encode(position, forKey: .position)
        try container.encodeIfPresent(createdAt.map(MonacoTimestamp.init(date:)), forKey: .createdAt)
    }
}

/// `PUT /v1/me/watchlist` body and answer: every symbol on the watchlist, once, in order.
public struct WatchlistOrderDTO: Codable, Equatable, Sendable {
    public let symbols: [String]

    public init(symbols: [String]) {
        self.symbols = symbols
    }
}

/// How a watchlist write failed, in the terms the screen acts on.
public enum WatchlistWriteFailure: Equatable, Sendable {
    /// 409 on an add: the watchlist already holds the most stocks it can.
    case full
    /// 409 on a reorder: the list changed on another device; reload it.
    case changed
    /// 404 on an add: the catalogue lists no such stock.
    case unknownStock
    /// Anything else: offline, 5xx, a refusal with no special meaning.
    case other

    public init(_ error: Error, isReorder: Bool = false) {
        guard let api = error as? MonacoAPIError, let status = api.statusCode else {
            self = .other
            return
        }
        switch status {
        case 409: self = isReorder ? .changed : .full
        case 404: self = .unknownStock
        default: self = .other
        }
    }
}
