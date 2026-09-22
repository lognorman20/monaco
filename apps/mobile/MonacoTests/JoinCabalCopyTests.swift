import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// What a member is told when a join does not go through (#296). The copy is
/// pure, so it is pinned here without a network.
@MainActor
struct JoinCabalCopyTests {
    @Test func aReadOnlyCabalDoesNotAskTheMemberToRetry() {
        let message = JoinCabalCopy.failureMessage(for: Monaco.MonacoAPIError.httpStatus(403), enteredCode: true)

        #expect(message.contains("demo"))
        #expect(!message.contains("Try again"))
    }

    @Test func aBadCodeIsAboutTheCodeOnlyWhenOneWasTyped() {
        let typed = JoinCabalCopy.failureMessage(for: Monaco.MonacoAPIError.httpStatus(404), enteredCode: true)
        let fromRow = JoinCabalCopy.failureMessage(for: Monaco.MonacoAPIError.httpStatus(404), enteredCode: false)

        #expect(typed.contains("invite code"))
        #expect(!fromRow.contains("invite code"))
    }

    @Test func aMissingTokenAsksForSignInRatherThanARetry() {
        // The screen no longer pre-checks the token; a missing one arrives as an
        // error like any other, and must not read as a server failure.
        let message = JoinCabalCopy.failureMessage(for: Monaco.MonacoAPIError.missingAccessToken, enteredCode: false)

        #expect(message.contains("Sign in"))
    }

    @Test func anUnknownFailureStaysRetryable() {
        let message = JoinCabalCopy.failureMessage(for: Monaco.MonacoAPIError.httpStatus(500), enteredCode: true)

        #expect(message == "Couldn't join this cabal. Try again.")
    }

    @Test func noMessageLeaksAnHttpCode() {
        // Product voice: no raw status codes in anything a member reads.
        let messages = [403, 404, 401, 500, 502].map {
            JoinCabalCopy.failureMessage(for: Monaco.MonacoAPIError.httpStatus($0), enteredCode: true)
        }

        for message in messages {
            let carriesADigit = message.contains(where: \.isNumber)
            #expect(!carriesADigit, "copy should not carry a status code: \(message)")
        }
    }
}
