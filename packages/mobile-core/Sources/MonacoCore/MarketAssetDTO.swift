import Foundation

public struct MarketAssetDTO: Codable, Equatable, Sendable, Identifiable {
    public let symbol: String
    public let name: String
    public let solanaMint: String
    public let routable: Bool
    public let priceUsdcMicros: Int64?
    public let change24h: String?

    public var id: String { symbol }

    public init(
        symbol: String,
        name: String,
        solanaMint: String,
        routable: Bool,
        priceUsdcMicros: Int64? = nil,
        change24h: String? = nil
    ) {
        self.symbol = symbol
        self.name = name
        self.solanaMint = solanaMint
        self.routable = routable
        self.priceUsdcMicros = priceUsdcMicros
        self.change24h = change24h
    }
}

public struct ListMarketAssetsResponseDTO: Codable, Equatable, Sendable {
    public let assets: [MarketAssetDTO]
    public let hasMore: Bool
    /// The market session every row in this page was priced in. Nil against an
    /// older backend, which is why nothing renders a session chip unconditionally.
    public let market: MarketStatusDTO?

    public init(assets: [MarketAssetDTO], hasMore: Bool, market: MarketStatusDTO? = nil) {
        self.assets = assets
        self.hasMore = hasMore
        self.market = market
    }
}

public struct PopularAssetsResponseDTO: Codable, Equatable, Sendable {
    public let assets: [MarketAssetDTO]
    public let market: MarketStatusDTO?

    public init(assets: [MarketAssetDTO], market: MarketStatusDTO? = nil) {
        self.assets = assets
        self.market = market
    }
}

public struct AssetLiquidityDTO: Codable, Equatable, Sendable {
    public let label: String
    public let routable: Bool
    public let buyProbeUsdcMicros: Int64
    public let buyProbeOutAmount: String?
    public let sellProbeInAmount: String?
    public let sellProbeOutAmount: String?
    public let spreadBps: Int?

    public init(
        label: String,
        routable: Bool,
        buyProbeUsdcMicros: Int64,
        buyProbeOutAmount: String? = nil,
        sellProbeInAmount: String? = nil,
        sellProbeOutAmount: String? = nil,
        spreadBps: Int? = nil
    ) {
        self.label = label
        self.routable = routable
        self.buyProbeUsdcMicros = buyProbeUsdcMicros
        self.buyProbeOutAmount = buyProbeOutAmount
        self.sellProbeInAmount = sellProbeInAmount
        self.sellProbeOutAmount = sellProbeOutAmount
        self.spreadBps = spreadBps
    }
}

/// The stats grid. Every field is optional because the backend omits a cell it
/// could not source rather than sending a placeholder, and a grid that renders
/// "—" is honest in a way that a made-up number is not.
///
/// Market cap, P/E and dividend yield are deliberately absent: nothing behind
/// xStocks publishes them.
/// Which instrument a figure is about.
///
/// An xStock has two prices: the underlying equity on its home exchange, and the
/// token on Solana. They differ by a premium of tens of basis points, which is what
/// the stock-vs-token card exists to show. Every Pyth history source we have serves
/// the underlying, so anything folded from candles — the stats grid, the chart —
/// is the equity's, while the hero price is the token's. A current price above "the
/// day's high" is therefore two instruments, not a bug, and the label is what makes
/// that readable instead of alarming.
public enum MarketPriceBasis: String, Codable, Sendable {
    /// The equity on NASDAQ/NYSE: `Equity.US.AAPL/USD`.
    case underlying
    /// The xStock itself: `Crypto.AAPLX/USD`, or its on-chain price.
    case token
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = MarketPriceBasis(rawValue: raw) ?? .unknown
    }
}

