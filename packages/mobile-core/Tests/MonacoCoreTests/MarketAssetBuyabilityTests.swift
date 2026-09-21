import XCTest
@testable import MonacoCore

final class MarketAssetBuyabilityTests: XCTestCase {
    func testCanBuy_listedToken_evenWhenRoutableFalse() {
        let asset = MarketAssetDTO(
            symbol: "AAPLc",
            name: "Apple",
            tokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
            routable: false
        )
        XCTAssertTrue(asset.canBuy)
    }

    func testCanBuy_blankToken_falseWhenNotRoutable() {
        let asset = MarketAssetDTO(
            symbol: "BAD",
            name: "Bad",
            tokenAddress: "  ",
            routable: false
        )
        XCTAssertFalse(asset.canBuy)
    }
}
