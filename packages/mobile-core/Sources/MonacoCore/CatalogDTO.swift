import Foundation

public struct CatalogAssetDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let name: String
    /// When false, Jupiter returned no route for a probe quote; nil means unknown (legacy responses).
    public let routable: Bool?

    public init(symbol: String, name: String, routable: Bool? = nil) {
        self.symbol = symbol
        self.name = name
        self.routable = routable
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
