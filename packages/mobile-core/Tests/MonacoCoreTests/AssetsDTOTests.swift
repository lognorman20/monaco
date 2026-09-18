import XCTest
@testable import MonacoCore

final class AssetsDTOTests: XCTestCase {
    func testMarketAssetDTO_decodesPricedCatalogRow() throws {
        let json = """
        {
          "symbol": "AAPLx",
          "name": "Apple",
          "solanaMint": "MintAAPL",
          "routable": true,
          "priceUsdcMicros": 185000000,
          "change24h": "0.012500"
        }
        """
        let asset = try JSONDecoder().decode(MarketAssetDTO.self, from: Data(json.utf8))
        XCTAssertEqual(asset.symbol, "AAPLx")
        XCTAssertEqual(asset.priceUsdcMicros, 185_000_000)
        XCTAssertEqual(asset.change24h, "0.012500")
    }

    func testAssetChartResponseDTO_decodesEmptySeries() throws {
        let json = """
        {
          "points": [],
          "emptyReason": "price history unavailable"
        }
        """
        let chart = try JSONDecoder().decode(AssetChartResponseDTO.self, from: Data(json.utf8))
        XCTAssertTrue(chart.points.isEmpty)
        XCTAssertEqual(chart.emptyReason, "price history unavailable")
    }
}
