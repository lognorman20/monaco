import Foundation

public struct MarketAssetDTO: Codable, Equatable, Sendable, Identifiable {
    public let symbol: String
    public let name: String
    public let tokenAddress: String
    public let routable: Bool
    public let priceUsdcMicros: Int64?
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

    public init(assets: [MarketAssetDTO], hasMore: Bool) {
        self.assets = assets
        self.hasMore = hasMore
    }
}

public struct PopularAssetsResponseDTO: Codable, Equatable, Sendable {
    public let assets: [MarketAssetDTO]

    public init(assets: [MarketAssetDTO]) {
        self.assets = assets
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

public struct AssetDetailDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let name: String
    public let tokenAddress: String
    public let routable: Bool
    public let priceUsdcMicros: Int64?
    public let change24h: String?
    public let liquidity: AssetLiquidityDTO

    public init(
        symbol: String,
        name: String,
        tokenAddress: String,
        routable: Bool,
        priceUsdcMicros: Int64? = nil,
        change24h: String? = nil,
        liquidity: AssetLiquidityDTO
    ) {
        self.symbol = symbol
        self.name = name
        self.tokenAddress = tokenAddress
        self.routable = routable
        self.priceUsdcMicros = priceUsdcMicros
        self.change24h = change24h
        self.liquidity = liquidity
    }
}

public struct AssetChartPointDTO: Codable, Equatable, Sendable, Identifiable {
    public let timestamp: Int64
    public let priceUsdcMicros: Int64

    public var id: Int64 { timestamp }

    public var date: Date {
        Date(timeIntervalSince1970: TimeInterval(timestamp))
    }

    public var chartValue: Double {
        Double(priceUsdcMicros) / 1_000_000
    }

    public init(timestamp: Int64, priceUsdcMicros: Int64) {
        self.timestamp = timestamp
        self.priceUsdcMicros = priceUsdcMicros
    }
}

public struct AssetChartDTO: Codable, Equatable, Sendable {
    public let points: [AssetChartPointDTO]
    public let emptyReason: String?

    public init(points: [AssetChartPointDTO], emptyReason: String? = nil) {
        self.points = points
        self.emptyReason = emptyReason
    }
}

public enum AssetChartRange: String, CaseIterable, Sendable {
    case oneDay = "1D"
    case oneWeek = "1W"
    case oneMonth = "1M"

    public var label: String { rawValue }
}
