import XCTest
@testable import MonacoCore

final class MarketAssetDTOTests: XCTestCase {
    func testListMarketAssets_decodesCatalogPage() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_assets", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(
            ListMarketAssetsResponseDTO.self,
            from: Data(contentsOf: fixtureURL)
        )

        XCTAssertEqual(dto.assets.count, 1)
        XCTAssertEqual(dto.assets[0].symbol, "AAPLx")
        XCTAssertEqual(dto.assets[0].priceUsdcMicros, 185_000_000)
        XCTAssertTrue(dto.hasMore)
    }

    func testAssetDetail_decodesLiquiditySnippet() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_asset_detail", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(AssetDetailDTO.self, from: Data(contentsOf: fixtureURL))

        XCTAssertEqual(dto.liquidity.label, "Via Jupiter")
        XCTAssertEqual(dto.liquidity.buyProbeOutAmount, "100000000")
        XCTAssertEqual(dto.liquidity.spreadBps, 12)
        XCTAssertTrue(dto.routable)
    }

    func testAssetChart_decodesPoints() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_asset_chart", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(AssetChartDTO.self, from: Data(contentsOf: fixtureURL))

        XCTAssertEqual(dto.points.count, 2)
        XCTAssertNil(dto.emptyReason)
        XCTAssertEqual(dto.points[1].priceUsdcMicros, 185_000_000)
    }

    func testAssetChart_decodesEmptySeriesWithReason() throws {
        let json = """
        {
          "points": [],
          "emptyReason": "price history unavailable"
        }
        """
        let dto = try JSONDecoder().decode(AssetChartDTO.self, from: Data(json.utf8))
        XCTAssertEqual(dto.points, [])
        XCTAssertEqual(dto.emptyReason, "price history unavailable")
    }
}
