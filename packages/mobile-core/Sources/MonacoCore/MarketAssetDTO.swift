import Foundation

public struct MarketAssetDTO: Codable, Equatable, Sendable, Identifiable {
    public let symbol: String
    public let name: String
    public let tokenAddress: String
    public let routable: Bool
    /// The token's Chainlink total-return mark, per token.
    public let priceUsdcMicros: Int64?
    /// The underlying equity's move against its previous regular-session close, as a
    /// decimal ratio. It is the stock's day move, not the token's.
    public let change24h: String?

    public var id: String { symbol }

    /// Picker rows use this, not a live DEX probe. A listed token is buyable even when
    /// `routable` was cached false from an old probe.
    public var canBuy: Bool {
        routable || !tokenAddress.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    public init(
        symbol: String,
        name: String,
        tokenAddress: String,
        routable: Bool,
        priceUsdcMicros: Int64? = nil,
        change24h: String? = nil
    ) {
        self.symbol = symbol
        self.name = name
        self.tokenAddress = tokenAddress
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
    /// (ask - bid) / mid of the two Kyber probes, in basis points. Nil unless both
    /// probes found a route.
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

/// Which instrument a figure is about.
///
/// A B20 token and the equity it tracks are different units. The token's price is
/// the equity's price times the token's multiplier (which grows as cash dividends are
/// reinvested and moves with splits), and its pools trade when the exchange is shut.
/// Pyth serves the underlying, so the chart and the stats grid are the equity's, per
/// share, while the hero price is the token's Chainlink total-return mark, per token.
/// A hero price above "the day's high" is therefore two instruments, not a bug, and
/// the label is what makes that readable.
public enum MarketPriceBasis: String, Codable, Sendable {
    /// The equity on its home exchange: `Equity.US.AAPL/USD`, per share.
    case underlying
    /// The B20 token on Base, per token: its Chainlink rounds or a Kyber quote.
    case token
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = MarketPriceBasis(rawValue: raw) ?? .unknown
    }
}

/// The stats grid. Every field is optional because the backend omits a cell it
/// could not source rather than sending a placeholder.
///
/// Market cap, P/E and dividend yield are deliberately absent: nothing behind a B20
/// token publishes them.
public struct AssetStatsDTO: Codable, Equatable, Sendable {
    public let openUsdcMicros: Int64?
    public let highUsdcMicros: Int64?
    public let lowUsdcMicros: Int64?
    public let previousCloseUsdcMicros: Int64?
    public let week52HighUsdcMicros: Int64?
    public let week52LowUsdcMicros: Int64?
    /// Round-trip trading cost in basis points, from the Kyber probes.
    public let spreadBps: Int?
    /// Pyth's own confidence interval around the latest equity price, in USDC micros.
    public let confUsdcMicros: Int64?
    /// Which instrument the price cells describe. They come from Pyth's equity feed,
    /// so the grid has to be headed with `basisSymbol` or it reads as though those
    /// numbers were about the token the user is buying.
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

    /// Header for the price cells, e.g. "AAPL on its home exchange". Nil when the
    /// backend did not say which instrument the cells are about, in which case the
    /// grid is drawn unheaded rather than under a guess.
    public var basisCaption: String? {
        MarketPriceBasisCaption.caption(basis: basis, symbol: basisSymbol)
    }
}

/// The one place the basis label is worded, shared by the stats grid and the chart.
public enum MarketPriceBasisCaption {
    public static func caption(basis: MarketPriceBasis?, symbol: String?) -> String? {
        guard let symbol, !symbol.isEmpty else { return nil }
        switch basis {
        case .underlying: return "\(AssetSymbolFormatter.display(symbol)) on its home exchange"
        case .token: return "\(AssetSymbolFormatter.display(symbol)) token on Base"
        case .unknown, nil: return nil
        }
    }
}

/// Where a line on the stock-vs-token card came from.
public enum ReferenceQuoteSource: String, Codable, Sendable {
    /// Pyth's price for the underlying equity, per share. A reference line only.
    case pythEquity = "pyth_equity"
    /// The mid of a Kyber buy and sell probe on Base, per token.
    case dexKyber = "dex_kyber"
    /// The token's Chainlink total-return mark, per token. The premium is measured
    /// against it.
    case chainlinkTRV = "chainlink_trv"
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = ReferenceQuoteSource(rawValue: raw) ?? .unknown
    }
}

/// How much a reference quote can be trusted right now.
public enum ReferenceQuoteStatus: String, Codable, Sendable {
    /// The price is current.
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
    /// Our Pyth key is not entitled to the feed. Retrying will not fix it.
    case notEntitled = "not_entitled"
    /// The backend has no Pyth key configured.
    case notConfigured = "not_configured"
    /// Kyber found no route for one of the two probes.
    case noRoute = "no_route"
    /// The upstream failed or answered with something unusable.
    case upstreamError = "upstream_error"
}

/// One line of the stock-vs-token card.
public struct ReferenceQuoteDTO: Codable, Equatable, Sendable {
    public let source: ReferenceQuoteSource
    public let status: ReferenceQuoteStatus
    public let priceUsdcMicros: Int64?
    /// Pyth's confidence interval. Never set on a Kyber or Chainlink line.
    public let confUsdcMicros: Int64?
    /// When the price was struck. Nil for a Kyber quote, which does not say.
    public let publishedAt: Date?
    public let reason: String?
    /// The probes behind a `dexKyber` mid.
    public let bidUsdcMicros: Int64?
    public let askUsdcMicros: Int64?

