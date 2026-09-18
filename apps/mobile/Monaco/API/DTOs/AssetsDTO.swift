import Foundation
import MonacoCore

// The shared package owns the wire schema; aliases preserve native call sites.
typealias MarketAssetDTO = MonacoCore.MarketAssetDTO
typealias ListMarketAssetsResponse = MonacoCore.ListMarketAssetsResponseDTO
typealias PopularMarketAssetsResponse = MonacoCore.PopularMarketAssetsResponseDTO
typealias AssetLiquidityDTO = MonacoCore.AssetLiquidityDTO
typealias AssetDetailDTO = MonacoCore.AssetDetailDTO
typealias AssetChartRange = MonacoCore.AssetChartRange
typealias AssetChartPointDTO = MonacoCore.AssetChartPointDTO
typealias AssetChartResponse = MonacoCore.AssetChartResponseDTO

extension MarketAssetDTO {
    var displayName: String {
        AssetDisplayName.format(catalogName: name)
    }

    var displaySymbol: String {
        AssetSymbolFormatter.format(symbol)
    }
}

extension AssetDetailDTO {
    var displayName: String {
        AssetDisplayName.format(catalogName: name)
    }

    var displaySymbol: String {
        AssetSymbolFormatter.format(symbol)
    }
}

private enum AssetSymbolFormatter {
    static func format(_ symbol: String) -> String {
        let trimmed = symbol.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return trimmed }
        if trimmed.lowercased().hasSuffix("x"), trimmed.count > 1 {
            return String(trimmed.dropLast())
        }
        return trimmed
    }
}
