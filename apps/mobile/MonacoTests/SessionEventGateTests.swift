import Testing
@testable import Monaco

/// The decisions `DynamicAuthService` makes about SDK events and background revokes. Each one
/// decides whether something that arrives late, for a session that may have been replaced,
/// is allowed to end or change the session open now.
///
/// Epoch 1 is the sign-in that was open when a subscription or revoke was made; epoch 2 is
/// the member who signed in after it.
struct SessionEventGateTests {
    // MARK: A "nobody is signed in" event

    /// The baseline every case below changes one input of: a real expiry of the session
    /// that is open now.
    private func endsSession(
        eventHasUser: Bool = false,
        eventEpoch: Int = 2,
        currentEpoch: Int = 2,
        revokePending: Bool = false,
        isAuthenticated: Bool = true,
        sdkHoldsUser: Bool = false
    ) -> Bool {
        SessionEventGate.shouldEndSession(
            eventHasUser: eventHasUser,
            eventEpoch: eventEpoch,
            currentEpoch: currentEpoch,
            revokePending: revokePending,
            isAuthenticated: isAuthenticated,
            sdkHoldsUser: sdkHoldsUser
        )
    }

    /// Dynamic ended the open session (it expired, or was revoked elsewhere): the member is
    /// signed out and told why.
    @Test func aRealExpiryOnTheOpenSessionEndsIt() {
        #expect(endsSession())
    }

    /// The previous member's revoke finished after the next member signed in. Its "no user"
    /// was about the old sign-in and must not sign out the new one.
    @Test func aNoUserEventFromAnEarlierSignInIsIgnored() {
        #expect(!endsSession(eventEpoch: 1, currentEpoch: 2))
    }

    /// Our own revoke's logout is what the SDK is reporting. The session it ends is already
    /// gone locally, and whoever is signing in meanwhile must not be torn down by it.
    @Test func aNoUserEventWhileARevokeIsPendingIsIgnored() {
        #expect(!endsSession(revokePending: true))
    }

    /// The event is stale: by the time it is delivered the SDK holds a user again.
    @Test func aNoUserEventIsIgnoredWhileTheSDKStillHoldsAUser() {
        #expect(!endsSession(sdkHoldsUser: true))
    }

    /// Nothing to end: the member has already signed out, or never finished signing in.
    @Test func aNoUserEventWithNoOpenSessionEndsNothing() {
        #expect(!endsSession(isAuthenticated: false))
    }

    /// A user appearing is never an ending.
    @Test func anEventCarryingAUserEndsNothing() {
        #expect(!endsSession(eventHasUser: true))
    }

    // MARK: A rotated token

    private func adopts(
        eventEpoch: Int = 2,
        currentEpoch: Int = 2,
        isSigningOut: Bool = false,
        isAuthenticated: Bool = true,
        sdkToken: String? = "rotated",
        current: String? = "held"
    ) -> Bool {
        SessionEventGate.shouldAdoptToken(
            eventEpoch: eventEpoch,
            currentEpoch: currentEpoch,
            isSigningOut: isSigningOut,
            isAuthenticated: isAuthenticated,
            sdkToken: sdkToken,
            current: current
        )
    }

    @Test func aRotationOnTheOpenSessionIsAdopted() {
        #expect(adopts())
    }

    /// After a sign-out the SDK can republish the old token. Adopting it would put a token
    /// back on a signed-out app, and the next request would run under a dead session.
    @Test func aTokenRepublishedDuringSignOutIsNotAdopted() {
        #expect(!adopts(isSigningOut: true))
    }

    @Test func aTokenRepublishedAfterTheSessionEndedIsNotAdopted() {
        #expect(!adopts(isAuthenticated: false, current: nil))
    }

    /// A subscription made for an earlier sign-in must not hand its token to the member
    /// signed in now.
    @Test func aTokenFromAnEarlierSignInIsNotAdopted() {
        #expect(!adopts(eventEpoch: 1, currentEpoch: 2))
    }

    @Test func theTokenAlreadyHeldIsNotAdoptedAgain() {
        #expect(!adopts(sdkToken: "held", current: "held"))
    }