public struct AssetStatsDTO: Codable, Equatable, Sendable {
    public let openUsdcMicros: Int64?
    public let highUsdcMicros: Int64?
    public let lowUsdcMicros: Int64?
    public let previousCloseUsdcMicros: Int64?
    public let week52HighUsdcMicros: Int64?
    public let week52LowUsdcMicros: Int64?
    /// Round-trip trading cost in basis points, from the Jupiter probes.
    public let spreadBps: Int?
    /// Pyth's own confidence interval around the latest mark, in USDC micros.
    public let confUsdcMicros: Int64?
    /// Which instrument the candle-derived cells describe. Open, high, low, previous
    /// close and the 52-week range come from the underlying equity's candles, not
    /// the token's — the grid has to be headed with `basisSymbol` or it reads as
    /// though those numbers were about the thing the user is buying.
    public let basis: MarketPriceBasis?
    /// The instrument `basis` names, for display: "AAPL".
    public let basisSymbol: String?

    public init(
        openUsdcMicros: Int64? = nil,
        highUsdcMicros: Int64? = nil,
        lowUsdcMicros: Int64? = nil,
        previousCloseUsdcMicros: Int64? = nil,
        week52HighUsdcMicros: Int64? = nil,
        week52LowUsdcMicros: Int64? = nil,
        spreadBps: Int? = nil,
        confUsdcMicros: Int64? = nil,
        basis: MarketPriceBasis? = nil,
        basisSymbol: String? = nil
    ) {
        self.openUsdcMicros = openUsdcMicros
        self.highUsdcMicros = highUsdcMicros
        self.lowUsdcMicros = lowUsdcMicros
        self.previousCloseUsdcMicros = previousCloseUsdcMicros
        self.week52HighUsdcMicros = week52HighUsdcMicros
        self.week52LowUsdcMicros = week52LowUsdcMicros
        self.spreadBps = spreadBps
        self.confUsdcMicros = confUsdcMicros
        self.basis = basis
        self.basisSymbol = basisSymbol
    }

    /// True when no figure could be sourced, so the grid should not be drawn at
    /// all. A basis label on its own is not a grid.
    public var isEmpty: Bool {
        openUsdcMicros == nil
            && highUsdcMicros == nil
            && lowUsdcMicros == nil
            && previousCloseUsdcMicros == nil
            && week52HighUsdcMicros == nil
            && week52LowUsdcMicros == nil
            && spreadBps == nil
            && confUsdcMicros == nil
    }

    /// Header for the candle-derived cells, e.g. "AAPL on its home exchange". Nil
    /// when the backend did not say which instrument the cells are about, in which
    /// case the grid is drawn unheaded rather than under a guess.
    public var basisCaption: String? {
        guard let basisSymbol, !basisSymbol.isEmpty else { return nil }
        switch basis {
        case .underlying: return "\(basisSymbol) on its home exchange"
        case .token: return "\(basisSymbol) on Solana"
        case .unknown, nil: return nil
        }
    }
}

/// Where a reference price came from. `jupiter` is the on-chain fallback used when
/// an xStock has no Pyth crypto feed — labelled separately so nothing on screen
/// reads as a Pyth price that is not one.
public enum ReferenceQuoteSource: String, Codable, Sendable {
    case pythEquity = "pyth_equity"
    case pythCrypto = "pyth_crypto"
    case jupiter
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = ReferenceQuoteSource(rawValue: raw) ?? .unknown
    }
}

/// How much a reference quote can be trusted right now.
public enum ReferenceQuoteStatus: String, Codable, Sendable {
    /// The feed published recently.
    case live
    /// A real price, frozen — an equity feed outside the cash session, or a feed
    /// that has stopped publishing. `publishedAt` says when it stopped.
    case stale
    /// No price at all. `reason` says why, and `priceUsdcMicros` is nil.
    case unavailable
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = ReferenceQuoteStatus(rawValue: raw) ?? .unknown
    }
}

/// Why a quote is unavailable. Stable strings the app maps to copy.
public enum ReferenceQuoteReason: String, Sendable {
    /// The symbol has no such Pyth feed.
    case noFeed = "no_feed"
    /// Our API key is not entitled to the feed. Retrying will not fix it.
    case notEntitled = "not_entitled"
    /// The upstream failed or answered with something unusable.
    case upstreamError = "upstream_error"
}

