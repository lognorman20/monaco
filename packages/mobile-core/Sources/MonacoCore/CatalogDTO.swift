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

    public init(assets: [CatalogAssetDTO]) {
        self.assets = assets
    }
}
