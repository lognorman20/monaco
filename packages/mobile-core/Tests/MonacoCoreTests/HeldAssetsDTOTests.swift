import Foundation
import XCTest
@testable import MonacoCore

final class MarketAssetRowDecodingTests: XCTestCase {
    private func decode(_ json: String) throws -> MarketAssetDTO {
        try JSONDecoder().decode(MarketAssetDTO.self, from: Data(json.utf8))
    }

    func testSparkAndLogoDecode() throws {
        let asset = try decode("""
        {
          "symbol": "AAPLc", "name": "Apple", "tokenAddress": "0xb201", "routable": true,
          "priceUsdcMicros": 232050000, "change24h": "0.0124",
          "spark": [226500000, 228000000, 232050000],
          "logoUrl": "https://metadata.coinbase.com/equity_icons/aapl.png"
        }
        """)
        XCTAssertEqual(asset.sparkUsdcMicros, [226_500_000, 228_000_000, 232_050_000])
        XCTAssertEqual(asset.logoURL, URL(string: "https://metadata.coinbase.com/equity_icons/aapl.png"))
    }

    func testABackendWithoutTheNewFieldsStillDecodes() throws {
        // The fields are additive; an older backend must not break the list.
        let asset = try decode("""
        {"symbol": "AAPLc", "name": "Apple", "tokenAddress": "0xb201", "routable": true}
        """)
        XCTAssertTrue(asset.sparkUsdcMicros.isEmpty)
        XCTAssertNil(asset.logoUrl)
        XCTAssertNil(asset.logoURL)
    }

    func testNullSparkIsAnEmptySeriesNotAFailure() throws {
        let asset = try decode("""
        {"symbol": "AAPLc", "name": "Apple", "tokenAddress": "0xb201", "routable": true, "spark": null, "logoUrl": null}
        """)
        XCTAssertTrue(asset.sparkUsdcMicros.isEmpty)
        XCTAssertNil(asset.logoURL)
    }

    func testAnUnusableLogoStringIsNotAURL() throws {
        let asset = try decode("""
        {"symbol": "AAPLc", "name": "Apple", "tokenAddress": "0xb201", "routable": true, "logoUrl": ""}
        """)
        XCTAssertNil(asset.logoURL)
    }

    func testRoundTripsThroughEncoding() throws {
        let original = MarketSampleData.popularAssets[0]
        let data = try JSONEncoder().encode(original)
        XCTAssertEqual(try JSONDecoder().decode(MarketAssetDTO.self, from: data), original)
    }
}

final class HeldAssetsDTOTests: XCTestCase {
    func testResponseDecodes() throws {
        let json = """
        {
          "held": [{
            "asset": {"symbol": "AAPLc", "name": "Apple", "tokenAddress": "0xb201", "routable": true, "spark": [1, 2]},
            "cabals": [{"groupId": "g1", "name": "Weekend investors", "units": "4.2", "valueUsd": "974.61", "dollarPnl": "112.40", "mySliceUsd": "243.65"}],
            "totalValueUsd": "974.61", "totalDollarPnl": "112.40", "mySliceUsd": "243.65"
          }],
          "upForVote": [{
            "asset": {"symbol": "NVDAc", "name": "NVIDIA", "tokenAddress": "0xb202", "routable": true},
            "openProposals": 2,
            "cabalNames": ["Semis or bust", "Rent"],
            "soonestExpiresAt": "2026-09-22T18:00:00Z"
          }],
          "market": {"session": "open", "isOpen": true, "afterHours": false, "asOf": "2026-09-22T14:00:00Z"}
        }
        """
        let decoded = try JSONDecoder().decode(HeldAssetsResponseDTO.self, from: Data(json.utf8))
        XCTAssertEqual(decoded.held.count, 1)
        XCTAssertEqual(decoded.held[0].id, "AAPLc")
        XCTAssertEqual(decoded.held[0].cabals.first?.name, "Weekend investors")
        XCTAssertEqual(decoded.upForVote[0].openProposals, 2)
        XCTAssertEqual(decoded.upForVote[0].soonestExpiresAt?.timeIntervalSince1970, 1_790_100_000)
        XCTAssertEqual(decoded.market?.session, .open)
    }

    func testAnEmptyPayloadIsTwoEmptySections() throws {
        let decoded = try JSONDecoder().decode(HeldAssetsResponseDTO.self, from: Data("{}".utf8))
        XCTAssertTrue(decoded.held.isEmpty)
        XCTAssertTrue(decoded.upForVote.isEmpty)
        XCTAssertNil(decoded.market)
    }

