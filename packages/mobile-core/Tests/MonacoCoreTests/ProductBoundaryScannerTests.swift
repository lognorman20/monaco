import XCTest
@testable import MonacoCore

final class ProductBoundaryScannerTests: XCTestCase {
    func testProductBoundaryScanner_blocksEvmAddressInMainFlow() {
        let address = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
        XCTAssertTrue(ProductBoundaryScanner.containsForbiddenEVMAddress("Send to \(address)"))
        XCTAssertFalse(ProductBoundaryScanner.mainFlowCopyIsClean("Send to \(address)"))
        XCTAssertTrue(ProductBoundaryScanner.containsForbiddenHost("https://basescan.org/tx/abc"))
        XCTAssertTrue(ProductBoundaryScanner.containsForbiddenHost("https://etherscan.io/tx/abc"))
        XCTAssertTrue(ProductBoundaryScanner.mainFlowCopyIsClean("Send USDC on Base to this address"))
    }
}
