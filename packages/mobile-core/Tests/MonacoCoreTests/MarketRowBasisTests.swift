import XCTest

@testable import MonacoCore

/// A market row carries two figures that each name their instrument: the day-change pill
/// and the sparkline. On Base both are the underlying equity's, from one Pyth series, and
/// the line takes the pill's tone. `SparkTint` is the rule that keeps that honest if the two
/// ever name different instruments: a B20 token and its share are different units, so a
/// line tinted by a move measured on the other would be about something else.
final class MarketRowBasisTests: XCTestCase {

    private func asset(
        spark: [Int64],
        change24h: String?,
        sparkBasis: MarketPriceBasis?,
        changeBasis: MarketPriceBasis?
    ) -> MarketAssetDTO {
        MarketAssetDTO(
            symbol: "AAPLc",
            name: "Apple",
            tokenAddress: "0xb200000000000000000000c2e324d24d7eecd1fb",
            routable: true,
            priceUsdcMicros: 232_050_000,
            change24h: change24h,
            change24hBasis: changeBasis,
            change24hBasisSymbol: changeBasis == .token ? "AAPLc" : "AAPL",
            sparkUsdcMicros: spark,
            sparkBasis: sparkBasis,
            sparkBasisSymbol: sparkBasis == .token ? "AAPLc" : "AAPL"
        )
    }

    private let risingSeries: [Int64] = [226_500_000, 228_000_000, 230_100_000, 231_400_000]
    private let fallingSeries: [Int64] = [231_400_000, 230_100_000, 228_000_000, 226_500_000]

    /// The production shape on Base: one instrument, two measurements. The move is against
    /// the previous close and the line is the session, so they may point different ways, and
    /// the reported move is the right one to tint by.
    func testTheBaseShapeTintsTheLineByTheDayMove() {
        let row = MarketRowData(
            asset: asset(spark: risingSeries, change24h: "-0.0083", sparkBasis: .underlying, changeBasis: .underlying)
        )
        XCTAssertEqual(row.sparkTint, .reportedDayChange)
        XCTAssertNil(row.sparkBasisNote, "nothing to explain when both halves are the same instrument")
    }

    /// A token line beside the share's move: the move's sign says nothing about this line.
    func testADisagreeingBasisTintsTheLineFromTheLine() {
        let row = MarketRowData(
            asset: asset(spark: risingSeries, change24h: "-0.0083", sparkBasis: .token, changeBasis: .underlying)
        )
        XCTAssertEqual(row.sparkTint, .series(rising: true, flat: false))
        XCTAssertEqual(row.sparkBasisNote, "AAPLc")
    }

    func testADisagreeingBasisAlsoTintsAFallingLineFromTheLine() {
        let row = MarketRowData(
            asset: asset(spark: fallingSeries, change24h: "0.0124", sparkBasis: .token, changeBasis: .underlying)
        )
        XCTAssertEqual(row.sparkTint, .series(rising: false, flat: false))
    }

    /// An older backend says nothing about basis, which reads as "not stated", not "differ".
    func testAnUnstatedBasisKeepsTheReportedChange() {
        let row = MarketRowData(
            asset: asset(spark: risingSeries, change24h: "-0.0083", sparkBasis: nil, changeBasis: nil)
        )
        XCTAssertEqual(row.sparkTint, .reportedDayChange)
        XCTAssertNil(row.sparkBasisNote)
    }

    func testNoSeriesFallsBackToTheReportedChange() {
        let row = MarketRowData(
            asset: asset(spark: [], change24h: "0.0124", sparkBasis: .token, changeBasis: .underlying)
        )
        XCTAssertNil(row.spark)
        XCTAssertEqual(row.sparkTint, .reportedDayChange)
    }

    func testAFlatWindowIsFlat() {
        let flat: [Int64] = [231_400_000, 232_000_000, 230_900_000, 231_400_000]
        let row = MarketRowData(
            asset: asset(spark: flat, change24h: "0.0124", sparkBasis: .token, changeBasis: .underlying)
        )
        XCTAssertEqual(row.sparkTint, .series(rising: true, flat: true))
    }

