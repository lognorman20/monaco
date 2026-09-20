import Foundation
import Testing
@testable import Monaco

struct SessionErrorMappingTests {
    private let baseURL = URL(string: "http://127.0.0.1:8080")!

    @Test func urlErrorMapsToCantReachMonaco() {
        let described = SessionErrorMapping.describe(URLError(.notConnectedToInternet), apiBaseURL: baseURL)
        #expect(described.message == "Can't reach Monaco. Check your connection and try again.")
        #expect(described.debugDetail.contains("notConnectedToInternet"))
        #expect(described.debugDetail.contains(baseURL.absoluteString))
    }

    @Test func cannotConnectToHostAlsoMapsToCantReachMonaco() {
        let described = SessionErrorMapping.describe(URLError(.cannotConnectToHost), apiBaseURL: baseURL)
        #expect(described.message == "Can't reach Monaco. Check your connection and try again.")
        #expect(described.debugDetail.contains("cannotConnectToHost"))
    }

    @Test func serverErrorMapsToTryAgainInAMoment() {
        let described = SessionErrorMapping.describe(MonacoAPIError.httpStatus(500), apiBaseURL: baseURL)
        #expect(described.message == "Monaco's server hit a problem. Try again in a moment.")
        #expect(described.debugDetail.contains("HTTP 500"))
        #expect(described.debugDetail.contains(baseURL.absoluteString))
    }

    @Test func anyFiveHundredsStatusMapsToServerError() {
        for status in [500, 502, 503, 599] {
            let described = SessionErrorMapping.describe(MonacoAPIError.httpStatus(status), apiBaseURL: baseURL)
            #expect(described.message == "Monaco's server hit a problem. Try again in a moment.")
        }
    }

    @Test func unauthorizedMapsToSignInVerificationMessage() {
        let described = SessionErrorMapping.describe(MonacoAPIError.httpStatus(401), apiBaseURL: baseURL)
        #expect(described.message == SessionErrorMapping.signInVerificationFailureMessage)
        #expect(described.message == "We couldn't verify your sign-in. Try again.")
        #expect(described.debugDetail.contains("HTTP 401"))
    }

    @Test func otherHttpStatusesFallBackToGenericPolishedCopy() {
        let described = SessionErrorMapping.describe(MonacoAPIError.httpStatus(418), apiBaseURL: baseURL)
        #expect(described.message == "Couldn't open Monaco. Try again.")
        // Never leak the raw status into user-facing copy.
        #expect(!described.message.contains("418"))
        #expect(described.debugDetail.contains("HTTP 418"))
    }

    @Test func unknownErrorFallsBackToGenericPolishedCopyWithoutRawDescriptionInMessage() {
        struct SomeOtherError: Error {}
        let described = SessionErrorMapping.describe(SomeOtherError(), apiBaseURL: baseURL)
        #expect(described.message == "Couldn't open Monaco. Try again.")
        #expect(described.debugDetail.contains("SomeOtherError"))
    }

    @Test func debugDetailAlwaysCarriesTheApiBaseURL() {
        let otherBase = URL(string: "https://staging.monaco.example")!
        let described = SessionErrorMapping.describe(URLError(.timedOut), apiBaseURL: otherBase)
        #expect(described.debugDetail.contains("https://staging.monaco.example"))
    }
}
