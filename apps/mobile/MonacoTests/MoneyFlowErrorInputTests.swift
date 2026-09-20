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
        // The server's Retry-After rides along instead of being dropped here: chat showed a
        // countdown off this same response while the money flows said "a moment".
        XCTAssertEqual(
            FlowErrorInput(MonacoCore.MonacoAPIError.rateLimited(retryAfterSeconds: 30)),
            FlowErrorInput(status: 429, retryAfterSeconds: 30)
        )
        XCTAssertEqual(
            MoneyFlowCopy.cashOutFailure(
                FlowErrorInput(MonacoCore.MonacoAPIError.rateLimited(retryAfterSeconds: 30))
            ).nextStep,
            "Try again in 30 seconds."
        )
        // A 429 with no header keeps the generic wording.
        XCTAssertEqual(
            FlowErrorInput(MonacoCore.MonacoAPIError.rateLimited(retryAfterSeconds: nil)),
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

    /// A failed refresh is rewritten into a URLError that keeps the underlying code, and the
    /// commonest one is `.notConnectedToInternet` — which is also in `neverSentURLErrorCodes`.
    /// Matched in the wrong order the offline branch swallows it and the sign-in branch is
    /// dead for exactly the case it was added for.
    func testRefreshFailure_takesTheSignInBranch_evenWhenItLooksOffline() {
        let refreshFailedOffline = URLError(
            .notConnectedToInternet,
            userInfo: [monacoTokenRefreshFailedErrorKey: true]
        )
        let input = FlowErrorInput(refreshFailedOffline)
        XCTAssertTrue(input.isSignInUnavailable)
        XCTAssertFalse(input.isOffline)
        XCTAssertEqual(
            MoneyFlowCopy.cashOutFailure(input).message,
            "We couldn't check your sign-in, so we didn't cash out."
        )
        // A plain offline error still takes the offline branch.
        XCTAssertEqual(FlowErrorInput(URLError(.notConnectedToInternet)), .offline())
    }

    /// Neither is given a fabricated status: an unreadable reply is genuinely unconfirmed,
    /// and a blocked leave never reaches money copy (GroupDetailView words it itself).
    /// A status here would word either one as an in-flight money request.
    func testCoreInvalidResponseAndLeaveBlocked_stayStatusless() {
        for error in [
            MonacoCore.MonacoAPIError.invalidResponse,
            MonacoCore.MonacoAPIError.leaveBlocked(.pendingRedeem),
        ] {
            let input = FlowErrorInput(error)
            XCTAssertNil(input.status, "\(error)")
            XCTAssertFalse(input.isOffline, "\(error)")
            XCTAssertFalse(input.isSignInUnavailable, "\(error)")
            XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(input), MoneyFlowCopy.unconfirmed, "\(error)")
        }
    }

    func testAmbiguousFailures_areUnconfirmed() {
        XCTAssertEqual(MoneyFlowCopy.cashOutFailure(FlowErrorInput(URLError(.timedOut))), MoneyFlowCopy.unconfirmed)
        XCTAssertEqual(MoneyFlowCopy.cashOutFailure(FlowErrorInput(URLError(.networkConnectionLost))), MoneyFlowCopy.unconfirmed)
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(Monaco.MonacoAPIError.invalidResponse)), MoneyFlowCopy.unconfirmed)
        let malformed = DecodingError.dataCorrupted(.init(codingPath: [], debugDescription: "bad json"))
        XCTAssertEqual(MoneyFlowCopy.sellStakeFailure(FlowErrorInput(malformed)), MoneyFlowCopy.unconfirmed)
    }
}
