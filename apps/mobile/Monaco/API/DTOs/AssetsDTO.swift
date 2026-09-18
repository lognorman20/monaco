import Foundation

struct MarketAssetDTO: Codable, Equatable, Identifiable {
    let symbol: String
    let name: String
    let solanaMint: String
    let routable: Bool
    let priceUsdcMicros: Int64?
    let change24h: String?

    var id: String { symbol }
}

struct ListMarketAssetsResponse: Codable, Equatable {
    let assets: [MarketAssetDTO]
    let hasMore: Bool
}

struct PopularMarketAssetsResponse: Codable, Equatable {
    let assets: [MarketAssetDTO]
}

struct AssetLiquidityDTO: Codable, Equatable {
    let label: String
    let routable: Bool
    let buyProbeUsdcMicros: Int64
    let buyProbeOutAmount: String?
    let sellProbeOutAmount: String?
    let spreadBps: Int?
}

struct AssetDetailDTO: Codable, Equatable {
    let symbol: String
    let name: String
    let solanaMint: String
    let routable: Bool
    let priceUsdcMicros: Int64?
    let change24h: String?
    let liquidity: AssetLiquidityDTO
}

enum AssetChartRange: String, CaseIterable, Identifiable {
    case oneDay = "1D"
    case oneWeek = "1W"
    case oneMonth = "1M"

    var id: String { rawValue }

    var title: String {
        switch self {
        case .oneDay: "1D"
        case .oneWeek: "1W"
        case .oneMonth: "1M"
        }
    }
}

struct AssetChartPointDTO: Codable, Equatable, Identifiable {
    let timestamp: Int64
    let priceUsdcMicros: Int64

    var id: Int64 { timestamp }
}

struct AssetChartResponse: Codable, Equatable {
    let points: [AssetChartPointDTO]
    let emptyReason: String?
}

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
