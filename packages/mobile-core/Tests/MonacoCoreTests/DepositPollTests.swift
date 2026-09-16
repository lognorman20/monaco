import XCTest
@testable import MonacoCore

final class DepositPollTests: XCTestCase {
    func testDepositPollStateMachine_pendingToConfirmed_fromFixtureDepositDTO() throws {
        // Arrange
        let pendingJSON = """
        {
          "depositId": "dep-001",
          "status": "pending",
          "shareUnits": 0
        }
        """
        let confirmedJSON = """
        {
          "depositId": "dep-001",
          "status": "confirmed",
          "shareUnits": 1000000
        }
        """
        let pending = try JSONDecoder().decode(
            DepositStatusDTO.self,
            from: Data(pendingJSON.utf8)
        )
        let confirmed = try JSONDecoder().decode(
            DepositStatusDTO.self,
            from: Data(confirmedJSON.utf8)
        )
        var machine = DepositPollStateMachine()

        // Act
        machine.apply(status: pending.status)
        let afterPending = machine.phase
        let pendingTerminal = machine.isTerminal
        machine.apply(status: confirmed.status)
        let afterConfirmed = machine.phase
        let confirmedTerminal = machine.isTerminal

        // Assert
        XCTAssertEqual(afterPending, .awaitingSweep)
        XCTAssertFalse(pendingTerminal)
        XCTAssertEqual(afterConfirmed, .credited)
        XCTAssertTrue(confirmedTerminal)
        XCTAssertEqual(confirmed.shareUnits, 1_000_000)
    }
}
