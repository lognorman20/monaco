import Combine
import DynamicSDKSwift
import Foundation
import MonacoCore
import os

/// HTTP status from Dynamic (or tests) so login copy can map 429 / 422 without a vendor HTTP error type.
struct DynamicAuthHTTPError: Error, Equatable {
    let status: Int
    let detail: String?
}

enum DynamicSessionGoneError: Error {
    case signedOut
}

/// Wraps Dynamic SDK init, session restore, SMS/email OTP, device registration, and step-up.
@MainActor
final class DynamicAuthService: ObservableObject {
    typealias Phase = LoginFlow.Phase

    /// Where the login form is and what just happened to it. See `LoginFlow`.
    @Published private(set) var flow: LoginFlow
    @Published private(set) var accessToken: String?
    @Published private(set) var lastSignOutReason: String?
    @Published private(set) var needsDeviceRegistration = false
    @Published private(set) var needsStepUp = false
    @Published private(set) var loginChannel: DynamicLoginChannel = .sms
    @Published private(set) var securityOTPMessage: String?

    private var sessionStore: MonacoSessionStore
    private let tokenRefresh = SingleFlight<String?>()
    private var isRestoreInFlight = false
    /// Subscriptions that belong to the open sign-in. Cancelled when it ends, so a late
    /// event from the SDK about the old session is never delivered to the next one.
    private var sessionCancellables = Set<AnyCancellable>()
    private let sdk: DynamicSDK?

    /// The access tokens this sign-in has used. A 401 for a token that is not in here
    /// belongs to a session that has already ended, so it must neither mint a token nor
    /// sign out whoever is signed in now.
    private var sessionTokens = SessionTokenLedger()
    /// Set the moment a sign-out starts, so a late 401 cannot stamp "session expired"
    /// over a sign-out the member asked for, and a second tap cannot start a second one.
    private var isSigningOut = false
    /// Best-effort revoke of the Dynamic session, running after local state is already gone.
    private let pendingRevoke = PendingRevoke()
    /// Bumped by every successful sign-in, so a revoke or an SDK event that belongs to an
    /// earlier session can tell that the session it is about is no longer the open one.
    private var signInEpoch = 0
    /// How long a new sign-in will wait out a revoke before going ahead anyway.
    private static let revokeWait = Swift.Duration.seconds(2)

    private static var stepUpScope: TokenScope {
        .userUpdate
    }

    /// What just happened to the login form. `flow.step` says which field is on screen.
    var phase: Phase { flow.phase }

    /// Which field the login form is on, so a failed request never takes the code box away
    /// from a member who already has a code.
    var loginStep: LoginFlow.Step { flow.step }

    /// Who is signed in. Screens key their loads on this rather than on `accessToken`, so
    /// Dynamic's token rotation is not mistaken for a new session.
    var sessionIdentity: String? {
        guard case .authenticated(let userID) = phase, accessToken != nil else { return nil }
        return userID
    }

    /// `sessionStore` is injected so tests keep their markers out of the app's own defaults;
    /// the app always uses the standard one.
    init(settings: DynamicAuthSettings, sessionStore: MonacoSessionStore = MonacoSessionStore()) {
        self.sessionStore = sessionStore
        if settings.isConfigured {
            sdk = DynamicAuthService.ensureSDK(environmentID: settings.environmentID)
        } else {
            sdk = nil
        }
        flow = LoginFlow(phase: sessionStore.hasExplicitLogin ? .restoring : .idle)

        AccessTokenRefreshRegistry.shared.register { [weak self] rejectedToken in
            try await self?.refreshedAccessToken(replacing: rejectedToken)
        }
    }

    convenience init() {
        self.init(settings: Config.dynamic)
    }

    static func ensureSDK(environmentID: String) -> DynamicSDK {
        DynamicSDK.initialize(
            props: ClientProps(
                environmentId: environmentID,
                appLogoUrl: "https://monaco.app/logo.png",
                appName: "Monaco",
                redirectUrl: "monaco://",
                appOrigin: "https://monaco.app"
            )
        )
    }

