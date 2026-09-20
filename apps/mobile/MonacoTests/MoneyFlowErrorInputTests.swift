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

    func testCoreClientErrors_mapOntoTheSameInput() {
        XCTAssertEqual(FlowErrorInput(MonacoCore.MonacoAPIError.httpStatus(503)), FlowErrorInput(status: 503))
        XCTAssertEqual(
            FlowErrorInput(MonacoCore.MonacoAPIError.rejected(status: 400, message: "invalid destination address")),
            FlowErrorInput(status: 400, serverMessage: "invalid destination address")
        )
        XCTAssertEqual(
            FlowErrorInput(MonacoCore.MonacoAPIError.rateLimited(retryAfterSeconds: 30)),
            FlowErrorInput(status: 429)
        )
    }

    func testFailureToCheckSignIn_saysNothingWasSent_insteadOfUnconfirmed() {
        // The request never ran: no token to send, or the refresh that would have
        // authorised it never came back.
        let noToken = FlowErrorInput(Monaco.MonacoAPIError.missingAccessToken)
        XCTAssertTrue(noToken.isSignInUnavailable)
        let refreshFailed = URLError(
            .userAuthenticationRequired,
            userInfo: [monacoTokenRefreshFailedErrorKey: true]
        )
        XCTAssertTrue(FlowErrorInput(refreshFailed).isSignInUnavailable)
        let failure = MoneyFlowCopy.cashOutFailure(FlowErrorInput(refreshFailed))
        XCTAssertNotEqual(failure, MoneyFlowCopy.unconfirmed)
        XCTAssertTrue(failure.isRetryable)
    }

    func testAmbiguousFailures_areUnconfirmed() {
        XCTAssertEqual(MoneyFlowCopy.cashOutFailure(FlowErrorInput(URLError(.timedOut))), MoneyFlowCopy.unconfirmed)
        XCTAssertEqual(MoneyFlowCopy.cashOutFailure(FlowErrorInput(URLError(.networkConnectionLost))), MoneyFlowCopy.unconfirmed)
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(Monaco.MonacoAPIError.invalidResponse)), MoneyFlowCopy.unconfirmed)
        let malformed = DecodingError.dataCorrupted(.init(codingPath: [], debugDescription: "bad json"))
        XCTAssertEqual(MoneyFlowCopy.sellStakeFailure(FlowErrorInput(malformed)), MoneyFlowCopy.unconfirmed)
    }
}
