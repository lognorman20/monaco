import XCTest
@testable import MonacoCore

final class DisplayFormatterTests: XCTestCase {
    func testPercentReturnFormatter_positiveReturn_showsPlusPrefix() {
        // Arrange
        let raw = "0.124"

        // Act
        let formatted = PercentReturnFormatter.format(raw)

        // Assert
        XCTAssertTrue(formatted.hasPrefix("+"))
        XCTAssertTrue(formatted.contains("%"))
    }

    func testPercentReturnFormatter_nilPercentReturn_showsEmDashOrHidden() {
        // Arrange
        let raw: String? = nil

        // Act
        let formatted = PercentReturnFormatter.format(raw)

        // Assert
        XCTAssertEqual(formatted, "—")
    }

    func testDollarPnlFormatter_negativeShowsLossCopy() {
        // Arrange
        let raw = "-$12.40"

        // Act
        let formatted = DollarPnlFormatter.format(raw)

        // Assert
        XCTAssertTrue(formatted.localizedCaseInsensitiveContains("loss"))
    }

    func testSlicePercentFormatter_formatsOneDecimal() {
        // Arrange
        let raw = "0.425"

        // Act
        let formatted = SlicePercentFormatter.format(raw)

        // Assert
        XCTAssertEqual(formatted, "42.5%")
    }
}
