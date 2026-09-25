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

    func testAssetSymbolFormatter_stock_stripsAAPLx() {
        XCTAssertEqual(AssetSymbolFormatter.display("AAPLx"), "AAPL")
    }

    func testDisplay_stripsXStocksSuffix() {
        XCTAssertEqual(AssetSymbolFormatter.display("AAPLx"), "AAPL")
        XCTAssertEqual(AssetSymbolFormatter.display("NVDAx"), "NVDA")
        XCTAssertEqual(AssetSymbolFormatter.display("BRK.Bx"), "BRK.B")
        XCTAssertEqual(AssetSymbolFormatter.display("Vx"), "V")
        XCTAssertEqual(AssetSymbolFormatter.display(" SPYx "), "SPY")
    }

    func testDisplay_leavesOtherSymbolsAlone() {
        XCTAssertEqual(AssetSymbolFormatter.display("USDC"), "USDC")
        XCTAssertEqual(AssetSymbolFormatter.display("AAPL"), "AAPL")
        XCTAssertEqual(AssetSymbolFormatter.display("TOOLONGx"), "TOOLONGx")
        XCTAssertEqual(AssetSymbolFormatter.display("aaplx"), "aaplx")
        XCTAssertEqual(AssetSymbolFormatter.display("x"), "x")
        XCTAssertEqual(AssetSymbolFormatter.display(""), "")
    }

    func testDisplay_rawMint_returnsUnknownStock() {
        XCTAssertEqual(AssetSymbolFormatter.display("XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB"), "Unknown stock")
    }

    func testAssetDisplayNames_resolvesDemoTickers() {
        XCTAssertEqual(AssetDisplayNames.name(forSymbol: "AAPLx"), "Apple")
        XCTAssertEqual(AssetDisplayNames.name(forSymbol: "AAPL"), "Apple")
        XCTAssertEqual(AssetDisplayNames.name(forSymbol: "NVDAx"), "Nvidia")
        XCTAssertEqual(AssetDisplayNames.name(forSymbol: "TSLAx"), "Tesla")
        XCTAssertEqual(AssetDisplayNames.name(forSymbol: "MSFTx"), "Microsoft")
        XCTAssertEqual(AssetDisplayNames.name(forSymbol: "SPYx"), "S&P 500")
        XCTAssertEqual(AssetDisplayNames.name(forSymbol: "BRK.Bx"), "Berkshire Hathaway")
    }

    func testAssetDisplayNames_unknownReturnsNil() {
        XCTAssertNil(AssetDisplayNames.name(forSymbol: "ZZZZx"))
        XCTAssertNil(AssetDisplayNames.name(forSymbol: "USDC"))
        XCTAssertNil(AssetDisplayNames.name(forSymbol: ""))
        XCTAssertNil(AssetDisplayNames.name(forSymbol: "XsDoVfqeBukxuZHWhdvWHBhgEHjGNst4MLodqsJHzoB"))
    }
}