    /// Follows the SDK for the sign-in that just opened, and only for it.
    ///
    /// Dynamic's logout is global, and a sign-out's revoke can still be running when the next
    /// member signs in. Every handler here captures the epoch it was created for and drops
    /// events once another sign-in has replaced it; the subscriptions themselves are
    /// cancelled when the session ends.
    ///
    /// Device registration / step-up would send a second OTP (often email).
    /// Product login is SMS only — those SDK flags are ignored (`refreshSecurityFlags`).
    private func observeSession(epoch: Int) {
        sessionCancellables.removeAll()
        guard let sdk else { return }

        sdk.auth.minAuthTokenChanges
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in self?.sdkTokenChanged(epoch: epoch) }
            .store(in: &sessionCancellables)

        sdk.auth.tokenChanges
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in self?.sdkTokenChanged(epoch: epoch) }
            .store(in: &sessionCancellables)

        sdk.auth.authenticatedUserChanges
            .receive(on: DispatchQueue.main)
            .sink { [weak self] user in self?.sdkUserChanged(signedIn: user != nil, epoch: epoch) }
            .store(in: &sessionCancellables)
    }

    /// A rotated token. The value the publisher carried is not trusted: after a logout the
    /// SDK can republish the old token, so what is adopted is what the SDK holds right now,
    /// and only while the session it was subscribed for is still open.
    private func sdkTokenChanged(epoch: Int) {
        let held = sdkSessionJWT()
        guard SessionEventGate.shouldAdoptToken(
            eventEpoch: epoch,
            currentEpoch: signInEpoch,
            isSigningOut: isSigningOut,
            isAuthenticated: isAuthenticated,
            sdkToken: held,
            current: accessToken
        ), let held else { return }
        adoptAccessToken(held)
    }

    /// The SDK says nobody is signed in. Ends the session only when that is news about the
    /// session that is open now: not while our own revoke is running (its logout is what the
    /// SDK is reporting), not for an earlier sign-in, and not when the SDK in fact still
    /// holds a user.
    private func sdkUserChanged(signedIn: Bool, epoch: Int) {
        defer { refreshSecurityFlags() }
        guard SessionEventGate.shouldEndSession(
            eventHasUser: signedIn,
            eventEpoch: epoch,
            currentEpoch: signInEpoch,
            revokePending: pendingRevoke.isPending,
            isAuthenticated: isAuthenticated,
            sdkHoldsUser: sdk?.auth.authenticatedUser != nil
        ) else { return }
        endSession(reason: LoginFailureCopy.sessionExpired)
    }

    private var isAuthenticated: Bool {
        if case .authenticated = phase { return true }
        return false
    }

    func restoreSessionIfNeeded() async {
        guard accessToken == nil, sessionStore.hasExplicitLogin else {
            if phase == .restoring { flow.signedOut() }
            return
        }
        switch phase {
        case .restoring, .restoreFailed: break
        default: return
        }
        guard !isRestoreInFlight else { return }
        isRestoreInFlight = true
        defer { isRestoreInFlight = false }

        flow.restoring()
        guard let sdk else {
            flow.restoreFailed(message: LoginFailureCopy.restoreOffline)
            return
        }
        if let creds = sdkSessionCredentials() {
            applyAuthenticated(userID: creds.userID, token: creds.token, isRestore: true)
            return
        }
        if sdk.auth.authenticatedUser == nil, sdkSessionJWT() == nil {
            AppLogger.session.notice("Session restore: Dynamic has no saved session")
            endSession(reason: LoginFailureCopy.sessionExpired)
            return
        }
        AppLogger.session.notice("Session restore: saved session could not be verified (offline)")
        flow.restoreFailed(message: LoginFailureCopy.restoreOffline)
    }

    func shouldInvalidateBackendSession(serverUserId: String) -> Bool {
        sessionStore.shouldInvalidateSession(serverUserId: serverUserId)
    }

    func recordBackendSession(userId: String) {
        sessionStore.recordSession(userId: userId)
    }

    func refreshedAccessToken(replacing rejectedToken: String) async throws -> String? {
        // The request was made by a session that has since ended (sign-out, or another
        // account signed in). Retrying it under the current token would run one member's
        // request as another.
        //
        // Checked once, with nothing awaited between here and `adoptAccessToken`, so the
        // answer cannot go stale before it is acted on. If an `await` is ever added below
        // (an async SDK refresh, say), check the ledger again after it: a sign-out or a new
        // sign-in can land during the suspension.
        guard sessionTokens.contains(rejectedToken) else { return nil }
        if let current = accessToken, current != rejectedToken {
            return current
        }
        guard let sdk else { return nil }
        if sdk.auth.authenticatedUser == nil {
            return nil
        }
        let fresh = sdkSessionJWT()
        if let fresh, fresh != rejectedToken, accessToken != nil {
            adoptAccessToken(fresh)
        }
        return fresh
    }

    private func adoptAccessToken(_ token: String) {
        accessToken = token
        sessionTokens.adopt(token)
    }

    // MARK: One-time codes

    func sendSMSCode(to phoneNumberE164: String) async {
        loginChannel = .sms
        await sendCode(to: phoneNumberE164) {
            try await self.requireSDK().auth.sms.sendOTP(phoneData: Self.phoneData(from: phoneNumberE164))
        }
    }

    func loginWithSMSCode(_ code: String, sentTo _: String) async {
        await verifyCode {
            try await self.requireSDK().auth.sms.verifyOTP(token: code)
        }
    }

    func sendEmailCode(to email: String) async {
        loginChannel = .email
        await sendCode(to: email) {
            try await self.requireSDK().auth.email.sendOTP(email: email)
        }
    }

    func loginWithEmailCode(_ code: String, sentTo _: String) async {
        await verifyCode {
            try await self.requireSDK().auth.email.verifyOTP(token: code)
        }
    }

    /// Dynamic takes the number split into dial code, region and subscriber number. The
    /// split comes off `E164PhoneNumber`, the same parser the form validated with, so there
    /// is one rule about what a sendable number is.
    static func phoneData(from raw: String) throws -> PhoneData {
        guard let number = E164PhoneNumber(raw) else {
            throw DynamicAuthHTTPError(status: 422, detail: "That phone number looks incomplete.")
        }
        return PhoneData(dialCode: "+" + number.countryCode, iso2: number.regionCode, phone: number.nationalNumber)
    }

    func sendDeviceRegistrationCode() async {
        await sendFollowUpOTP {
            try await self.resendOnLoginChannel()
        }
    }

    func completeDeviceRegistration(_ code: String) async {
        await verifyFollowUpOTP {
            try await self.verifyOnLoginChannel(code)
        }
    }

    func sendStepUpCode() async {
        await sendFollowUpOTP {
            _ = try await self.requireSDK().stepUpAuth.sendOtp()
        }
    }

    func completeStepUp(_ code: String) async {
        await verifyFollowUpOTP {
            try await self.requireSDK().stepUpAuth.verifyOtp(
                verificationToken: code,
                requestedScopes: [Self.stepUpScope]
            )
        }
    }

    private func resendOnLoginChannel() async throws {
        let sdk = try requireSDK()
        switch loginChannel {
        case .sms:
            try await sdk.auth.sms.resendOTP()
        case .email:
            try await sdk.auth.email.resendOTP()
        }
    }

    private func verifyOnLoginChannel(_ code: String) async throws {
        let sdk = try requireSDK()
        switch loginChannel {
        case .sms:
            try await sdk.auth.sms.verifyOTP(token: code)
        case .email:
            try await sdk.auth.email.verifyOTP(token: code)
        }
    }

    /// Device registration / step-up must not reuse login `sendCode`, which
    /// flips `phase` off `.authenticated` and can fire the other OTP channel.
    private func sendFollowUpOTP(_ send: () async throws -> Void) async {
        securityOTPMessage = nil
        do {
            try await send()
            securityOTPMessage = nil
        } catch {
            let failure = Self.loginFailure(from: error, step: .sendCode)
            AppLogger.session.error("Follow-up OTP send failed: \(String(describing: error), privacy: .public)")
            securityOTPMessage = LoginFailureCopy.message(for: failure, step: .sendCode)
        }
    }

    private func verifyFollowUpOTP(_ verify: () async throws -> Void) async {
        securityOTPMessage = nil
        do {
            try await verify()
            refreshSecurityFlags()
            await captureAuthenticatedSession(isRestore: false)
        } catch {
            let failure = Self.loginFailure(from: error, step: .verifyCode)
            AppLogger.session.error("Follow-up OTP verify failed: \(String(describing: error), privacy: .public)")
            securityOTPMessage = LoginFailureCopy.message(for: failure, step: .verifyCode)
        }
    }

    private func sendCode(to destination: String, _ send: () async throws -> Void) async {
        // A sign-out whose Dynamic revoke is still running would tear this session down
        // again. Waited out *before* the form is marked busy: this can give up after a
        // couple of seconds, and a disabled form with no cancel is not somewhere to leave
        // a member.
        await awaitPendingRevoke()
        // A second tap while the first request is in flight must not send a second code.
        guard flow.beginSend() else { return }
        lastSignOutReason = nil
        do {
            try await send()
            flow.sendSucceeded(destination: destination)
        } catch {
            let failure = Self.loginFailure(from: error, step: .sendCode)
            AppLogger.session.error("Send code failed: \(String(describing: error), privacy: .public)")
            // Note the failure, but leave the member where they are: a throttled resend
            // must not take away a code box they are about to use.
            flow.sendFailed(message: LoginFailureCopy.message(for: failure, step: .sendCode))
        }
    }

    private func verifyCode(_ verify: () async throws -> Void) async {
        await awaitPendingRevoke()
        guard flow.beginVerify() else { return }
        do {
            try await verify()
            await captureAuthenticatedSession(isRestore: false)
        } catch {
            let failure = Self.loginFailure(from: error, step: .verifyCode)
            AppLogger.session.error("Verify code failed: \(String(describing: error), privacy: .public)")
            // The code field stays up whatever went wrong; see `LoginFlow`.
            flow.verifyFailed(message: LoginFailureCopy.message(for: failure, step: .verifyCode))
        }
    }

    /// "Change number" / switching sign-in method.
    func resetLoginFlow() {
        flow.returnToAddressEntry()
    }

    // MARK: Sign out

    /// The member's own sign-out.
    func logout() async {
        performLogout(reason: nil)
    }

    /// Same as `logout()`, but records why so LoginView can explain it.
    ///
    /// Private on purpose: a sign-out driven by a server reply must name the token that
    /// reply rejected, so it goes through `signOut(reason:rejectedToken:)`. The member's own
    /// sign-out goes through `logout()`. Leaving this reachable is what lets a stale 401 end
    /// the wrong session.
    private func signOut(reason: String) async {
        performLogout(reason: reason)
    }

    /// A 401 answering a request made with `rejectedToken`, ending the session with a
    /// reason for the login screen to show. Guarded exactly like
    /// `signOutAfterRejectedSession(rejectedToken:)`: a reply that outlived its sign-in must
    /// not sign out the account signed in now, nor stamp its login screen with a reason
    /// meant for the previous one.
    func signOut(reason: String, rejectedToken: String) async {
        guard sessionTokens.contains(rejectedToken) else { return }
        await signOut(reason: reason)
    }

    /// The backend still answered 401 after a token refresh: the session is over.
    /// Private on purpose — every caller must come through a `rejectedToken` overload so
    /// the unguarded path cannot be reintroduced from another area.
    private func signOutAfterRejectedSession() async {
        await signOut(reason: LoginFailureCopy.sessionExpired)
    }

    /// A 401 answering a request made with `rejectedToken`. Ignored unless that token
    /// belongs to the session that is still open, so a reply that outlived its sign-in
    /// cannot sign out the next account or contradict a deliberate sign-out.
    func signOutAfterRejectedSession(rejectedToken: String) async {
        guard sessionTokens.contains(rejectedToken) else { return }
        await signOutAfterRejectedSession()
    }

    /// Sign-out is local-first: the session is gone before Dynamic is told, so the login
    /// screen comes back immediately even offline, polling loops lose their token at once,
    /// and a second tap has nothing left to do. `sdk.auth.logout()` used to be awaited
    /// first, which left the member on a dead screen for as long as the network took.
    private func performLogout(reason: String?) {
        guard !isSigningOut else { return }
        isSigningOut = true
        endSession(reason: reason)

        guard let sdk else { return }
        let epoch = signInEpoch
        pendingRevoke.start { [weak self] in
            // Someone has signed in since this revoke was scheduled. Dynamic's logout is
            // global, so revoking now would tear down the session that replaced this one.
            // This is what makes giving up on the wait safe.
            guard let self,
                  SessionEventGate.shouldRevoke(scheduledEpoch: epoch, currentEpoch: self.signInEpoch)
            else { return }
            try? await sdk.auth.logout()
        }
    }

    /// Lets a new sign-in wait out a revoke that is still in flight, so a late logout cannot
    /// tear down the session it is about to create — but only briefly. See `PendingRevoke`.
    private func awaitPendingRevoke() async {
        await pendingRevoke.wait(atMost: Self.revokeWait)
    }

    private func endSession(reason: String?) {
        sessionCancellables.removeAll()
        accessToken = nil
        sessionTokens.clear()
        lastSignOutReason = reason
        needsDeviceRegistration = false
        needsStepUp = false
        securityOTPMessage = nil
        flow.signedOut()
        sessionStore.clear()
    }

    nonisolated static func isSignedOutError(_ error: Error) -> Bool {
        if error is DynamicSessionGoneError { return true }
        if error is URLError { return false }
        if (error as NSError).domain == NSURLErrorDomain { return false }
        if error is DynamicAuthHTTPError { return false }
        let text = error.localizedDescription.lowercased()
        return text.contains("not authenticated") || text.contains("session expired")
    }

    nonisolated static func loginFailure(from error: Error, step: LoginStep) -> LoginFailure {
        if error is URLError || (error as NSError).domain == NSURLErrorDomain {
            return .offline
        }
        if let http = error as? DynamicAuthHTTPError {
            guard (400..<600).contains(http.status) else { return .offline }
            return LoginFailureCopy.failure(forHTTPStatus: http.status, step: step, detail: http.detail)
        }
        let text = error.localizedDescription.lowercased()
        if text.contains("invalid") || text.contains("incorrect") || text.contains("credential") {
            return .codeRejected
        }
        return .other(detail: nil)
    }

    private func captureAuthenticatedSession(isRestore: Bool) async {
        if let creds = await waitForSessionCredentials() {
            applyAuthenticated(userID: creds.userID, token: creds.token, isRestore: isRestore)
            return
        }
        if let user = sdk?.auth.authenticatedUser, let token = sdkSessionJWT(), !token.isEmpty {
            applyAuthenticated(userID: user.userId ?? "", token: token, isRestore: isRestore)
            return
        }
        flow.verifyFailed(message: LoginFailureCopy.tokenUnavailable)
    }

    private func waitForSessionCredentials() async -> (userID: String, token: String)? {
        for _ in 0..<40 {
            if let creds = sdkSessionCredentials() {
                return creds
            }
            try? await Task.sleep(for: .milliseconds(50))
        }
        return sdkSessionCredentials()
    }

    private func sdkSessionCredentials() -> (userID: String, token: String)? {
        guard let sdk, let user = sdk.auth.authenticatedUser else { return nil }
        guard let token = sdkSessionJWT() else { return nil }
        return (user.userId ?? "", token)
    }

    private func sdkSessionJWT() -> String? {
        guard let sdk else { return nil }
        return DynamicSessionToken.preferred(minAuthToken: sdk.auth.minAuthToken, idToken: sdk.auth.token)
    }

    private func applyAuthenticated(userID: String, token: String, isRestore: Bool) {
        // A device-registration or step-up code hands the same member's credentials back
        // mid-session. That session carries on: clearing the ledger here would drop the
        // token a request still in flight was sent with, so its 401 could neither refresh
        // nor sign out. Its epoch and subscriptions stay as they are for the same reason.
        if SessionEventGate.continuesOpenSession(
            openUserID: sessionIdentity,
            isSigningOut: isSigningOut,
            newUserID: userID
        ) {
            if !token.isEmpty, token != accessToken { adoptAccessToken(token) }
            securityOTPMessage = nil
            refreshSecurityFlags()
            return
        }
        isSigningOut = false
        // From here a revoke scheduled by the previous sign-out is stale: this is the
        // session Dynamic holds now, and ending it would sign the member straight out.
        signInEpoch += 1
        sessionTokens.clear()
        if token.isEmpty {
            accessToken = nil
        } else {
            adoptAccessToken(token)
        }
        lastSignOutReason = nil
        securityOTPMessage = nil
        flow.authenticated(userID: userID)
        if !isRestore {
            sessionStore.markExplicitLogin()
        }
        observeSession(epoch: signInEpoch)
        refreshSecurityFlags()
    }

    private func refreshSecurityFlags() {
        needsDeviceRegistration = false
        needsStepUp = false
    }

    private func requireSDK() throws -> DynamicSDK {
        guard let sdk else {
            throw DynamicAuthHTTPError(status: 500, detail: "Dynamic is not configured")
        }
        return sdk
    }
}