    @Test func anEmptyOrMissingSDKTokenIsNotAdopted() {
        #expect(!adopts(sdkToken: nil))
        #expect(!adopts(sdkToken: ""))
    }

    // MARK: A sign-out's background revoke

    /// Nobody has signed in since: Dynamic is told the session is over.
    @Test func theRevokeRunsWhenNoOneHasSignedInSince() {
        #expect(SessionEventGate.shouldRevoke(scheduledEpoch: 1, currentEpoch: 1))
    }

    /// A new sign-in went ahead after its bounded wait. Dynamic's logout is global, so the
    /// revoke running now would sign the new member straight out.
    @Test func theRevokeDoesNotRunOnceANewSignInHasBumpedTheEpoch() {
        #expect(!SessionEventGate.shouldRevoke(scheduledEpoch: 1, currentEpoch: 2))
    }

    /// The gate above wired into `PendingRevoke` the way `DynamicAuthService.performLogout`
    /// does it: the revoke is held on the network while a new sign-in bumps the epoch, and
    /// when the network answers the logout is not sent.
    @MainActor
    @Test func aRevokeHeldPastANewSignInNeverCallsLogout() async {
        let pending = PendingRevoke()
        let network = HeldCall()
        let service = RevokingService()

        let scheduled = service.signInEpoch
        pending.start {
            await network.wait()
            guard SessionEventGate.shouldRevoke(scheduledEpoch: scheduled, currentEpoch: service.signInEpoch)
            else { return }
            service.logoutCalls += 1
        }

        // The next sign-in gives up waiting and goes ahead.
        await pending.wait(atMost: .milliseconds(20))
        service.signInEpoch += 1

        network.release()
        await pending.wait(atMost: .seconds(30))

        #expect(!pending.isPending)
        #expect(service.logoutCalls == 0)
    }

    /// The same, with nobody signing in: the logout is sent once the network answers.
    @MainActor
    @Test func aRevokeWithNoSignInSinceCallsLogout() async {
        let pending = PendingRevoke()
        let service = RevokingService()

        let scheduled = service.signInEpoch
        pending.start {
            guard SessionEventGate.shouldRevoke(scheduledEpoch: scheduled, currentEpoch: service.signInEpoch)
            else { return }
            service.logoutCalls += 1
        }
        await pending.wait(atMost: .seconds(30))

        #expect(service.logoutCalls == 1)
    }

    // MARK: Credentials handed over again mid-session

    /// A step-up or device-registration code returns the same member's credentials. The
    /// session carries on, keeping the token an in-flight request was sent with.
    @Test func theSameMemberMidSessionContinuesTheSession() {
        #expect(SessionEventGate.continuesOpenSession(openUserID: "user-a", isSigningOut: false, newUserID: "user-a"))
    }

    @Test func aDifferentMemberIsANewSession() {
        #expect(!SessionEventGate.continuesOpenSession(openUserID: "user-a", isSigningOut: false, newUserID: "user-b"))
    }

    @Test func noOpenSessionMeansANewSession() {
        #expect(!SessionEventGate.continuesOpenSession(openUserID: nil, isSigningOut: false, newUserID: "user-a"))
    }

    @Test func signingInDuringASignOutIsANewSession() {
        #expect(!SessionEventGate.continuesOpenSession(openUserID: "user-a", isSigningOut: true, newUserID: "user-a"))
    }

    /// Dynamic did not name the user; two unnamed sign-ins are not known to be the same one.
    @Test func anUnnamedUserIsAlwaysANewSession() {
        #expect(!SessionEventGate.continuesOpenSession(openUserID: "", isSigningOut: false, newUserID: ""))
    }
}

/// Stands in for the service's epoch and Dynamic's logout.
@MainActor
private final class RevokingService {
    var signInEpoch = 1
    var logoutCalls = 0
}

/// A call that does not return until the test releases it.
@MainActor
private final class HeldCall {
    private var continuation: CheckedContinuation<Void, Never>?
    private var isReleased = false

    func wait() async {
        guard !isReleased else { return }
        await withCheckedContinuation { continuation = $0 }
    }

    func release() {
        isReleased = true
        continuation?.resume()
        continuation = nil
    }
}