    /// The pill shows only a move the backend labelled as the underlying's, and its dollar
    /// face is measured on the underlying's last close, never on the token's price.
    func testThePillIsTheSharesMovePricedPerShare() {
        let row = MarketRowData(
            asset: asset(spark: [100_000_000, 110_000_000], change24h: "0.1", sparkBasis: .underlying, changeBasis: .underlying)
        )
        XCTAssertEqual(row.dayMove?.caption, "AAPL day move")
        XCTAssertEqual(row.dayMoveReferencePriceUsdcMicros, 110_000_000)

        let tokenLine = MarketRowData(
            asset: asset(spark: [100_000_000, 110_000_000], change24h: "0.1", sparkBasis: .token, changeBasis: .underlying)
        )
        XCTAssertNil(tokenLine.dayMoveReferencePriceUsdcMicros, "a token line is not the share's price")

        let unlabelled = MarketRowData(
            asset: asset(spark: [100_000_000, 110_000_000], change24h: "0.1", sparkBasis: .underlying, changeBasis: nil)
        )
        XCTAssertNil(unlabelled.dayMove, "an unlabelled move is not shown beside the token's price")
    }

    func testBasisDecodesFromTheWireAlongsideTheSeries() throws {
        let json = """
        {
          "symbol": "AAPLc",
          "name": "Apple",
          "tokenAddress": "0xb200000000000000000000c2e324d24d7eecd1fb",
          "routable": true,
          "priceUsdcMicros": 232050000,
          "change24h": "-0.0083",
          "change24hBasis": "underlying",
          "change24hBasisSymbol": "AAPL",
          "spark": [226500000, 231400000],
          "sparkBasis": "underlying",
          "sparkBasisSymbol": "AAPL",
          "logoUrl": "https://metadata.coinbase.com/equity_icons/aapl.png"
        }
        """.data(using: .utf8)!

        let asset = try JSONDecoder().decode(MarketAssetDTO.self, from: json)
        XCTAssertEqual(asset.sparkBasis, .underlying)
        XCTAssertEqual(asset.sparkBasisSymbol, "AAPL")
        XCTAssertEqual(asset.change24hBasis, .underlying)
        XCTAssertFalse(asset.sparkAndChangeDisagreeOnInstrument)
        XCTAssertEqual(asset.logoURL?.absoluteString, "https://metadata.coinbase.com/equity_icons/aapl.png")
    }

    /// `spark` and `logoUrl` default because a backend that cannot source them still serves
    /// a usable row. `routable` must not: defaulting it to false silently disables Buy.
    func testRoutableIsRequiredWhileTheDecorationIsNot() throws {
        let withoutDecoration = """
        {"symbol":"AAPLc","name":"Apple","tokenAddress":"0xb2","routable":true}
        """.data(using: .utf8)!
        let asset = try JSONDecoder().decode(MarketAssetDTO.self, from: withoutDecoration)
        XCTAssertTrue(asset.routable)
        XCTAssertEqual(asset.sparkUsdcMicros, [])
        XCTAssertNil(asset.logoUrl)
        XCTAssertNil(asset.sparkBasis)

        let withoutRoutable = """
        {"symbol":"AAPLc","name":"Apple","tokenAddress":"0xb2"}
        """.data(using: .utf8)!
        XCTAssertThrowsError(
            try JSONDecoder().decode(MarketAssetDTO.self, from: withoutRoutable),
            "a missing routable must fail loudly rather than disable Buy everywhere"
        )
    }

    /// The app loads whatever logo URL it is handed, so only https is accepted.
    func testOnlyAnHttpsLogoIsLoaded() {
        XCTAssertNotNil(StockLogoURL.parse("https://metadata.coinbase.com/equity_icons/a.png"))
        XCTAssertNil(StockLogoURL.parse("http://metadata.coinbase.com/equity_icons/a.png"))
        XCTAssertNil(StockLogoURL.parse("javascript:alert(1)"))
        XCTAssertNil(StockLogoURL.parse("file:///etc/passwd"))
        XCTAssertNil(StockLogoURL.parse(""))
        XCTAssertNil(StockLogoURL.parse(nil))
    }
}
