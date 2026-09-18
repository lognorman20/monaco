import XCTest
@testable import MonacoCore

final class ProposalDTOTests: XCTestCase {
    func testThesisDecodesFromDetailAndIsOptionalForList() throws {
        let detail = Data(#"{"id":"p-1","symbol":"AAPLx","usdcMicros":"1","status":"open","thesis":"Growth"}"#.utf8)
        let list = Data(#"{"id":"p-1","symbol":"AAPLx","usdcMicros":"1","status":"open"}"#.utf8)
        XCTAssertEqual(try JSONDecoder().decode(ProposalDTO.self, from: detail).thesis, "Growth")
        XCTAssertNil(try JSONDecoder().decode(ProposalDTO.self, from: list).thesis)
    }

    func testProposalDTO_decodesOpenPassedFailedExpiredStatuses() throws {
        // Arrange
        let statuses = ["open", "passed", "failed", "expired"]

        // Act
        let decoded = try statuses.map { status -> ProposalDTO in
            let json = """
            {
              "id": "prop-\(status)",
              "symbol": "AAPLx",
              "usdcMicros": "2500000",
              "status": "\(status)"
            }
            """
            return try JSONDecoder().decode(ProposalDTO.self, from: Data(json.utf8))
        }

        // Assert
        XCTAssertEqual(decoded.map(\.status), statuses)
        for (dto, status) in zip(decoded, statuses) {
            XCTAssertEqual(ProposalStatusDisplay.from(status: dto.status)?.label, ProposalStatusDisplay.from(status: status)?.label)
            XCTAssertEqual(dto.resolvedKind, "buy")
        }
    }

    func testProposalDTO_decodesSellKindAndTokenAmount() throws {
        let json = """
        {
          "id": "prop-sell",
          "symbol": "AAPLx",
          "kind": "sell",
          "tokenAmount": "50000000",
          "status": "open"
        }
        """

        let dto = try JSONDecoder().decode(ProposalDTO.self, from: Data(json.utf8))

        XCTAssertEqual(dto.resolvedKind, "sell")
        XCTAssertEqual(dto.tokenAmount, "50000000")
        XCTAssertNil(dto.usdcMicros)
    }
}
