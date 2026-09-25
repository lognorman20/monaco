import Foundation

public struct CatalogAssetDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let name: String
    /// When false, Jupiter returned no route for a probe quote; nil means unknown (legacy responses).
    public let routable: Bool?
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

    public var resolvedKind: AssetKind { kind ?? .stock }
    public var resolvedDecimals: Int { tokenDecimals ?? AssetCatalogDefaults.decimals }

    public init(
        symbol: String,
        name: String,
        routable: Bool? = nil,
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
        self.routable = routable
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

    public var isTradable: Bool {
        routable ?? true
    }
}

public struct SearchAssetsResponseDTO: Codable, Equatable, Sendable {
    public let assets: [CatalogAssetDTO]
    public let hasMore: Bool

    public init(assets: [CatalogAssetDTO], hasMore: Bool) {
        self.assets = assets
        self.hasMore = hasMore
    }
}