    func testAMalformedSessionChipDoesNotTakeTheSectionsDown() throws {
        // This route feeds two of the Stocks tab's four sections. The chip is
        // decoration, and the market list and the detail both already drop an
        // unparseable one rather than fail the payload.
        let json = """
        {
          "held": [{
            "asset": {"symbol": "AAPLc", "name": "Apple", "tokenAddress": "0xb201", "routable": true},
            "totalValueUsd": "974.61", "totalDollarPnl": "112.40", "mySliceUsd": "243.65"
          }],
          "upForVote": [{
            "asset": {"symbol": "NVDAc", "name": "NVIDIA", "tokenAddress": "0xb202", "routable": true},
            "openProposals": 2
          }],
          "market": {"session": "open", "isOpen": true, "asOf": "never"}
        }
        """
        let decoded = try JSONDecoder().decode(HeldAssetsResponseDTO.self, from: Data(json.utf8))
        XCTAssertEqual(decoded.held.count, 1)
        XCTAssertEqual(decoded.upForVote.count, 1)
        XCTAssertNil(decoded.market)
    }

    func testMissingMoneyFieldsFallBackToZeroRatherThanFailing() throws {
        let json = """
        {"held": [{"asset": {"symbol": "AAPLc", "name": "Apple", "tokenAddress": "0xb2", "routable": true}}]}
        """
        let decoded = try JSONDecoder().decode(HeldAssetsResponseDTO.self, from: Data(json.utf8))
        XCTAssertEqual(decoded.held[0].mySliceUsd, "0")
        XCTAssertTrue(decoded.held[0].cabals.isEmpty)
    }
}

final class CabalStockCopyTests: XCTestCase {
    private func cabal(_ name: String, slice: String = "10.00") -> HeldAssetCabalDTO {
        HeldAssetCabalDTO(groupId: name, name: name, units: "1", valueUsd: "10", dollarPnl: "1", mySliceUsd: slice)
    }

    func testOneCabalIsNamed() {
        XCTAssertEqual(
            CabalStockCopy.heldLine(cabals: [cabal("Weekend investors")], mySliceUsd: "243.65"),
            "Weekend investors · your slice $243.65"
        )
    }

    func testSeveralCabalsAreCounted() {
        XCTAssertEqual(
            CabalStockCopy.heldLine(cabals: [cabal("A"), cabal("B")], mySliceUsd: "294.70"),
            "2 cabals · your slice $294.70"
        )
    }

    func testASliceThatRoundsToNothingIsDropped() {
        XCTAssertEqual(
            CabalStockCopy.heldLine(cabals: [cabal("A")], mySliceUsd: "0.001"),
            "A"
        )
    }

    func testAnUnreadableSliceIsDropped() {
        XCTAssertEqual(CabalStockCopy.heldLine(cabals: [cabal("A")], mySliceUsd: "??"), "A")
    }

    func testVoteLineNamesTheOnlyCabal() {
        XCTAssertEqual(CabalStockCopy.voteLine(openProposals: 1, cabalNames: ["Rent"]), "1 open vote · Rent")
    }

    func testVoteLineCountsWithoutNamingWhenThereAreSeveral() {
        XCTAssertEqual(
            CabalStockCopy.voteLine(openProposals: 3, cabalNames: ["A", "B", "C"]),
            "3 open votes"
        )
    }

    func testVoteDeadlineSpeech() {
        let now = Date(timeIntervalSince1970: 1_790_085_600)
        XCTAssertNil(CabalStockCopy.voteDeadline(nil, now: now))
        XCTAssertEqual(CabalStockCopy.voteDeadline(now.addingTimeInterval(-60), now: now), "closing now")
        XCTAssertEqual(CabalStockCopy.voteDeadline(now.addingTimeInterval(90), now: now), "closes in 1 minute")
        XCTAssertEqual(CabalStockCopy.voteDeadline(now.addingTimeInterval(35 * 60), now: now), "closes in 35 minutes")
        XCTAssertEqual(CabalStockCopy.voteDeadline(now.addingTimeInterval(4 * 3600), now: now), "closes in 4 hours")
        XCTAssertEqual(CabalStockCopy.voteDeadline(now.addingTimeInterval(50 * 3600), now: now), "closes in 2 days")
    }
}
