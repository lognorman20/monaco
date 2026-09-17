import Foundation

public struct CatalogAssetDTO: Codable, Equatable, Sendable {
    public let symbol: String
    public let name: String

    public init(symbol: String, name: String) {
        self.symbol = symbol
        self.name = name
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