    public init(
        source: ReferenceQuoteSource,
        status: ReferenceQuoteStatus,
        priceUsdcMicros: Int64? = nil,
        confUsdcMicros: Int64? = nil,
        publishedAt: Date? = nil,
        reason: String? = nil,
        bidUsdcMicros: Int64? = nil,
        askUsdcMicros: Int64? = nil
    ) {
        self.source = source
        self.status = status
        self.priceUsdcMicros = priceUsdcMicros
        self.confUsdcMicros = confUsdcMicros
        self.publishedAt = publishedAt
        self.reason = reason
        self.bidUsdcMicros = bidUsdcMicros
        self.askUsdcMicros = askUsdcMicros
    }

    /// True when this line carries a number worth showing.
    public var isPriced: Bool {
        status != .unavailable && (priceUsdcMicros ?? 0) > 0
    }

    public var unavailableReason: ReferenceQuoteReason? {
        reason.flatMap(ReferenceQuoteReason.init(rawValue:))
    }

    private enum CodingKeys: String, CodingKey {
        case source, status, priceUsdcMicros, confUsdcMicros, publishedAt, reason, bidUsdcMicros, askUsdcMicros
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        source = try container.decodeIfPresent(ReferenceQuoteSource.self, forKey: .source) ?? .unknown
        status = try container.decodeIfPresent(ReferenceQuoteStatus.self, forKey: .status) ?? .unknown
        priceUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .priceUsdcMicros)
        confUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .confUsdcMicros)
        publishedAt = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .publishedAt)?.date
        reason = try container.decodeIfPresent(String.self, forKey: .reason)
        bidUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .bidUsdcMicros)
        askUsdcMicros = try container.decodeIfPresent(Int64.self, forKey: .askUsdcMicros)
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(source, forKey: .source)
        try container.encode(status, forKey: .status)
        try container.encodeIfPresent(priceUsdcMicros, forKey: .priceUsdcMicros)
        try container.encodeIfPresent(confUsdcMicros, forKey: .confUsdcMicros)
        try container.encodeIfPresent(publishedAt.map(MonacoTimestamp.init(date:)), forKey: .publishedAt)
        try container.encodeIfPresent(reason, forKey: .reason)
        try container.encodeIfPresent(bidUsdcMicros, forKey: .bidUsdcMicros)
        try container.encodeIfPresent(askUsdcMicros, forKey: .askUsdcMicros)
    }
}

