import XCTest
@testable import MonacoCore

final class MoneyFlowCopyTests: XCTestCase {
    func testCashOut_internalServerStringsAreTranslated() {
        let overBalance = MoneyFlowCopy.cashOutFailure(
            FlowErrorInput(status: 400, serverMessage: "amount exceeds available platform balance")
        )
        XCTAssertEqual(overBalance.message, "That's more than your account balance.")
        XCTAssertFalse(overBalance.isRetryable)

        let badAddress = MoneyFlowCopy.cashOutFailure(
            FlowErrorInput(status: 400, serverMessage: " Invalid destination address\n")
        )
        XCTAssertEqual(badAddress.message, "That destination isn't a Solana wallet address.")

        let ownAddress = MoneyFlowCopy.cashOutFailure(
            FlowErrorInput(status: 400, serverMessage: "cannot withdraw to your deposit address")
        )
        XCTAssertEqual(ownAddress.message, "That's your own Monaco deposit address.")
    }

    func testCashOut_conflictIsNotRetryable() {
        let failure = MoneyFlowCopy.cashOutFailure(
            FlowErrorInput(status: 409, serverMessage: "a platform withdrawal is already in progress")
        )
        XCTAssertEqual(failure.message, "A cash out is already on its way.")
        XCTAssertFalse(failure.isRetryable)
    }

    func testOffline_saysNothingWasSentAndAllowsRetry() {
        for failure in [
            MoneyFlowCopy.cashOutFailure(.offline()),
            MoneyFlowCopy.fundCabalFailure(.offline()),
            MoneyFlowCopy.sellStakeFailure(.offline()),
        ] {
            XCTAssertTrue(failure.isRetryable)
            XCTAssertEqual(failure.nextStep, "Check your internet and try again — nothing was sent.")
        }
    }

    func testNoStatus_isUnconfirmedAndSendsTheSameSubmissionAgain() {
        let failure = MoneyFlowCopy.cashOutFailure(FlowErrorInput())
        XCTAssertEqual(failure, MoneyFlowCopy.unconfirmed)
        // The idempotency key makes the same payload a replay, so the screen must keep a
        // way forward instead of stranding the member on a disabled button.
        XCTAssertEqual(failure.recovery, .resendSame)
        XCTAssertTrue(failure.isRetryable)
        XCTAssertTrue(failure.mustResendSameSubmission)
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(FlowErrorInput()), MoneyFlowCopy.unconfirmed)
        XCTAssertEqual(MoneyFlowCopy.sellStakeFailure(FlowErrorInput()), MoneyFlowCopy.unconfirmed)
    }

    func testServerError_isRetryableWithoutClaimingNothingMoved() {
        let failure = MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 502, serverMessage: "bad gateway"))
        XCTAssertEqual(failure.message, "We couldn't add that money.")
        XCTAssertTrue(failure.isRetryable)
        // A 5xx is never stored by the backend, so it is as unknown as a timeout.
        XCTAssertEqual(failure.recovery, .resendSame)
    }

    func testRecovery_separatesAFreshTryFromAReplay() {
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(.offline()).recovery, .retry)
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 429)).recovery, .retry)
        XCTAssertEqual(MoneyFlowCopy.sellStakeFailure(FlowErrorInput(status: 409)).recovery, .retry)
        XCTAssertEqual(MoneyFlowCopy.cashOutFailure(FlowErrorInput(status: 401)).recovery, .none)
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 404)).recovery, .none)
    }

    func testFundCabal_statusSpecificCopy() {
        XCTAssertEqual(
            MoneyFlowCopy.fundCabalFailure(
                FlowErrorInput(status: 400, serverMessage: "amount exceeds available platform balance")
            ).message,
            "That's more than your account balance."
        )
        XCTAssertEqual(
            MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 403)).message,
            "You have to be a member of this cabal to add money to it."
        )
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 404)).message, "That cabal no longer exists.")
    }

    func testSellStake_memberFacingServerCopyPassesThrough() {
        let failure = MoneyFlowCopy.sellStakeFailure(
            FlowErrorInput(status: 400, serverMessage: "The pot could not raise enough USDC for this cash out.")
        )
        XCTAssertEqual(failure.message, "The pot could not raise enough USDC for this cash out.")
        XCTAssertFalse(failure.isRetryable)
    }

    func testSellStake_bodiless400IsTheDustFloor_and409IsRetryable() {
        XCTAssertEqual(MoneyFlowCopy.sellStakeFailure(FlowErrorInput(status: 400)).message, "Cash out at least $0.10.")
        XCTAssertEqual(
            MoneyFlowCopy.sellStakeFailure(FlowErrorInput(status: 400, serverMessage: "invalid json")).message,
            "Cash out at least $0.10."
        )
        XCTAssertTrue(MoneyFlowCopy.sellStakeFailure(FlowErrorInput(status: 409)).isRetryable)
    }

    func testGeneric_sessionExpiredAndRateLimited() {
        let expired = MoneyFlowCopy.cashOutFailure(FlowErrorInput(status: 401))
        XCTAssertEqual(expired.summary, "Your session expired. Sign in again to cash out.")
        XCTAssertFalse(expired.isRetryable)
        XCTAssertTrue(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 429)).isRetryable)
    }

    func testMemberFacingMessage_rejectsInternalFragments() {
        XCTAssertNil(MoneyFlowCopy.memberFacingMessage("not a group member"))
        XCTAssertNil(MoneyFlowCopy.memberFacingMessage("Internal error"))
        XCTAssertNil(MoneyFlowCopy.memberFacingMessage(""))
        XCTAssertNil(MoneyFlowCopy.memberFacingMessage(nil))
        XCTAssertEqual(MoneyFlowCopy.memberFacingMessage("Cash out at least $0.10."), "Cash out at least $0.10.")
    }

    func testNeverSentCodes_excludeTimeoutsAndDroppedConnections() {
        XCTAssertTrue(FlowErrorInput.neverSentURLErrorCodes.contains(.notConnectedToInternet))
        XCTAssertFalse(FlowErrorInput.neverSentURLErrorCodes.contains(.timedOut))
        XCTAssertFalse(FlowErrorInput.neverSentURLErrorCodes.contains(.networkConnectionLost))
    }
}
