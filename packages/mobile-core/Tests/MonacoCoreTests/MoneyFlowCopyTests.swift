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

    func testServerError_isAFreshTry_becauseTheBackendReleasedTheKey() {
        let failure = MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 502, serverMessage: "bad gateway"))
        XCTAssertEqual(failure.message, "We couldn't add that money.")
        XCTAssertTrue(failure.isRetryable)
        // `Idempotency.run` releases the key on 5xx so the retry reaches the handler again.
        // Calling this `.resendSame` would promise a replay the server has thrown away.
        XCTAssertEqual(failure.recovery, .retry)
        XCTAssertFalse(failure.mustResendSameSubmission)
        XCTAssertEqual(MoneyFlowCopy.cashOutFailure(FlowErrorInput(status: 500)).recovery, .retry)
    }

    /// A 409 on a money route means an attempt is still holding the key the app just sent,
    /// and that attempt may be landing the money. It is never a fresh submission.
    func testConflict_isAReplayOnEveryMoneyFlow_neverAFreshSubmission() {
        for failure in [
            MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 409)),
            MoneyFlowCopy.sellStakeFailure(FlowErrorInput(status: 409)),
        ] {
            XCTAssertEqual(failure.recovery, .resendSame)
            XCTAssertTrue(failure.mustResendSameSubmission)
        }
        // Fund used to fall through to generic(), which worded an in-flight deposit as a
        // flat failure and offered a fresh send while the key was still pending.
        let fund = MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 409))
        XCTAssertEqual(fund.message, "Your last deposit is still finishing.")
        XCTAssertNotEqual(fund.message, "We couldn't add that money.")
        XCTAssertEqual(
            MoneyFlowCopy.sellStakeFailure(FlowErrorInput(status: 409)).message,
            "Your last cash out is still finishing."
        )
    }

    /// The copy must not promise that a retry can only ever go through once: the backend
    /// abandons an in-progress claim after 5 minutes, and a payload derived from live
    /// equity can change its own fingerprint. It sends the member to their balance instead.
    func testUnconfirmed_doesNotPromiseSingleExecution_andKeepsTheBalanceCheck() {
        let nextStep = try? XCTUnwrap(MoneyFlowCopy.unconfirmed.nextStep)
        let step = nextStep ?? ""
        XCTAssertTrue(step.lowercased().contains("balance"), "lost the balance check: \(step)")
        XCTAssertFalse(step.lowercased().contains("only go through once"), "promises single execution: \(step)")
        XCTAssertFalse(step.lowercased().contains("can only"), "promises single execution: \(step)")
    }

    func testSignInUnavailable_saysNothingWasSent_andStaysRetryable() {
        let failure = MoneyFlowCopy.cashOutFailure(FlowErrorInput(isSignInUnavailable: true))
        XCTAssertEqual(failure.message, "We couldn't check your sign-in, so we didn't cash out.")
        XCTAssertEqual(failure.nextStep, "Try again in a moment — nothing was sent.")
        XCTAssertEqual(failure.recovery, .retry)
        XCTAssertNotEqual(failure, MoneyFlowCopy.unconfirmed)
        XCTAssertEqual(
            MoneyFlowCopy.fundCabalFailure(FlowErrorInput(isSignInUnavailable: true)).message,
            "We couldn't check your sign-in, so we didn't add that money."
        )
    }

    func testRecovery_separatesAFreshTryFromAReplay() {
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(.offline()).recovery, .retry)
        XCTAssertEqual(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 429)).recovery, .retry)
        XCTAssertEqual(MoneyFlowCopy.sellStakeFailure(FlowErrorInput(status: 409)).recovery, .resendSame)
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

    /// The sequence the new copy will most often produce: a money POST times out, the
    /// member sends the same amount again under the pending key, and the backend answers
    /// 409 `in_progress`. Both steps have to keep a way forward and keep the same key.
    func testTimeoutThenInProgressConflict_staysAReplayThroughout() {
        let timedOut = MoneyFlowCopy.fundCabalFailure(FlowErrorInput())
        XCTAssertEqual(timedOut.recovery, .resendSame)

        let submission = IdempotentSubmission { "key-1" }
        var request = URLRequest(url: URL(string: "https://api.test/v1/groups/g1/fund")!)
        request.httpMethod = "POST"
        request.httpBody = Data(#"{"amount":1}"#.utf8)
        let first = submission.key(for: request)
        XCTAssertTrue(submission.hasPendingKey, "a timeout leaves the key pending")

        // The 409 must not clear the key, or the next attempt mints a second submission.
        let conflict = HTTPURLResponse(
            url: request.url!,
            statusCode: 409,
            httpVersion: nil,
            headerFields: [IdempotentSubmission.statusHeader: IdempotentSubmission.inProgressStatus]
        )!
        submission.record(response: conflict, forKey: first)
        XCTAssertTrue(submission.hasPendingKey)
        XCTAssertEqual(submission.key(for: request), first, "the retry must ride the same key")

        let stillRunning = MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 409))
        XCTAssertEqual(stillRunning.recovery, .resendSame)
        XCTAssertTrue(stillRunning.isRetryable)
    }

    func testGeneric_sessionExpiredAndRateLimited() {
        let expired = MoneyFlowCopy.cashOutFailure(FlowErrorInput(status: 401))
        XCTAssertEqual(expired.summary, "Your session expired. Sign in again to cash out.")
        XCTAssertFalse(expired.isRetryable)
        XCTAssertTrue(MoneyFlowCopy.fundCabalFailure(FlowErrorInput(status: 429)).isRetryable)
    }

    /// Same PR, same `Retry-After` header: chat read it and the money flows threw it away,
    /// so a throttled member was told "a moment" in one place and "60 seconds" in the other.
    func testRateLimited_readsTheServersRetryAfter_likeChatAlreadyDoes() {
        let counted = MoneyFlowCopy.fundCabalFailure(
            FlowErrorInput(status: 429, retryAfterSeconds: 60)
        )
        XCTAssertEqual(counted.nextStep, "Try again in 60 seconds.")
        XCTAssertEqual(counted.recovery, .retry)

        XCTAssertEqual(
            MoneyFlowCopy.cashOutFailure(FlowErrorInput(status: 429, retryAfterSeconds: 1)).nextStep,
            "Try again in 1 second."
        )
        // No header, or a useless one, keeps the old wording rather than inventing a number.
        for input in [
            FlowErrorInput(status: 429),
            FlowErrorInput(status: 429, retryAfterSeconds: 0),
        ] {
            XCTAssertEqual(
                MoneyFlowCopy.sellStakeFailure(input).nextStep, "Wait a moment and try again."
            )
        }
    }

    func testMemberFacingMessage_rejectsInternalFragments() {
        XCTAssertNil(MoneyFlowCopy.memberFacingMessage("not a group member"))
        XCTAssertNil(MoneyFlowCopy.memberFacingMessage("Internal error"))
        XCTAssertNil(MoneyFlowCopy.memberFacingMessage(""))
        XCTAssertNil(MoneyFlowCopy.memberFacingMessage(nil))
        XCTAssertEqual(MoneyFlowCopy.memberFacingMessage("Cash out at least $0.10."), "Cash out at least $0.10.")
    }

    /// `.retry` is the one answer that lets a screen mint a NEW idempotency key, so it is the
    /// one that can move the money twice. It is now spelled out at every branch rather than
    /// arriving by default from an `isRetryable: true`, and this pins the whole matrix so a
    /// new branch cannot quietly widen it. Every case listed here is either provably never
    /// sent (offline, sign-in, 429 rejected by the rate limiter) or a 5xx, where the backend
    /// releases the key and the retry genuinely reaches the handler again.
    func testRetry_isOnlyEverOfferedWhereAFreshKeyCannotDoubleSubmit() {
        let freshSubmissionIsSafe: [FlowErrorInput] = [
            FlowErrorInput(isOffline: true),
            FlowErrorInput(isSignInUnavailable: true),
            FlowErrorInput(status: 429),
            FlowErrorInput(status: 500),
            FlowErrorInput(status: 503),
        ]
        for input in freshSubmissionIsSafe {
            for failure in [
                MoneyFlowCopy.cashOutFailure(input),
                MoneyFlowCopy.fundCabalFailure(input),
                MoneyFlowCopy.sellStakeFailure(input),
            ] {
                XCTAssertEqual(failure.recovery, .retry, "expected a fresh retry for \(input)")
            }
        }

        // Everything with an unknown or in-flight outcome must replay the pending key, and
        // everything that would fail the same way must offer nothing at all.
        XCTAssertEqual(MoneyFlowCopy.unconfirmed.recovery, .resendSame)
        for flow in [MoneyFlowCopy.fundCabalFailure, MoneyFlowCopy.sellStakeFailure] {
            XCTAssertEqual(flow(FlowErrorInput(status: 409)).recovery, .resendSame)
            XCTAssertEqual(flow(FlowErrorInput()).recovery, .resendSame)
        }
        // The external cash out fails closed on 409: two backend conditions land there and
        // the app cannot yet tell them apart, so it offers no resend rather than guessing.
        XCTAssertEqual(MoneyFlowCopy.cashOutFailure(FlowErrorInput(status: 409)).recovery, .none)
        XCTAssertEqual(MoneyFlowCopy.cashOutFailure(FlowErrorInput(status: 401)).recovery, .none)
    }

    func testNeverSentCodes_excludeTimeoutsAndDroppedConnections() {
        XCTAssertTrue(FlowErrorInput.neverSentURLErrorCodes.contains(.notConnectedToInternet))
        XCTAssertFalse(FlowErrorInput.neverSentURLErrorCodes.contains(.timedOut))
        XCTAssertFalse(FlowErrorInput.neverSentURLErrorCodes.contains(.networkConnectionLost))
    }
}
