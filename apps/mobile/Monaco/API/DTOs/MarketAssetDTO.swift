import Foundation

struct MarketAssetDTO: Codable, Equatable, Identifiable {
    let symbol: String
    let name: String
    let solanaMint: String
    let routable: Bool
    let priceUsdcMicros: Int64?
    let change24h: String?
    let logoUrl: String?

    var id: String { symbol }
}

struct ListMarketAssetsResponse: Codable, Equatable {
    let assets: [MarketAssetDTO]
    let hasMore: Bool
}

struct PopularAssetsResponse: Codable, Equatable {
    let assets: [MarketAssetDTO]
}

struct AssetLiquidityDTO: Codable, Equatable {
    let label: String
    let routable: Bool
    let buyProbeUsdcMicros: Int64
    let buyProbeOutAmount: String?
    let sellProbeInAmount: String?
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

struct AssetChartPointDTO: Codable, Equatable, Identifiable {
    let timestamp: Int64
    let priceUsdcMicros: Int64

    var id: Int64 { timestamp }

    var date: Date {
        Date(timeIntervalSince1970: TimeInterval(timestamp))
    }

    var chartValue: Double {
        Double(priceUsdcMicros) / 1_000_000
    }
}

struct AssetChartDTO: Codable, Equatable {
    let points: [AssetChartPointDTO]
    let emptyReason: String?
}

enum AssetChartRange: String, CaseIterable {
    case oneDay = "1D"
    case oneWeek = "1W"
    case oneMonth = "1M"

    var label: String { rawValue }
}
