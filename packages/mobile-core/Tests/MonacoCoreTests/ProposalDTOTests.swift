import XCTest
@testable import MonacoCore

final class ProposalDTOTests: XCTestCase {
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

    func testProposalDTO_decodesThesisWhenPresentAndNilWhenAbsent() throws {
        let withThesis = """
        {
          "id": "prop-thesis",
          "symbol": "AAPLx",
          "usdcMicros": "2500000",
          "status": "open",
          "thesis": "Strong earnings beat, raising guidance."
        }
        """
        let withoutThesis = """
        {
          "id": "prop-no-thesis",
          "symbol": "AAPLx",
          "usdcMicros": "2500000",
          "status": "open"
        }
        """

        let dtoWithThesis = try JSONDecoder().decode(ProposalDTO.self, from: Data(withThesis.utf8))
        let dtoWithoutThesis = try JSONDecoder().decode(ProposalDTO.self, from: Data(withoutThesis.utf8))

        XCTAssertEqual(dtoWithThesis.thesis, "Strong earnings beat, raising guidance.")
        XCTAssertNil(dtoWithoutThesis.thesis)
    }
}
