import XCTest
@testable import MonacoCore

final class PlatformWithdrawalDTOTests: XCTestCase {
    func testPlatformWithdrawalResponseDTO_decodesFromAPI() throws {
        let json = """
        {
          "withdrawalId": "wd-001",
          "amount": 3000000,
          "toAddress": "11111111111111111111111111111112",
          "status": "confirmed",
          "txHash": "sig-abc",
          "createdAt": "2026-03-18T12:00:00Z"
        }
        """
        let dto = try JSONDecoder().decode(PlatformWithdrawalResponseDTO.self, from: Data(json.utf8))
        XCTAssertEqual(dto.withdrawalId, "wd-001")
        XCTAssertEqual(dto.amount, 3_000_000)
        XCTAssertEqual(dto.toAddress, "11111111111111111111111111111112")
        XCTAssertEqual(dto.status, "confirmed")
        XCTAssertEqual(dto.txHash, "sig-abc")
    }
}