/// One leg of the stock-vs-token comparison.
public struct ReferenceQuoteDTO: Codable, Equatable, Sendable {
    public let source: ReferenceQuoteSource
    public let status: ReferenceQuoteStatus
    public let priceUsdcMicros: Int64?
    public let confUsdcMicros: Int64?
    public let publishedAt: Date?
    public let reason: String?

    public init(
        source: ReferenceQuoteSource,
        status: ReferenceQuoteStatus,
        priceUsdcMicros: Int64? = nil,
        confUsdcMicros: Int64? = nil,
        publishedAt: Date? = nil,
        reason: String? = nil
    ) {
        self.source = source
        self.status = status
        self.priceUsdcMicros = priceUsdcMicros
        self.confUsdcMicros = confUsdcMicros
        self.publishedAt = publishedAt
        self.reason = reason
    }

    /// True when this leg carries a number worth showing.
    public var isPriced: Bool {
        status != .unavailable && (priceUsdcMicros ?? 0) > 0
    }

    public var unavailableReason: ReferenceQuoteReason? {
        reason.flatMap(ReferenceQuoteReason.init(rawValue:))
    }

    private enum CodingKeys: String, CodingKey {
        case source, status, priceUsdcMicros, confUsdcMicros, publishedAt, reason
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        source = try container.decodeIfPresent(ReferenceQuoteSource.self, forKey: .source) ?? .unknown
        status = try container.decodeIfPresent(ReferenceQuoteStatus.self, forKey: .status) ?? .unknown
        priceUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .priceUsdcMicros)
        confUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .confUsdcMicros)
        publishedAt = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .publishedAt)?.date
        reason = try container.decodeIfPresent(String.self, forKey: .reason)
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(source, forKey: .source)
        try container.encode(status, forKey: .status)
        try container.encodeIfPresent(priceUsdcMicros, forKey: .priceUsdcMicros)
        try container.encodeIfPresent(confUsdcMicros, forKey: .confUsdcMicros)
        try container.encodeIfPresent(publishedAt.map(MonacoTimestamp.init(date:)), forKey: .publishedAt)
        try container.encodeIfPresent(reason, forKey: .reason)
    }
}

/// The underlying equity against the token that tracks it: what Apple costs on
/// NASDAQ versus what AAPLx costs on Solana, plus how far apart they are.
public struct StockVsTokenDTO: Codable, Equatable, Sendable {
    public let equity: ReferenceQuoteDTO
    public let token: ReferenceQuoteDTO
    /// How far the token trades above (positive) or below (negative) the equity.
    /// Nil unless both legs carry a real price — a premium against a missing leg
    /// would be a number nobody measured.
    public let premiumBps: Int?
    public let asOf: Date?

    public init(
        equity: ReferenceQuoteDTO,
        token: ReferenceQuoteDTO,
        premiumBps: Int? = nil,
        asOf: Date? = nil
    ) {
        self.equity = equity
        self.token = token
        self.premiumBps = premiumBps
        self.asOf = asOf
    }

    private enum CodingKeys: String, CodingKey {
        case equity, token, premiumBps, asOf
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        equity = try container.decode(ReferenceQuoteDTO.self, forKey: .equity)
        token = try container.decode(ReferenceQuoteDTO.self, forKey: .token)
        premiumBps = try container.decodeIfPresent(Int.self, forKey: .premiumBps)
        asOf = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .asOf)?.date
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(equity, forKey: .equity)
        try container.encode(token, forKey: .token)
        try container.encodeIfPresent(premiumBps, forKey: .premiumBps)
        try container.encodeIfPresent(asOf.map(MonacoTimestamp.init(date:)), forKey: .asOf)
    }
}

