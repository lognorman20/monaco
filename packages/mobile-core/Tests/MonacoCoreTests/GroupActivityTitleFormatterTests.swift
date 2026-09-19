import XCTest
@testable import MonacoCore

final class GroupActivityTitleFormatterTests: XCTestCase {
    func testMemberBuyKeepsExistingCopy() {
        XCTAssertEqual(
            GroupActivityTitleFormatter.format(kind: "buy", symbol: "AAPLx", agentDisplayName: nil, initiatedBy: nil),
            "Buy AAPLx"
        )
    }

    func testAgentBuyUsesAgentNameAndLowercaseVerb() {
        XCTAssertEqual(
            GroupActivityTitleFormatter.format(kind: "buy", symbol: "AAPLx", agentDisplayName: "Mr Cheese", initiatedBy: "agent"),
            "Mr Cheese buy AAPLx"
        )
    }

    func testAgentSellUsesAgentNameAndLowercaseVerb() {
        XCTAssertEqual(
            GroupActivityTitleFormatter.format(kind: "sell", symbol: "TSLAx", agentDisplayName: "Mr Cheese", initiatedBy: "agent"),
            "Mr Cheese sell TSLAx"
        )
    }

    func testAddAgentUsesAddedVerb() {
        XCTAssertEqual(
            GroupActivityTitleFormatter.format(kind: "add_agent", symbol: "Mr Cheese", agentDisplayName: "Mr Cheese", initiatedBy: nil),
            "Added Mr Cheese"
        )
    }

    func testPauseAgentUsesPausedVerb() {
        XCTAssertEqual(
            GroupActivityTitleFormatter.format(kind: "pause_agent", symbol: "", agentDisplayName: "Mr Cheese", initiatedBy: nil),
            "Paused Mr Cheese"
        )
    }
}
