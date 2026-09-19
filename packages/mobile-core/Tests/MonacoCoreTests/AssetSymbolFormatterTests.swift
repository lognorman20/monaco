import XCTest
@testable import MonacoCore

final class AssetSymbolFormatterTests: XCTestCase {
    func testFormat_stripsTrailingXStockSuffix() {
        XCTAssertEqual(AssetSymbolFormatter.format("TSLAx"), "TSLA")
        XCTAssertEqual(AssetSymbolFormatter.format("Goog"), "GOOG")
        XCTAssertEqual(AssetSymbolFormatter.format("USDC"), "USDC")
    }

    func testFormat_rawMint_returnsUnknownStock() {
        let mint = "XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB"
        XCTAssertEqual(AssetSymbolFormatter.format(mint), "Unknown stock")
    }
}
