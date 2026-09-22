import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// Kept apart from `SessionErrorMappingTests` because it needs MonacoCore's refresh flag, and
/// importing MonacoCore there would make that file's bare `MonacoAPIError` ambiguous.
struct SessionErrorMappingRefreshTests {
    private let baseURL = URL(string: "http://127.0.0.1:8080")!

    /// A 401 whose token refresh failed was refused, not unsent: even when the refresh failed
    /// for want of a connection, the member is told their sign-in could not be verified.
    @Test func aFailedTokenRefreshMapsToTheSignInMessageNotCantReach() {
        let refreshFailed = URLError(
            .notConnectedToInternet,
            userInfo: [monacoTokenRefreshFailedErrorKey: true]
        )
        let described = SessionErrorMapping.describe(refreshFailed, apiBaseURL: baseURL)
        #expect(described.message == SessionErrorMapping.signInVerificationFailureMessage)
        #expect(described.debugDetail.contains("Token refresh failed"))
        #expect(described.debugDetail.contains(baseURL.absoluteString))
    }

    /// The same URLError without the flag is an ordinary connection failure.
    @Test func anUnflaggedConnectionFailureStillReadsAsCantReach() {
        let described = SessionErrorMapping.describe(URLError(.notConnectedToInternet), apiBaseURL: baseURL)
        #expect(described.message == "Can't reach Monaco. Check your connection and try again.")
    }
}
