import XCTest
@testable import MonacoCore

final class PlatformBalanceDTOTests: XCTestCase {
    func testPlatformBalanceDTO_decodesFromAPI() throws {
        let json = """
        {
          "availableUsdcMicros": 2500000,
          "memberWalletAddress": "MemberAddr1111111111111111111111111111",
          "pendingAllocationMicros": 500000
        }
        """
        let dto = try JSONDecoder().decode(PlatformBalanceDTO.self, from: Data(json.utf8))
        XCTAssertEqual(dto.availableUsdcMicros, 2_500_000)
        XCTAssertEqual(dto.memberWalletAddress, "MemberAddr1111111111111111111111111111")
        XCTAssertEqual(dto.pendingAllocationMicros, 500_000)
    }

    func testFundGroupResponseDTO_decodesFromAPI() throws {
        let json = """
        {
          "depositId": "dep-001",
          "groupId": "grp-001",
          "amount": 1000000,
          "status": "pending",
          "fromAddress": "MemberAddr1111111111111111111111111111"
        }
        """
        let dto = try JSONDecoder().decode(FundGroupResponseDTO.self, from: Data(json.utf8))
        XCTAssertEqual(dto.depositId, "dep-001")
        XCTAssertEqual(dto.groupId, "grp-001")
        XCTAssertEqual(dto.amount, 1_000_000)
        XCTAssertEqual(dto.status, "pending")
    }
}
