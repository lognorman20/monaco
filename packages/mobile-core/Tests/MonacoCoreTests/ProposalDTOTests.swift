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
        }
    }
}
