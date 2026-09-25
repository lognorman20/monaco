import XCTest
@testable import MonacoCore

final class TesseraMobileCoreTests: XCTestCase {
    func testMarketAssetDTO_decodesWithoutKind_defaultsToStock8() throws {
        let json = """
        {
          "symbol": "AAPLx",
          "name": "Apple",
          "solanaMint": "mint",
          "routable": true,
          "priceUsdcMicros": 185000000
        }
        """
        let dto = try JSONDecoder().decode(MarketAssetDTO.self, from: Data(json.utf8))
        XCTAssertEqual(dto.resolvedKind, .stock)
        XCTAssertEqual(dto.resolvedDecimals, 8)
    }

    func testMarketAssetDTO_decodesPreIpoFields() throws {
        let json = """
        {
          "symbol": "tSpaceX",
          "name": "T-SpaceX",
          "solanaMint": "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v",
          "routable": true,
          "kind": "pre_ipo",
          "tokenDecimals": 9,
          "sector": "Aerospace",
          "premiumBps": -2700,
          "referenceMarkUsdcMicros": 774000000
        }
        """
        let dto = try JSONDecoder().decode(MarketAssetDTO.self, from: Data(json.utf8))
        XCTAssertEqual(dto.resolvedKind, .preIpo)
        XCTAssertEqual(dto.resolvedDecimals, 9)
        XCTAssertEqual(dto.sector, "Aerospace")
        XCTAssertEqual(dto.premiumBps, -2700)
    }

    func testAssetSymbolFormatter_preIpo_keepsTSpaceX() {
        XCTAssertEqual(AssetSymbolFormatter.display("tSpaceX", kind: .preIpo), "tSpaceX")
    }

    func testDisplayName_preIpo_stripsTPrefix() {
        XCTAssertEqual(CatalogAssetNameFormatter.format("T-SpaceX", kind: .preIpo), "SpaceX")
        XCTAssertEqual(
            AssetCatalogDisplayName.format(catalogName: "T-SpaceX", symbol: "tSpaceX", kind: .preIpo),
            "SpaceX"
        )
    }

    func testTokenQuantity_nineDecimals_preIpo_labelsTokens() {
        XCTAssertEqual(
            TokenQuantityFormatter.label(fromAtomics: "1500000000", decimals: 9, kind: .preIpo),
            "1.5 tokens"
        )
        XCTAssertEqual(
            TokenQuantityFormatter.label(fromAtomics: "1000000000", decimals: 9, kind: .preIpo),
            "1 token"
        )
    }

    func testMainFlowCopyAudit_preIpoStrings_pass() {
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean(PreIpoCopy.auditedStrings))
    }

    func testBuyQuoteDTO_kindStaysBuySell_assetKindPreIpo() throws {
        let json = """
        {
          "symbol": "tSpaceX",
          "kind": "buy",
          "routable": true,
          "assetKind": "pre_ipo",
          "tokenDecimals": 9,
          "premiumBps": -2700
        }
        """
        let dto = try JSONDecoder().decode(BuyQuoteDTO.self, from: Data(json.utf8))
        XCTAssertEqual(dto.kind, "buy")
        XCTAssertEqual(dto.resolvedAssetKind, .preIpo)
        XCTAssertEqual(dto.resolvedDecimals, 9)
        XCTAssertEqual(dto.premiumBps, -2700)
    }
}
