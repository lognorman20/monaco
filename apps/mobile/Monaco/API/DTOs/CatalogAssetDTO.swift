import Foundation

struct CatalogAssetDTO: Codable, Equatable, Identifiable {
    let symbol: String
    let name: String

    var id: String { symbol }
}

struct SearchAssetsResponse: Codable, Equatable {
    let assets: [CatalogAssetDTO]
    let hasMore: Bool
}
