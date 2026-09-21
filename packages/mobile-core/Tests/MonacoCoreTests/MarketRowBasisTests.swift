import XCTest

@testable import MonacoCore

/// A market row mixes two instruments: the day-change pill is the xStock token's
/// move from Jupiter, the sparkline is the underlying equity's day from Pyth. They
/// genuinely diverge — that divergence is the premise of the stock-vs-token
/// comparison — so a row that draws one and tints it by the other is lying about
/// which instrument the reader is looking at.
final class MarketRowBasisTests: XCTestCase {

    private func asset(
        spark: [Int64],
        change24h: String?,
        sparkBasis: MarketPriceBasis?,
        changeBasis: MarketPriceBasis?
    ) -> MarketAssetDTO {
        MarketAssetDTO(
            symbol: "AAPLx",
            name: "Apple xStock",
            solanaMint: "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp",
            routable: true,
            priceUsdcMicros: 232_050_000,
            change24h: change24h,
            sparkUsdcMicros: spark,
            sparkBasis: sparkBasis,
            sparkBasisSymbol: sparkBasis == .underlying ? "AAPL" : "AAPLx",
            changeBasis: changeBasis,
            changeBasisSymbol: "AAPLx"
        )
    }

    private let risingSeries: [Int64] = [226_500_000, 228_000_000, 230_100_000, 231_400_000]
    private let fallingSeries: [Int64] = [231_400_000, 230_100_000, 228_000_000, 226_500_000]

    /// The regression: the token was down on the day while the equity's drawn
    /// window rose, and the row painted the rising line red.
    func testADisagreeingBasisTintsTheLineFromTheLine() {
        let row = MarketRowData(
            asset: asset(
                spark: risingSeries,
                change24h: "-0.0083",
                sparkBasis: .underlying,
                changeBasis: .token
            )
        )
        XCTAssertEqual(row.sparkTint, .series(rising: true, flat: false))
        XCTAssertEqual(row.sparkBasisNote, "AAPL")
    }

    func testADisagreeingBasisAlsoTintsAFallingLineFromTheLine() {
        let row = MarketRowData(
            asset: asset(
                spark: fallingSeries,
                change24h: "0.0124",
                sparkBasis: .underlying,
                changeBasis: .token
            )
        )
        XCTAssertEqual(row.sparkTint, .series(rising: false, flat: false))
    }

    /// One instrument, two measurements: the change is against the previous close
    /// and the line is the window. Those are allowed to disagree, and the reported
    /// change is the right one to tint by.
    func testAnAgreeingBasisKeepsTheReportedDayChange() {
        let row = MarketRowData(
            asset: asset(
                spark: risingSeries,
                change24h: "-0.0083",
                sparkBasis: .token,
                changeBasis: .token
            )
        )
        XCTAssertEqual(row.sparkTint, .reportedDayChange)
        XCTAssertNil(row.sparkBasisNote, "nothing to explain when both halves are the same instrument")
    }

    /// An older backend says nothing about basis, which must read as "not stated"
    /// rather than "they differ".
    func testAnUnstatedBasisKeepsTheOldBehaviour() {
        let row = MarketRowData(
            asset: asset(spark: risingSeries, change24h: "-0.0083", sparkBasis: nil, changeBasis: nil)
        )
        XCTAssertEqual(row.sparkTint, .reportedDayChange)
        XCTAssertNil(row.sparkBasisNote)
    }

    /// A row with no series has nothing to tint, whatever the bases say.
    func testNoSeriesFallsBackToTheReportedChange() {
        let row = MarketRowData(
            asset: asset(spark: [], change24h: "0.0124", sparkBasis: .underlying, changeBasis: .token)
        )
        XCTAssertNil(row.spark)
        XCTAssertEqual(row.sparkTint, .reportedDayChange)
    }

    /// A window that ended exactly where it started is flat, not a rise.
    func testAFlatWindowIsFlat() {
        let flat: [Int64] = [231_400_000, 232_000_000, 230_900_000, 231_400_000]
        let row = MarketRowData(
            asset: asset(spark: flat, change24h: "0.0124", sparkBasis: .underlying, changeBasis: .token)
        )
        XCTAssertEqual(row.sparkTint, .series(rising: true, flat: true))
    }

    func testBasisDecodesFromTheWireAlongsideTheSeries() throws {
        let json = """
        {
          "symbol": "AAPLx",
          "name": "Apple xStock",
          "solanaMint": "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp",
          "routable": true,
          "priceUsdcMicros": 232050000,
          "change24h": "-0.0083",
          "spark": [226500000, 231400000],
          "sparkBasis": "underlying",
          "sparkBasisSymbol": "AAPL",
          "changeBasis": "token",
          "changeBasisSymbol": "AAPLx",
          "logoUrl": "https://xstocks-metadata.backed.fi/logos/tokens/AAPLx.png"
        }
        """.data(using: .utf8)!

        let asset = try JSONDecoder().decode(MarketAssetDTO.self, from: json)
        XCTAssertEqual(asset.sparkBasis, .underlying)
        XCTAssertEqual(asset.sparkBasisSymbol, "AAPL")
        XCTAssertEqual(asset.changeBasis, .token)
        XCTAssertTrue(asset.sparkAndChangeDisagreeOnInstrument)
        XCTAssertEqual(
            asset.logoURL?.absoluteString,
            "https://xstocks-metadata.backed.fi/logos/tokens/AAPLx.png"
        )
    }

    /// `spark` and `logoUrl` default because a backend that cannot source them
    /// still serves a usable row. `routable` must not: defaulting it to false
    /// silently disables Buy on every screen in the app, which is a failure that
    /// looks like a product decision.
    func testRoutableIsRequiredWhileTheDecorationIsNot() throws {
        let withoutDecoration = """
        {"symbol":"AAPLx","name":"Apple","solanaMint":"m","routable":true}
        """.data(using: .utf8)!
        let asset = try JSONDecoder().decode(MarketAssetDTO.self, from: withoutDecoration)
        XCTAssertTrue(asset.routable)
        XCTAssertEqual(asset.sparkUsdcMicros, [])
        XCTAssertNil(asset.logoUrl)
        XCTAssertNil(asset.sparkBasis)

        let withoutRoutable = """
        {"symbol":"AAPLx","name":"Apple","solanaMint":"m"}
        """.data(using: .utf8)!
        XCTAssertThrowsError(
            try JSONDecoder().decode(MarketAssetDTO.self, from: withoutRoutable),
            "a missing routable must fail loudly rather than disable Buy everywhere"
        )
    }
}
