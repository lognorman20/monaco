/// Whether something the Dynamic SDK reports, or something a sign-out left running, still
/// concerns the session that is open now.
///
/// Dynamic's logout is global and its publishers are process-wide, so an event can arrive
/// for a session that has already been replaced: a sign-out's revoke finishing after the
/// next member signed in, or the SDK republishing the old token after a logout. Acting on
/// one of those signs out the wrong account or hands it the wrong token.
///
/// `DynamicAuthService` reads its state into these and does what they say. Only the SDK
/// plumbing around them needs a Dynamic environment; the decisions are pure and tested in
/// `SessionEventGateTests`.
///
/// Epochs are `DynamicAuthService.signInEpoch`: bumped by every new sign-in, captured by each
/// subscription and each revoke when it is made.
enum SessionEventGate {
    /// The SDK reported a change of user. It ends the open session only when it is news
    /// about that session:
    /// - the event says nobody is signed in (a user appearing is not an ending);
    /// - it was subscribed for the sign-in that is open now, not an earlier one;
    /// - no revoke of ours is running, since that revoke's logout is what the SDK would be
    ///   reporting;
    /// - the member is signed in as far as the app is concerned;
    /// - and the SDK, asked now, really holds no user. The event may be stale; the SDK's
    ///   current state is not.
    static func shouldEndSession(
        eventHasUser: Bool,
        eventEpoch: Int,
        currentEpoch: Int,
        revokePending: Bool,
        isAuthenticated: Bool,
        sdkHoldsUser: Bool
    ) -> Bool {
        !eventHasUser
            && eventEpoch == currentEpoch
            && !revokePending
            && isAuthenticated
            && !sdkHoldsUser
    }

    /// The SDK rotated its token. The value the event carried is not used: after a logout
    /// the SDK can republish the old token. What is adopted is `sdkToken`, what the SDK holds
    /// now, and only while the session the subscription was made for is still open and not
    /// on its way out. A token equal to the one held is not news.
    static func shouldAdoptToken(
        eventEpoch: Int,
        currentEpoch: Int,
        isSigningOut: Bool,
        isAuthenticated: Bool,
        sdkToken: String?,
        current: String?
    ) -> Bool {
        guard eventEpoch == currentEpoch, !isSigningOut, isAuthenticated else { return false }
        guard let sdkToken, !sdkToken.isEmpty else { return false }
        return sdkToken != current
    }

    /// A sign-out's background revoke, about to call Dynamic. It runs only if no sign-in
    /// has happened since it was scheduled: Dynamic's logout is global, so a late revoke
    /// would end the session that replaced the one it was meant for. This check is what makes
    /// it safe for a new sign-in to stop waiting for the revoke after a bounded time.
    static func shouldRevoke(scheduledEpoch: Int, currentEpoch: Int) -> Bool {
        scheduledEpoch == currentEpoch
    }

    /// The SDK handed over credentials again while a session is open, as it does after a
    /// device-registration or step-up code. When they are for the member who is already
    /// signed in, and no sign-out is under way, this is the same session carrying on, not a
    /// new one: the tokens it has used stay valid for 401s still in flight, and its
    /// subscriptions and epoch stay as they are.
    ///
    /// An unknown user ID never counts as the same member, so a sign-in the SDK could not
    /// name is always treated as new.
    static func continuesOpenSession(
        openUserID: String?,
        isSigningOut: Bool,
        newUserID: String
    ) -> Bool {
        guard !isSigningOut, let openUserID, !openUserID.isEmpty else { return false }
        return openUserID == newUserID
    }
}
