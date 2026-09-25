import Foundation
import MonacoCore

/// Strips xStocks catalog branding from user-facing asset names.
enum AssetDisplayName {
    static func format(catalogName: String, kind: AssetKind = .stock) -> String {
        CatalogAssetNameFormatter.format(catalogName, kind: kind)
    }
}

extension CatalogAssetDTO {
    var displayName: String {
        AssetCatalogDisplayName.format(catalogName: name, symbol: symbol, kind: resolvedKind)
    }

    var displayTicker: String {
        AssetSymbolFormatter.display(symbol, kind: resolvedKind)
    }
}

extension MarketAssetDTO {
    var displayName: String {
        AssetCatalogDisplayName.format(catalogName: name, symbol: symbol, kind: resolvedKind)
    }

    var displayTicker: String {
        AssetSymbolFormatter.display(symbol, kind: resolvedKind)
    }
}

extension AssetDetailDTO {
    var displayName: String {
        AssetCatalogDisplayName.format(catalogName: name, symbol: symbol, kind: resolvedKind)
    }

    var displayTicker: String {
        AssetSymbolFormatter.display(symbol, kind: resolvedKind)
    }
}