public struct AssetDetailDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let name: String
    public let solanaMint: String
    public let routable: Bool
    public let priceUsdcMicros: Int64?
    public let change24h: String?
    public let liquidity: AssetLiquidityDTO
    /// The session the hero header reads. Nil against an older backend.
    public let marketSession: MarketSession?
    public let afterHours: Bool
    public let market: MarketStatusDTO?
    public let stats: AssetStatsDTO?
    public let stockVsToken: StockVsTokenDTO?

    public init(
        symbol: String,
        name: String,
        solanaMint: String,
        routable: Bool,
        priceUsdcMicros: Int64? = nil,
        change24h: String? = nil,
        liquidity: AssetLiquidityDTO,
        marketSession: MarketSession? = nil,
        afterHours: Bool = false,
        market: MarketStatusDTO? = nil,
        stats: AssetStatsDTO? = nil,
        stockVsToken: StockVsTokenDTO? = nil
    ) {
        self.symbol = symbol
        self.name = name
        self.solanaMint = solanaMint
        self.routable = routable
        self.priceUsdcMicros = priceUsdcMicros
        self.change24h = change24h
        self.liquidity = liquidity
        self.marketSession = marketSession
        self.afterHours = afterHours
        self.market = market
        self.stats = stats
        self.stockVsToken = stockVsToken
    }

    private enum CodingKeys: String, CodingKey {
        case symbol, name, solanaMint, routable, priceUsdcMicros, change24h, liquidity
        case marketSession, afterHours, market, stats, stockVsToken
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        symbol = try container.decode(String.self, forKey: .symbol)
        name = try container.decode(String.self, forKey: .name)
        solanaMint = try container.decode(String.self, forKey: .solanaMint)
        routable = try container.decodeIfPresent(Bool.self, forKey: .routable) ?? false
        priceUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .priceUsdcMicros)
        change24h = try container.decodeIfPresent(String.self, forKey: .change24h)
        liquidity = try container.decode(AssetLiquidityDTO.self, forKey: .liquidity)
        market = try container.decodeIfPresent(MarketStatusDTO.self, forKey: .market)
        marketSession = try container.decodeIfPresent(MarketSession.self, forKey: .marketSession) ?? market?.session
        afterHours = try container.decodeIfPresent(Bool.self, forKey: .afterHours) ?? market?.afterHours ?? false
        // An empty grid object is the same as no grid at all; collapse it here so
        // no screen has to check both.
        let decodedStats = try container.decodeIfPresent(AssetStatsDTO.self, forKey: .stats)
        stats = (decodedStats?.isEmpty ?? true) ? nil : decodedStats
        stockVsToken = try container.decodeIfPresent(StockVsTokenDTO.self, forKey: .stockVsToken)
    }
}

public struct AssetChartPointDTO: Codable, Equatable, Sendable, Identifiable {
    public let timestamp: Int64
    public let priceUsdcMicros: Int64
    /// Candle fields, present when the series came from Benchmarks. All zero when
    /// the sampled Hermes fallback produced the series, which only knows a price
    /// at an instant.
    public let openUsdcMicros: Int64
    public let highUsdcMicros: Int64
    public let lowUsdcMicros: Int64

    public var id: Int64 { timestamp }

    public var date: Date {
        Date(timeIntervalSince1970: TimeInterval(timestamp))
    }

    public var chartValue: Double {
        Double(priceUsdcMicros) / 1_000_000
    }

    /// True when this point can be drawn as a candle rather than only as a close.
    public var hasCandle: Bool {
        openUsdcMicros > 0 && highUsdcMicros > 0 && lowUsdcMicros > 0
    }

    public init(
        timestamp: Int64,
        priceUsdcMicros: Int64,
        openUsdcMicros: Int64 = 0,
        highUsdcMicros: Int64 = 0,
        lowUsdcMicros: Int64 = 0
    ) {
        self.timestamp = timestamp
        self.priceUsdcMicros = priceUsdcMicros
        self.openUsdcMicros = openUsdcMicros
        self.highUsdcMicros = highUsdcMicros
        self.lowUsdcMicros = lowUsdcMicros
    }

