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

    // MARK: - Demo polish formatters

    func testPercentReturnFormatter_usesTypographicMinusAndUnsignedZero() {
        XCTAssertEqual(PercentReturnFormatter.format("-0.036"), "\u{2212}3.6%")
        XCTAssertEqual(PercentReturnFormatter.format("0.096"), "+9.6%")
        XCTAssertEqual(PercentReturnFormatter.format("-0.0001"), "0.0%")
        XCTAssertEqual(PercentReturnFormatter.format("-0"), "0.0%")
        XCTAssertEqual(PercentReturnFormatter.format("-3.6%"), "\u{2212}3.6%")
        XCTAssertEqual(PercentReturnFormatter.format("\u{2212}0.036"), "\u{2212}3.6%")
        XCTAssertEqual(PercentReturnFormatter.format(""), "—")
        XCTAssertEqual(PercentReturnFormatter.format(nil), "—")
        XCTAssertEqual(PercentReturnFormatter.format("—"), "—")
    }

    func testUsdAmountFormatter_negativeAmountReadsLikeEveryOtherNegativeFigure() {
        // Never "$-12.50": the sign goes first, and it is U+2212, as `compact` already does.
        XCTAssertEqual(UsdAmountFormatter.format(decimal: Decimal(string: "-12.50")!), "\u{2212}$12.50")
        XCTAssertEqual(UsdAmountFormatter.format(micros: -12_431_800_000), "\u{2212}$12,431.80")
        XCTAssertEqual(UsdAmountFormatter.format(decimalString: "-1234.5"), "\u{2212}$1,234.50")
        // Dust that rounds to nothing keeps the unsigned zero.
        XCTAssertEqual(UsdAmountFormatter.format(decimal: Decimal(string: "-0.001")!), "$0.00")
        XCTAssertEqual(UsdAmountFormatter.format(decimal: Decimal(string: "12.50")!), "$12.50")
    }

    func testPercentFormatters_rejectValuesThatAreNotNumbers() {
        // A NaN or infinite ratio from the API must read as no figure, not "+nan%".
        // Both of them: returning the raw string just moved the garbage, so a slice read
        // "nan" where the return next to it read "—".
        for raw in ["nan", "-nan", "inf", "-infinity"] {
            XCTAssertEqual(PercentReturnFormatter.format(raw), "—", raw)
            XCTAssertEqual(SlicePercentFormatter.format(raw), "—", raw)
        }
    }

    func testDollarPnlFormatter_readsATypographicMinusAsALoss() {
        XCTAssertEqual(DollarPnlFormatter.format("\u{2212}$3.10"), "\u{2212}$3.10 loss")
    }

    func testUsdAmountFormatter_compact() {
        XCTAssertEqual(UsdAmountFormatter.compact(decimalString: "12431.8"), "$12,431.80")
        XCTAssertEqual(UsdAmountFormatter.compact(decimalString: "99999.99"), "$99,999.99")
        XCTAssertEqual(UsdAmountFormatter.compact(decimalString: "100000"), "$100K")
        XCTAssertEqual(UsdAmountFormatter.compact(decimalString: "124500"), "$124.5K")
        XCTAssertEqual(UsdAmountFormatter.compact(decimalString: "12431180"), "$12.4M")
        XCTAssertEqual(UsdAmountFormatter.compact(decimalString: "2000000000"), "$2B")
        XCTAssertEqual(UsdAmountFormatter.compact(decimalString: "0"), "$0.00")
    }

    func testProposalShareFormatter_sharesLabel() {
        XCTAssertEqual(ProposalShareFormatter.sharesLabel(fromAtomics: "120340000"), "1.2034 shares")
        XCTAssertEqual(ProposalShareFormatter.sharesLabel(fromAtomics: "120345678"), "1.2035 shares")
        XCTAssertEqual(ProposalShareFormatter.sharesLabel(fromAtomics: "50000000"), "0.5 shares")
        XCTAssertEqual(ProposalShareFormatter.sharesLabel(fromAtomics: "100000000"), "1 share")
        XCTAssertEqual(ProposalShareFormatter.sharesLabel(fromAtomics: "300000000"), "3 shares")
        XCTAssertEqual(ProposalShareFormatter.sharesLabel(fromAtomics: "0"), "0 shares")
        XCTAssertEqual(ProposalShareFormatter.sharesLabel(fromAtomics: "1000"), "< 0.0001 shares")
        XCTAssertEqual(ProposalShareFormatter.sharesLabel(fromAtomics: "garbage"), "garbage")
    }

    func testRelativeTimeFormatter_label() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let now = ISO8601DateFormatter().date(from: "2026-09-19T12:00:00Z")!
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "2026-09-19T11:59:30Z", now: now, calendar: calendar), "now")
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "2026-09-19T11:45:00Z", now: now, calendar: calendar), "15m")
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "2026-09-19T08:59:00.500Z", now: now, calendar: calendar), "3h")
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "2026-09-14T09:00:00Z", now: now, calendar: calendar), "Sep 14")
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "2025-12-31T09:00:00Z", now: now, calendar: calendar), "Dec 31, 2025")
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "2026-09-19T12:05:00Z", now: now, calendar: calendar), "now")
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "not a date", now: now, calendar: calendar), "")
    }

    func testRelativeTimeFormatter_datesUseLocalCalendarDay() {
        var tokyo = Calendar(identifier: .gregorian)
        tokyo.timeZone = TimeZone(identifier: "Asia/Tokyo")!
        let now = ISO8601DateFormatter().date(from: "2026-09-19T12:00:00Z")!
        // 20:00 UTC on Sep 14 is already Sep 15 in Tokyo.
        XCTAssertEqual(RelativeTimeFormatter.label(iso: "2026-09-14T20:00:00Z", now: now, calendar: tokyo), "Sep 15")
    }
}
