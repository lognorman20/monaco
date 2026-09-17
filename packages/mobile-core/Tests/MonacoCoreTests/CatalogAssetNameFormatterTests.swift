import XCTest
@testable import MonacoCore

final class CatalogAssetNameFormatterTests: XCTestCase {
    func testFormat_stripsTrailingXStockBranding() {
        XCTAssertEqual(CatalogAssetNameFormatter.format("Apple xStock"), "Apple")
        XCTAssertEqual(CatalogAssetNameFormatter.format("Tesla xStocks"), "Tesla")
        XCTAssertEqual(CatalogAssetNameFormatter.format("  Microsoft xstock  "), "Microsoft")
    }

    func testFormat_leavesPlainNamesUntouched() {
        XCTAssertEqual(CatalogAssetNameFormatter.format("Apple"), "Apple")
        XCTAssertEqual(CatalogAssetNameFormatter.format("USDC"), "USDC")
    }
}