    private enum CodingKeys: String, CodingKey {
        case timestamp, priceUsdcMicros, openUsdcMicros, highUsdcMicros, lowUsdcMicros
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        timestamp = try container.decode(Int64.self, forKey: .timestamp)
        priceUsdcMicros = try container.decode(Int64.self, forKey: .priceUsdcMicros)
        openUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .openUsdcMicros) ?? 0
        highUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .highUsdcMicros) ?? 0
        lowUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .lowUsdcMicros) ?? 0
    }
}

/// Which upstream produced a series: a dense Benchmarks candle series, or the
/// sparse sampled Hermes fallback.
public enum AssetChartSource: String, Codable, Sendable {
    case benchmarks
    case hermes
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = AssetChartSource(rawValue: raw) ?? .unknown
    }
}

public struct AssetChartDTO: Codable, Equatable, Sendable {
    public let points: [AssetChartPointDTO]
    public let emptyReason: String?
    /// The close of the regular session before this window: the baseline a day
    /// chart draws its dashed line at and measures its change against.
    ///
    /// Absent when the source does not know one. The sampled fallback
    /// (`source == .hermes`) never does — its grid starts inside the window, so any
    /// number it could offer here is a point already drawn in `points`, and a
    /// baseline sitting exactly on the curve's first point is not a baseline. Draw
    /// nothing rather than a line through t0.
    public let previousCloseUsdcMicros: Int64?
    /// The range this series was built for. A response that names a range the user
    /// has already tapped away from should be discarded, not drawn.
    public let range: AssetChartRange?
    public let source: AssetChartSource?
    /// Which instrument the curve is. Both Pyth history sources read the underlying
    /// equity's feed, so a 1D chart is the stock's, drawn under a token hero price.
    public let basis: MarketPriceBasis?
    public let basisSymbol: String?
    public let market: MarketStatusDTO?

    public init(
        points: [AssetChartPointDTO],
        emptyReason: String? = nil,
        previousCloseUsdcMicros: Int64? = nil,
        range: AssetChartRange? = nil,
        source: AssetChartSource? = nil,
        basis: MarketPriceBasis? = nil,
        basisSymbol: String? = nil,
        market: MarketStatusDTO? = nil
    ) {
        self.points = points
        self.emptyReason = emptyReason
        self.previousCloseUsdcMicros = previousCloseUsdcMicros
        self.range = range
        self.source = source
        self.basis = basis
        self.basisSymbol = basisSymbol
        self.market = market
    }

    private enum CodingKeys: String, CodingKey {
        case points, emptyReason, previousCloseUsdcMicros, range, source, basis, basisSymbol, market
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        points = try container.decodeIfPresent([AssetChartPointDTO].self, forKey: .points) ?? []
        emptyReason = try container.decodeIfPresent(String.self, forKey: .emptyReason)
        previousCloseUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .previousCloseUsdcMicros)
        // An unrecognised range name is not worth failing the whole series over;
        // a caller matching ranges just will not match this one.
        range = try container.decodeIfPresent(String.self, forKey: .range).flatMap(AssetChartRange.init(rawValue:))
        source = try container.decodeIfPresent(AssetChartSource.self, forKey: .source)
        basis = try container.decodeIfPresent(MarketPriceBasis.self, forKey: .basis)
        basisSymbol = try container.decodeIfPresent(String.self, forKey: .basisSymbol)
        market = try container.decodeIfPresent(MarketStatusDTO.self, forKey: .market)
    }
}

public enum AssetChartRange: String, Codable, CaseIterable, Sendable {
    case oneDay = "1D"
    case oneWeek = "1W"
    case oneMonth = "1M"
    case threeMonths = "3M"
    case oneYear = "1Y"
    case all = "ALL"

    public var label: String { rawValue }

    /// True for the ranges the day-change baseline applies to. Over a month or a
    /// year, "previous close" means the close before the window, which is not the
    /// number a day chart's dashed line is about.
    public var showsPreviousCloseBaseline: Bool { self == .oneDay }
}
