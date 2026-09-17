import XCTest
@testable import MonacoCore

final class AssetSymbolFormatterTests: XCTestCase {
    func testFormat_knownTicker_unchanged() {
        XCTAssertEqual(AssetSymbolFormatter.format("TSLAx"), "TSLAx")
        XCTAssertEqual(AssetSymbolFormatter.format("USDC"), "USDC")
    }

    func testFormat_rawMint_returnsUnknownStock() {
        let mint = "XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB"
        XCTAssertEqual(AssetSymbolFormatter.format(mint), "Unknown stock")
    }
}
