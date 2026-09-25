import XCTest
@testable import MonacoCore

final class TokenQuantityFormatterTests: XCTestCase {
    /// A quantity that already carries its display multiplier rounds the same way atomics do.
    func testLabelFromQuantity_roundsToFourDecimals() {
        XCTAssertEqual(TokenQuantityFormatter.label(quantity: Decimal(string: "0.73001856091")!, kind: .stock), "0.73 shares")
        XCTAssertEqual(TokenQuantityFormatter.label(quantity: Decimal(string: "1234.56789")!, kind: .stock), "1,234.5679 shares")
        XCTAssertEqual(TokenQuantityFormatter.label(quantity: 1, kind: .preIpo), "1 token")
        XCTAssertEqual(TokenQuantityFormatter.label(quantity: Decimal(string: "0.00001")!, kind: .stock), "< 0.0001 shares")
    }
}
