import Foundation

struct CatalogAssetDTO: Codable, Equatable, Identifiable {
    let symbol: String
    let name: String
    let routable: Bool?

    var id: String { symbol }

    var isTradable: Bool {
        routable ?? true
    }
}

struct SearchAssetsResponse: Codable, Equatable {
    let assets: [CatalogAssetDTO]
    let hasMore: Bool
}
