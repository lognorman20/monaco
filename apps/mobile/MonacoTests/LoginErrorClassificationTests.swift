import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// How Dynamic failures are read. The rule under test: only a real "no session" answer
/// may sign the user out — a request that failed on the way never does.
struct LoginErrorClassificationTests {
    @Test func offlineIsNeverTreatedAsSignedOut() {
        #expect(!DynamicAuthService.isSignedOutError(URLError(.notConnectedToInternet)))
        #expect(!DynamicAuthService.isSignedOutError(URLError(.timedOut)))
        #expect(!DynamicAuthService.isSignedOutError(DynamicAuthHTTPError(status: -1009, detail: nil)))
    }

    @Test func urlErrorsMapToOffline() {
        #expect(DynamicAuthService.loginFailure(from: URLError(.notConnectedToInternet), step: .sendCode) == .offline)
        #expect(DynamicAuthService.loginFailure(from: URLError(.networkConnectionLost), step: .verifyCode) == .offline)
        let bridged = NSError(domain: NSURLErrorDomain, code: NSURLErrorTimedOut)
        #expect(DynamicAuthService.loginFailure(from: bridged, step: .verifyCode) == .offline)
    }

    @Test func transportFailureWithoutHTTPStatusMapsToOffline() {
        let error = DynamicAuthHTTPError(status: -1009, detail: "offline")
        #expect(DynamicAuthService.loginFailure(from: error, step: .verifyCode) == .offline)
    }

    @Test func rateLimitIsRecognisedOnBothSteps() {
        let error = DynamicAuthHTTPError(status: 429, detail: "Too many requests")
        #expect(DynamicAuthService.loginFailure(from: error, step: .sendCode) == .rateLimited)
        #expect(DynamicAuthService.loginFailure(from: error, step: .verifyCode) == .rateLimited)
    }

    @Test func rejectedCodeKeepsTheCodeField() {
        let error = DynamicAuthHTTPError(status: 422, detail: "Invalid code")
        let failure = DynamicAuthService.loginFailure(from: error, step: .verifyCode)
        #expect(failure == .codeRejected)
        #expect(failure.keepsCodeEntry)
    }

    @Test func malformedProviderResponseFallsBackToGenericCopy() {
        struct Boom: Error {}
        let failure = DynamicAuthService.loginFailure(from: Boom(), step: .verifyCode)
        #expect(failure == .other(detail: nil))
        #expect(LoginFailureCopy.message(for: failure, step: .verifyCode) == "Couldn't sign you in. Try again.")
    }

    @Test func unknownErrorsFallBackToGenericCopy() {
        struct Boom: Error {}
        #expect(DynamicAuthService.loginFailure(from: Boom(), step: .sendCode) == .other(detail: nil))
    }
}
