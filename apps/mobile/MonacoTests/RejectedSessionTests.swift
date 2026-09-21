import Foundation
import Testing
@testable import Monaco

/// What counts as the server rejecting the session, for the screens that report the token
/// their read carried to the guarded sign-out.
@MainActor
struct RejectedSessionTests {
    @Test func a401WithOrWithoutABodyIsARejection() {
        #expect(RejectedSession.isRejection(Monaco.MonacoAPIError.httpStatus(401)))
        #expect(RejectedSession.isRejection(Monaco.MonacoAPIError.apiError(status: 401, message: "expired")))
    }

    /// A refusal of one request is not a refusal of the session: 403 means this member may not
    /// do this, and signing them out for it would lose them the whole app.
    @Test func otherRefusalsAreNotRejections() {
        #expect(!RejectedSession.isRejection(Monaco.MonacoAPIError.httpStatus(403)))
        #expect(!RejectedSession.isRejection(Monaco.MonacoAPIError.apiError(status: 403, message: "not a member")))
        #expect(!RejectedSession.isRejection(Monaco.MonacoAPIError.rateLimited(retryAfterSeconds: 30)))
        #expect(!RejectedSession.isRejection(Monaco.MonacoAPIError.missingAccessToken))
        #expect(!RejectedSession.isRejection(URLError(.notConnectedToInternet)))
    }

    /// Signed out: there is no token to send, so the request is never made and nothing is
    /// reported as rejected.
    @Test func noTokenMeansNoRequest() async {
        let auth = DynamicAuthService(
            settings: DynamicAuthSettings(environmentID: "", smsLoginEnabled: true, emailLoginEnabled: true),
            sessionStore: MonacoSessionStore(defaults: UserDefaults(suiteName: "RejectedSessionTests.\(UUID().uuidString)")!)
        )
        var sent = false
        do {
            _ = try await auth.sendingAccessToken { _ in sent = true }
            Issue.record("expected missingAccessToken")
        } catch Monaco.MonacoAPIError.missingAccessToken {
        } catch {
            Issue.record("unexpected \(error)")
        }
        #expect(!sent)
    }
}