/// The token in its pools against its own mark, with the equity it tracks as a
/// separate reference line.
///
/// `premiumBps` is `token` against `mark`: both are per token and both carry the
/// multiplier. The equity line is per share, so no premium is measured against it —
/// the multiplier would read as a premium that grows with every dividend.
public struct StockVsTokenDTO: Codable, Equatable, Sendable {
    /// The Kyber quote-implied mid.
    public let token: ReferenceQuoteDTO
    /// The Chainlink total-return mark (equal to the hero price).
    public let mark: ReferenceQuoteDTO
    /// Pyth's price for the underlying, per share.
    public let equity: ReferenceQuoteDTO
    /// The underlying's ticker, "AAPL".
    public let equitySymbol: String?
    /// How far the token trades above (positive) or below (negative) its mark. Nil
    /// unless both carry a real price.
    public let premiumBps: Int?
    /// Kyber ask against bid, in basis points.
    public let spreadBps: Int?
    public let asOf: Date?

    public init(
        token: ReferenceQuoteDTO,
        mark: ReferenceQuoteDTO,
        equity: ReferenceQuoteDTO,
        equitySymbol: String? = nil,
        premiumBps: Int? = nil,
        spreadBps: Int? = nil,
        asOf: Date? = nil
    ) {
        self.token = token
        self.mark = mark
        self.equity = equity
        self.equitySymbol = equitySymbol
        self.premiumBps = premiumBps
        self.spreadBps = spreadBps
        self.asOf = asOf
    }

    private enum CodingKeys: String, CodingKey {
        case token, mark, equity, equitySymbol, premiumBps, spreadBps, asOf
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        // A card missing a line is malformed, not partial: the backend always sends
        // all three, marking any it could not price as unavailable.
        token = try container.decode(ReferenceQuoteDTO.self, forKey: .token)
        mark = try container.decode(ReferenceQuoteDTO.self, forKey: .mark)
        equity = try container.decode(ReferenceQuoteDTO.self, forKey: .equity)
        equitySymbol = try container.decodeIfPresent(String.self, forKey: .equitySymbol)
        premiumBps = try container.decodeIfPresent(Int.self, forKey: .premiumBps)
        spreadBps = try container.decodeIfPresent(Int.self, forKey: .spreadBps)
        asOf = try container.decodeIfPresent(MonacoTimestamp.self, forKey: .asOf)?.date
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(token, forKey: .token)
        try container.encode(mark, forKey: .mark)
        try container.encode(equity, forKey: .equity)
        try container.encodeIfPresent(equitySymbol, forKey: .equitySymbol)
        try container.encodeIfPresent(premiumBps, forKey: .premiumBps)
        try container.encodeIfPresent(spreadBps, forKey: .spreadBps)
        try container.encodeIfPresent(asOf.map(MonacoTimestamp.init(date:)), forKey: .asOf)
    }
}

public struct AssetDetailDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let name: String
    public let tokenAddress: String
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
        tokenAddress: String,
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
        self.tokenAddress = tokenAddress
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
        case symbol, name, tokenAddress, routable, priceUsdcMicros, change24h, liquidity
        case marketSession, afterHours, market, stats, stockVsToken
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        symbol = try container.decode(String.self, forKey: .symbol)
        name = try container.decode(String.self, forKey: .name)
        tokenAddress = try container.decode(String.self, forKey: .tokenAddress)
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
    /// Candle fields, present when the series came from Benchmarks. Zero when the
    /// series came from a source that only knows a price at an instant (the Hermes
    /// sampler, the Chainlink rounds).
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

/// Which upstream produced a series.
public enum AssetChartSource: String, Codable, Sendable {
    /// Pyth Benchmarks candles for the underlying.
    case benchmarks
    /// The sparse Hermes sampler for the underlying, when Benchmarks is down.
    case hermes
    /// The token's own Chainlink total-return rounds, the last fallback.
    case chainlink
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
    /// Absent when the source does not know one. The Hermes sampler and the
    /// Chainlink rounds never do — any number they could offer would be a point
    /// already drawn in `points`. Draw nothing rather than a line through t0.
    public let previousCloseUsdcMicros: Int64?
    /// The range this series was built for. A response that names a range the user
    /// has already tapped away from should be discarded, not drawn.
    public let range: AssetChartRange?
    public let source: AssetChartSource?
    /// Which instrument the curve is: `underlying` for Pyth, `token` for the
    /// Chainlink fallback.
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

    /// Caption for the curve, e.g. "AAPL on its home exchange".
    public var basisCaption: String? {
        MarketPriceBasisCaption.caption(basis: basis, symbol: basisSymbol)
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
