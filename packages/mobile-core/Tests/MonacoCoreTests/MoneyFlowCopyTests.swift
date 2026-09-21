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
        XCTAssertEqual(badAddress.message, "That destination isn't a Base address.")
        // A Base address is 0x plus 40 hex characters; the old Solana length rule is gone.
        XCTAssertEqual(badAddress.nextStep, "Paste the address again — it's 0x followed by 40 letters and numbers.")
        XCTAssertFalse(badAddress.summary.contains("32 to 44"))

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

    func testNoStatus_isUnconfirmedAndNeverInvitesABlindRetry() {
        let failure = MoneyFlowCopy.cashOutFailure(FlowErrorInput())
        XCTAssertEqual(failure, MoneyFlowCopy.unconfirmed)
        XCTAssertFalse(failure.isRetryable)
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(FlowErrorInput()), MoneyFlowCopy.unconfirmed)
        XCTAssertEqual(MoneyFlowCopy.sellStakeFailure(FlowErrorInput()), MoneyFlowCopy.unconfirmed)
    }

    /// The copy must not promise that a retry can only go through once: there is no
    /// idempotency key, so a resend after an unknown outcome is a second submission. It
    /// sends the member to their balance instead and offers no retry.
    func testUnconfirmed_doesNotPromiseSingleExecution_andKeepsTheBalanceCheck() {
        let step = MoneyFlowCopy.unconfirmed.nextStep ?? ""
        XCTAssertTrue(step.lowercased().contains("balance"), "lost the balance check: \(step)")
        XCTAssertFalse(step.lowercased().contains("only go through once"), "promises single execution: \(step)")
        XCTAssertFalse(step.lowercased().contains("can only"), "promises single execution: \(step)")
        XCTAssertFalse(step.lowercased().contains("send the same amount"), "invites a resend: \(step)")
        XCTAssertFalse(MoneyFlowCopy.unconfirmed.isRetryable)
    }

    func testSignInUnavailable_saysNothingWasSent_andStaysRetryable() {
        let failure = MoneyFlowCopy.cashOutFailure(FlowErrorInput(isSignInUnavailable: true))
        XCTAssertEqual(failure.message, "We couldn't check your sign-in, so we didn't cash out.")
        XCTAssertEqual(failure.nextStep, "Try again in a moment — nothing was sent.")
        XCTAssertTrue(failure.isRetryable)
        XCTAssertNotEqual(failure, MoneyFlowCopy.unconfirmed)
        XCTAssertEqual(
            MoneyFlowCopy.fundCabalFailure(FlowErrorInput(isSignInUnavailable: true)).message,
            "We couldn't check your sign-in, so we didn't add that money."
        )
        XCTAssertEqual(
            MoneyFlowCopy.sellStakeFailure(FlowErrorInput(isSignInUnavailable: true)).message,
            "We couldn't check your sign-in, so we didn't cash out."
        )
    }

    func testServerError_isRetryableWithoutClaimingNothingMoved() {
        let failure = MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 502, serverMessage: "bad gateway"))
        XCTAssertEqual(failure.message, "We couldn't add that money.")
        XCTAssertTrue(failure.isRetryable)
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

    /// The 409 means an earlier cash out of this cabal is still running — often the
    /// member's own attempt that timed out. "Try again" would read as "the first one
    /// failed", so the next step points at the balance instead.
    func testSellStake_conflictSendsTheMemberToTheirBalance_notToARetry() {
        let failure = MoneyFlowCopy.sellStakeFailure(
            FlowErrorInput(status: 409, serverMessage: "withdraw already in progress")
        )
        XCTAssertEqual(failure.message, "Your last cash out is still finishing.")
        XCTAssertEqual(failure.nextStep, "Give it a minute, then check your balance.")
    }

    func testGeneric_sessionExpiredAndRateLimited() {
        let expired = MoneyFlowCopy.cashOutFailure(FlowErrorInput(status: 401))
        XCTAssertEqual(expired.summary, "Your session expired. Sign in again to cash out.")
        XCTAssertFalse(expired.isRetryable)
        XCTAssertTrue(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 429)).isRetryable)
    }

    /// Same `Retry-After` header: chat read it and the money flows threw it away, so a
    /// throttled member was told "a moment" in one place and "60 seconds" in the other.
    func testRateLimited_readsTheServersRetryAfter_likeChatAlreadyDoes() {
        let counted = MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 429, retryAfterSeconds: 60))
        XCTAssertEqual(counted.message, "Too many tries in a row.")
        XCTAssertEqual(counted.nextStep, "Try again in 60 seconds.")
        XCTAssertTrue(counted.isRetryable)

        XCTAssertEqual(
            MoneyFlowCopy.cashOutFailure(FlowErrorInput(status: 429, retryAfterSeconds: 1)).nextStep,
            "Try again in 1 second."
        )
        // No header, or a useless one, keeps the old wording rather than inventing a number.
        for input in [
            FlowErrorInput(status: 429),
            FlowErrorInput(status: 429, retryAfterSeconds: 0),
        ] {
            XCTAssertEqual(MoneyFlowCopy.sellStakeFailure(input).nextStep, "Wait a moment and try again.")
        }
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
