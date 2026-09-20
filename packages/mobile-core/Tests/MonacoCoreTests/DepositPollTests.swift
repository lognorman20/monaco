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

    func testDepositStatusNormalizer_failedPrefix() {
        XCTAssertTrue(DepositStatusNormalizer.isFailed("failed: submit_sweep"))
        XCTAssertFalse(DepositStatusNormalizer.isFailed("pending"))
    }

    func testDepositPollStateMachine_pollUntilTerminal_confirmsFromFetchSequence() async {
        var machine = DepositPollStateMachine()
        var fetches = ["pending", "pending", "confirmed"]

        let phase = await machine.pollUntilTerminal(
            maxWait: .seconds(1),
            interval: .milliseconds(10)
        ) {
            guard !fetches.isEmpty else { return "confirmed" }
            return fetches.removeFirst()
        }

        XCTAssertEqual(phase, .credited)
    }

    func testDepositPollStateMachine_pollUntilTerminal_retriesAfterFetchError() async {
        var machine = DepositPollStateMachine()
        var calls = 0

        let phase = await machine.pollUntilTerminal(
            maxWait: .seconds(1),
            interval: .milliseconds(10)
        ) {
            calls += 1
            if calls == 1 {
                throw MonacoAPIError.httpStatus(500)
            }
            return "confirmed"
        }

        XCTAssertEqual(phase, .credited)
        XCTAssertEqual(calls, 2)
    }
}
