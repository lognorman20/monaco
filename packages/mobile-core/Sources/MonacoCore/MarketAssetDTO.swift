import Foundation

public struct MarketAssetDTO: Codable, Equatable, Sendable, Identifiable {
    public let symbol: String
    public let name: String
    public let solanaMint: String
    public let routable: Bool
    public let priceUsdcMicros: Int64?
    public let change24h: String?
    public let kind: AssetKind?
    public let source: String?
    public let issuer: String?
    public let underlyingId: String?
    public let tokenDecimals: Int?
    public let sector: String?
    public let logoUrl: String?
    public let alwaysOpen: Bool?
    public let referenceMarkUsdcMicros: Int64?
    public let referenceValuationUsd: Int64?
    public let referenceUpdatedAt: String?
    public let premiumBps: Int?
    public let holders: Int?
    public let variantCount: Int?

    public var id: String { symbol }

    public var resolvedKind: AssetKind { kind ?? .stock }
    public var resolvedDecimals: Int { tokenDecimals ?? AssetCatalogDefaults.decimals }

    public init(
        symbol: String,
        name: String,
        solanaMint: String,
        routable: Bool,
        priceUsdcMicros: Int64? = nil,
        change24h: String? = nil,
        kind: AssetKind? = nil,
        source: String? = nil,
        issuer: String? = nil,
        underlyingId: String? = nil,
        tokenDecimals: Int? = nil,
        sector: String? = nil,
        logoUrl: String? = nil,
        alwaysOpen: Bool? = nil,
        referenceMarkUsdcMicros: Int64? = nil,
        referenceValuationUsd: Int64? = nil,
        referenceUpdatedAt: String? = nil,
        premiumBps: Int? = nil,
        holders: Int? = nil,
        variantCount: Int? = nil
    ) {
        self.symbol = symbol
        self.name = name
        self.solanaMint = solanaMint
        self.routable = routable
        self.priceUsdcMicros = priceUsdcMicros
        self.change24h = change24h
        self.kind = kind
        self.source = source
        self.issuer = issuer
        self.underlyingId = underlyingId
        self.tokenDecimals = tokenDecimals
        self.sector = sector
        self.logoUrl = logoUrl
        self.alwaysOpen = alwaysOpen
        self.referenceMarkUsdcMicros = referenceMarkUsdcMicros
        self.referenceValuationUsd = referenceValuationUsd
        self.referenceUpdatedAt = referenceUpdatedAt
        self.premiumBps = premiumBps
        self.holders = holders
        self.variantCount = variantCount
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

public struct AssetVariantDTO: Codable, Equatable, Sendable, Identifiable {
    public let symbol: String
    public let issuer: String
    public let solanaMint: String
    public let priceUsdcMicros: Int64?
    public let liquidityUsd: String?
    public let routable: Bool

    public var id: String { symbol }

    public init(
        symbol: String,
        issuer: String,
        solanaMint: String,
        priceUsdcMicros: Int64? = nil,
        liquidityUsd: String? = nil,
        routable: Bool
    ) {
        self.symbol = symbol
        self.issuer = issuer
        self.solanaMint = solanaMint
        self.priceUsdcMicros = priceUsdcMicros
        self.liquidityUsd = liquidityUsd
        self.routable = routable
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
    public let kind: AssetKind?
    public let source: String?
    public let issuer: String?
    public let underlyingId: String?
    public let tokenDecimals: Int?
    public let sector: String?
    public let logoUrl: String?
    public let alwaysOpen: Bool?
    public let referenceMarkUsdcMicros: Int64?
    public let referenceValuationUsd: Int64?
    public let referenceUpdatedAt: String?
    public let premiumBps: Int?
    public let holders: Int?
    public let variantCount: Int?
    public let variants: [AssetVariantDTO]?

    public var resolvedKind: AssetKind { kind ?? .stock }
    public var resolvedDecimals: Int { tokenDecimals ?? AssetCatalogDefaults.decimals }

    public init(
        symbol: String,
        name: String,
        solanaMint: String,
        routable: Bool,
        priceUsdcMicros: Int64? = nil,
        change24h: String? = nil,
        liquidity: AssetLiquidityDTO,
        kind: AssetKind? = nil,
        source: String? = nil,
        issuer: String? = nil,
        underlyingId: String? = nil,
        tokenDecimals: Int? = nil,
        sector: String? = nil,
        logoUrl: String? = nil,
        alwaysOpen: Bool? = nil,
        referenceMarkUsdcMicros: Int64? = nil,
        referenceValuationUsd: Int64? = nil,
        referenceUpdatedAt: String? = nil,
        premiumBps: Int? = nil,
        holders: Int? = nil,
        variantCount: Int? = nil,
        variants: [AssetVariantDTO]? = nil
    ) {
        self.symbol = symbol
        self.name = name
        self.solanaMint = solanaMint
        self.routable = routable
        self.priceUsdcMicros = priceUsdcMicros
        self.change24h = change24h
        self.liquidity = liquidity
        self.kind = kind
        self.source = source
        self.issuer = issuer
        self.underlyingId = underlyingId
        self.tokenDecimals = tokenDecimals
        self.sector = sector
        self.logoUrl = logoUrl
        self.alwaysOpen = alwaysOpen
        self.referenceMarkUsdcMicros = referenceMarkUsdcMicros
        self.referenceValuationUsd = referenceValuationUsd
        self.referenceUpdatedAt = referenceUpdatedAt
        self.premiumBps = premiumBps
        self.holders = holders
        self.variantCount = variantCount
        self.variants = variants
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
