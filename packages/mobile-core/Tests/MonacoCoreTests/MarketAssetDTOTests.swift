import XCTest
@testable import MonacoCore

final class MarketAssetDTOTests: XCTestCase {
    func testMarketAssetDTO_decodesTokenAddress() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_assets", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(
            ListMarketAssetsResponseDTO.self,
            from: Data(contentsOf: fixtureURL)
        )
        XCTAssertNotNil(dto.assets[0].tokenAddress)
        XCTAssertFalse(dto.assets[0].tokenAddress.isEmpty)
    }

    func testListMarketAssets_decodesCatalogPage() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_assets", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(
            ListMarketAssetsResponseDTO.self,
            from: Data(contentsOf: fixtureURL)
        )

        XCTAssertEqual(dto.assets.count, 1)
        XCTAssertEqual(dto.assets[0].symbol, "AAPLc")
        XCTAssertFalse(dto.assets[0].tokenAddress.isEmpty)
        XCTAssertEqual(dto.assets[0].priceUsdcMicros, 185_000_000)
        XCTAssertTrue(dto.hasMore)
        XCTAssertEqual(dto.market?.session, .open)
    }

    func testAssetDetail_decodesLiquiditySnippet() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_asset_detail", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(AssetDetailDTO.self, from: Data(contentsOf: fixtureURL))

        XCTAssertEqual(dto.liquidity.label, "Via DEX")
        XCTAssertEqual(dto.liquidity.buyProbeOutAmount, "430000")
        XCTAssertEqual(dto.liquidity.spreadBps, 59)
        XCTAssertTrue(dto.routable)
        XCTAssertEqual(dto.marketSession, .afterHours)
        XCTAssertTrue(dto.afterHours)
        XCTAssertEqual(dto.stats?.previousCloseUsdcMicros, 226_500_000)
        XCTAssertEqual(dto.stats?.basisCaption, "AAPL on its home exchange")
        XCTAssertEqual(dto.stockVsToken?.equity.status, .stale)
        XCTAssertEqual(dto.stockVsToken?.token.source, .dexKyber)
        XCTAssertEqual(dto.stockVsToken?.mark.source, .chainlinkTRV)
        XCTAssertEqual(dto.stockVsToken?.premiumBps, 50)
        XCTAssertEqual(dto.stockVsToken?.spreadBps, dto.liquidity.spreadBps)
        XCTAssertNotNil(dto.stockVsToken?.mark.publishedAt)
        XCTAssertNotNil(dto.stockVsToken?.token.probedAt)
        XCTAssertEqual(dto.stockDayMove?.caption, "AAPL day move")
    }

    func testAssetChart_decodesPoints() throws {
        let fixtureURL = try XCTUnwrap(
            Bundle.module.url(forResource: "market_asset_chart", withExtension: "json")
        )
        let dto = try JSONDecoder().decode(AssetChartDTO.self, from: Data(contentsOf: fixtureURL))

        XCTAssertEqual(dto.points.count, 2)
        XCTAssertNil(dto.emptyReason)
        XCTAssertEqual(dto.points[1].priceUsdcMicros, 185_000_000)
        XCTAssertEqual(dto.previousCloseUsdcMicros, 179_000_000)
        XCTAssertEqual(dto.range, .oneDay)
        XCTAssertEqual(dto.source, .benchmarks)
        XCTAssertEqual(dto.basis, .underlying)
        XCTAssertTrue(dto.points.allSatisfy(\.hasCandle))
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
