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

    func testPercentReturnFormatter_negativeRatio_formatsAsPercent() {
        // Backend sends signed ratios for losses, e.g. formatPercentReturnDecimal(-0.036).
        XCTAssertEqual(PercentReturnFormatter.format("-0.036"), "\u{2212}3.6%")
        XCTAssertEqual(PercentReturnFormatter.format("0"), "0.0%")
        XCTAssertEqual(PercentReturnFormatter.format("+0.124"), "+12.4%")
        XCTAssertEqual(PercentReturnFormatter.format("+12.4%"), "+12.4%")
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

    func testUsdAmountFormatter_formatsDecimalStringAsCurrency() {
        XCTAssertEqual(UsdAmountFormatter.format(decimalString: "2100.05"), "$2,100.05")
    }

    func testUsdAmountFormatter_formatsMicrosAsCurrency() {
        XCTAssertEqual(UsdAmountFormatter.format(micros: 2_100_050_000), "$2,100.05")
    }

    func testStakeWithdrawConverter_shareMicros_scalesWithUsdTarget() {
        let shares = StakeWithdrawConverter.shareMicros(
            forUsdMicros: 1_050_025_000,
            totalEquityUsdMicros: 2_100_050_000,
            maxShareMicros: 2_100_050
        )
        XCTAssertEqual(shares, 1_050_025)
    }

    func testStakeWithdrawConverter_fullWithdraw_returnsMaxShares() {
        XCTAssertTrue(
            StakeWithdrawConverter.isFullWithdraw(
                selectedUsdMicros: 2_100_050_000,
                totalEquityUsdMicros: 2_100_050_000
            )
        )
        XCTAssertEqual(
            StakeWithdrawConverter.shareMicros(
                forUsdMicros: 2_100_050_000,
                totalEquityUsdMicros: 2_100_050_000,
                maxShareMicros: 2_100_050
            ),
            2_100_050
        )
    }

    func testStakeWithdrawConverter_usdMicrosForFraction_atMax_returnsFullEquity() {
        XCTAssertEqual(
            StakeWithdrawConverter.usdMicros(forFraction: 1, maxUsdMicros: 2_100_050_000),
            2_100_050_000
        )
    }
}
