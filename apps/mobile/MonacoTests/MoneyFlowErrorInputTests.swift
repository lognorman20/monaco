import MonacoCore
import XCTest
@testable import Monaco

final class MoneyFlowErrorInputTests: XCTestCase {
    func testHTTPStatus_keepsTheStatus() {
        XCTAssertEqual(FlowErrorInput(Monaco.MonacoAPIError.httpStatus(503)), FlowErrorInput(status: 503))
    }

    func testAPIError_keepsTheServerMessage() {
        XCTAssertEqual(
            FlowErrorInput(Monaco.MonacoAPIError.apiError(status: 400, message: "invalid destination address")),
            FlowErrorInput(status: 400, serverMessage: "invalid destination address")
        )
    }

    func testNeverSentURLErrors_areOffline() {
        XCTAssertEqual(FlowErrorInput(URLError(.notConnectedToInternet)), .offline())
        XCTAssertEqual(FlowErrorInput(URLError(.cannotConnectToHost)), .offline())
    }

    func testAmbiguousFailures_areUnconfirmed() {
        XCTAssertEqual(MoneyFlowCopy.cashOutFailure(FlowErrorInput(URLError(.timedOut))), MoneyFlowCopy.unconfirmed)
        XCTAssertEqual(MoneyFlowCopy.cashOutFailure(FlowErrorInput(URLError(.networkConnectionLost))), MoneyFlowCopy.unconfirmed)
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(Monaco.MonacoAPIError.invalidResponse)), MoneyFlowCopy.unconfirmed)
        let malformed = DecodingError.dataCorrupted(.init(codingPath: [], debugDescription: "bad json"))
        XCTAssertEqual(MoneyFlowCopy.sellStakeFailure(FlowErrorInput(malformed)), MoneyFlowCopy.unconfirmed)
    }
}
