import Foundation
import MonacoCore
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

    /// Writes that go through the `MonacoCore` client (cabal pictures) raise its own error
    /// type; a 401 there is the same rejection and must not slip past as a plain failure.
    @Test func aCoreClient401IsARejection() {
        #expect(RejectedSession.isRejection(MonacoCore.MonacoAPIError.httpStatus(401)))
        #expect(RejectedSession.isRejection(MonacoCore.MonacoAPIError.rejected(status: 401, message: "expired")))
        #expect(!RejectedSession.isRejection(MonacoCore.MonacoAPIError.httpStatus(403)))
        #expect(!RejectedSession.isRejection(MonacoCore.MonacoAPIError.rejected(status: 413, message: "too big")))
        #expect(!RejectedSession.isRejection(MonacoCore.MonacoAPIError.rateLimited(retryAfterSeconds: 5)))
    }

    /// The rejection names the token the request carried, and anything else passes through.
    @Test func sendingTiesARejectionToItsToken() async {
        do {
            _ = try await RejectedSession.sending(token: "t-sent") { _ -> Int in
                throw MonacoCore.MonacoAPIError.httpStatus(401)
            }
            Issue.record("expected RejectedSession")
        } catch {
            #expect(error as? RejectedSession == RejectedSession(token: "t-sent"))
        }
        do {
            _ = try await RejectedSession.sending(token: "t-sent") { _ -> Int in
                throw MonacoCore.MonacoAPIError.httpStatus(404)
            }
            Issue.record("expected the 404 to surface")
        } catch {
            #expect(error as? MonacoCore.MonacoAPIError == .httpStatus(404))
        }
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
