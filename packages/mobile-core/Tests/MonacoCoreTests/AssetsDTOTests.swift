import XCTest
import MonacoCore

final class AssetsDTOTests: XCTestCase {
    func testChartRangesExposeNativePickerIdentityAndTitles() {
        XCTAssertEqual(AssetChartRange.allCases.map(\.id), ["1D", "1W", "1M"])
        XCTAssertEqual(AssetChartRange.allCases.map(\.title), ["1D", "1W", "1M"])
    }

    func testAssetDetailDecodesLiquidityWithoutOptionalSellInput() throws {
        let json = """
        {"symbol":"AAPLx","name":"Apple","solanaMint":"mint","routable":true,
         "liquidity":{"label":"available","routable":true,"buyProbeUsdcMicros":1000000}}
        """
        let detail = try JSONDecoder().decode(AssetDetailDTO.self, from: Data(json.utf8))
        XCTAssertNil(detail.liquidity.sellProbeInAmount)
        XCTAssertNil(detail.liquidity.sellProbeOutAmount)
        XCTAssertNil(detail.priceUsdcMicros)
    }

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
