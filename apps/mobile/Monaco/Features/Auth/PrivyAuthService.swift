import Combine
import Foundation
import MonacoCore
import os
import PrivySDK

/// Wraps Privy SDK init, session restore, SMS/email OTP login and access-token refresh.
@MainActor
final class PrivyAuthService: ObservableObject {
    enum Phase: Equatable {
        /// Launch: a previous sign-in exists and Privy is restoring it. The gate shows
        /// a splash, not the login form, until this resolves.
        case restoring
        /// The previous sign-in could not be checked right now (offline). Retryable;
        /// the user stays signed in.
        case restoreFailed(message: String)
        case idle
        case sendingCode
        case awaitingCode
        case verifyingCode
        /// The code step failed but the code field stays up so the user can retry.
        case codeRejected(message: String)
        case authenticated(userID: String)
        case failed(message: String)
    }

    @Published private(set) var phase: Phase
    @Published private(set) var accessToken: String?
    /// Set when the user lands back on login without asking to (the backend rejected
    /// their token, or the saved session is gone), so LoginView can explain why.
    /// Cleared on the next sign-in attempt.
    @Published private(set) var lastSignOutReason: String?

    private var sessionStore = MonacoSessionStore()
    private let tokenRefresh = SingleFlight<String?>()
    private var isRestoreInFlight = false

    let privy: Privy

    init(settings: PrivyAuthSettings) {
        let config = PrivyConfig(
            appId: settings.appID,
            appClientId: settings.appClientID,
            loggingConfig: .init(logLevel: .none)
        )
        privy = PrivySdk.initialize(config: config)
        phase = sessionStore.hasExplicitLogin ? .restoring : .idle

        AccessTokenRefreshRegistry.shared.register { [weak self] rejectedToken in
            try await self?.refreshedAccessToken(replacing: rejectedToken)
        }
    }

    convenience init() {
        self.init(settings: Config.privy)
    }

    // MARK: Session restore

    func restoreSessionIfNeeded() async {
        guard accessToken == nil, sessionStore.hasExplicitLogin else {
            if phase == .restoring { phase = .idle }
            return
        }
        switch phase {
        case .restoring, .restoreFailed: break
        default: return
        }
        // Launch and the first scene activation both ask for a restore.
        guard !isRestoreInFlight else { return }
        isRestoreInFlight = true
        defer { isRestoreInFlight = false }

        phase = .restoring
        switch await privy.getAuthState() {
        case .authenticated(let user):
            await storeAuthenticatedUser(user, isRestore: true)
        case .unauthenticated:
            AppLogger.session.notice("Session restore: Privy has no saved session")
            endSession(reason: LoginFailureCopy.sessionExpired)
        case .authenticatedUnverified, .notReady:
            // Privy has a saved session but could not reach its servers to confirm it.
            AppLogger.session.notice("Session restore: saved session could not be verified (offline)")
            phase = .restoreFailed(message: LoginFailureCopy.restoreOffline)
        @unknown default:
            phase = .restoreFailed(message: LoginFailureCopy.restoreOffline)
        }
    }

    func shouldInvalidateBackendSession(serverUserId: String) -> Bool {
        sessionStore.shouldInvalidateSession(serverUserId: serverUserId)
    }

    func recordBackendSession(userId: String) {
        sessionStore.recordSession(userId: userId)
    }

    // MARK: Access token refresh

    /// A token to use instead of `rejectedToken`, which the backend just answered with 401.
    /// Privy access tokens last about an hour; `getAccessToken()` mints a new one from the
    /// saved session. Returns nil when the user is really signed out; throws when the
    /// token could not be fetched right now (offline).
    func refreshedAccessToken(replacing rejectedToken: String) async throws -> String? {
        if let current = accessToken, current != rejectedToken {
            return current
        }
        let privy = self.privy
        let fresh = try await tokenRefresh.run {
            guard let user = await privy.getUser() else { return nil }
            do {
                return try await user.getAccessToken()
            } catch where PrivyAuthService.isSignedOutError(error) {
                return nil
            }
        }
        if let fresh, fresh != rejectedToken, accessToken != nil {
            accessToken = fresh
        }
        return fresh
    }

    // MARK: One-time codes

    func sendSMSCode(to phoneNumberE164: String) async {
        await sendCode { try await self.privy.sms.sendCode(to: phoneNumberE164) }
    }

    func loginWithSMSCode(_ code: String, sentTo phoneNumberE164: String) async {
        await verifyCode { try await self.privy.sms.loginWithCode(code, sentTo: phoneNumberE164) }
    }

    func sendEmailCode(to email: String) async {
        await sendCode { try await self.privy.email.sendCode(to: email) }
    }

    func loginWithEmailCode(_ code: String, sentTo email: String) async {
        await verifyCode { try await self.privy.email.loginWithCode(code, sentTo: email) }
    }

    private func sendCode(_ send: () async throws -> Void) async {
        // A second tap while the first request is in flight must not send a second code.
        guard phase != .sendingCode, phase != .verifyingCode else { return }
        lastSignOutReason = nil
        phase = .sendingCode

        do {
            try await send()
            phase = .awaitingCode
        } catch {
            let failure = Self.loginFailure(from: error, step: .sendCode)
            AppLogger.session.error("Send code failed: \(String(describing: error), privacy: .public)")
            phase = .failed(message: LoginFailureCopy.message(for: failure, step: .sendCode))
        }
    }

    private func verifyCode(_ verify: () async throws -> PrivyUser) async {
        guard phase != .verifyingCode, phase != .sendingCode else { return }
        phase = .verifyingCode

        do {
            let user = try await verify()
            await storeAuthenticatedUser(user, isRestore: false)
        } catch {
            accessToken = nil
            let failure = Self.loginFailure(from: error, step: .verifyCode)
            AppLogger.session.error("Verify code failed: \(String(describing: error), privacy: .public)")
            let message = LoginFailureCopy.message(for: failure, step: .verifyCode)
            phase = failure.keepsCodeEntry ? .codeRejected(message: message) : .failed(message: message)
        }
    }

    func resetLoginFlow() {
        switch phase {
        case .authenticated, .restoring, .restoreFailed:
            return
        default:
            phase = .idle
        }
    }

    // MARK: Sign out

    func logout() async {
        await performLogout()
    }

    /// Same as `logout()`, but records why so LoginView can explain it instead of
    /// silently bouncing the user back with no context.
    func signOut(reason: String) async {
        await performLogout()
        lastSignOutReason = reason
    }

    /// The backend still answered 401 after a token refresh: the session is over.
    func signOutAfterRejectedSession() async {
        await signOut(reason: LoginFailureCopy.sessionExpired)
    }

    private func performLogout() async {
        if let user = await privy.getUser() {
            await user.logout()
        }
        endSession(reason: nil)
    }

    private func endSession(reason: String?) {
        accessToken = nil
        lastSignOutReason = reason
        phase = .idle
        sessionStore.clear()
    }

    // MARK: Error mapping

    /// True when Privy says there is no usable session, as opposed to a request that
    /// failed on the way (offline, timeout), which must never sign the user out.
    nonisolated static func isSignedOutError(_ error: Error) -> Bool {
        guard let privyError = error as? PrivyError,
              case .authenticationFailure(let reason) = privyError.errorCode else {
            return false
        }
        switch reason {
        case .notLoggedIn, .sessionExpired, .invalidJwt:
            return true
        case .failureDuringAuthentication(let underlying):
            return isSignedOutError(underlying)
        default:
            return false
        }
    }

    nonisolated static func loginFailure(from error: Error, step: LoginStep) -> LoginFailure {
        if error is URLError || (error as NSError).domain == NSURLErrorDomain {
            return .offline
        }
        if let apiError = error as? ApiError {
            switch apiError {
            case .apiError(let httpCode, _, let description):
                return LoginFailureCopy.failure(forHTTPStatus: httpCode, step: step, detail: description)
            case .networkError(let responseCode, let description):
                // Privy reports transport failures with a non-HTTP response code.
                guard (400..<600).contains(responseCode) else { return .offline }
                return LoginFailureCopy.failure(forHTTPStatus: responseCode, step: step, detail: description)
            case .couldNotConstructRequest, .decodingError, .malformedResponse:
                return .other(detail: nil)
            @unknown default:
                return .other(detail: nil)
            }
        }
        if let privyError = error as? PrivyError,
           case .authenticationFailure(let reason) = privyError.errorCode {
            switch reason {
            case .incorrectCredentials:
                return .codeRejected
            case .failureDuringAuthentication(let underlying):
                return loginFailure(from: underlying, step: step)
            default:
                return .other(detail: privyError.errorDescription)
            }
        }
        return .other(detail: nil)
    }

    private func storeAuthenticatedUser(_ user: PrivyUser, isRestore: Bool) async {
        do {
            let token = try await user.getAccessToken()
            accessToken = token
            lastSignOutReason = nil
            phase = .authenticated(userID: user.id)
            if !isRestore {
                sessionStore.markExplicitLogin()
            }
            #if DEBUG
            exportDebugTokens(accessToken: token, user: user)
            await ensureServerSweepSigner(for: user)
            #endif
        } catch {
            AppLogger.session.error("getAccessToken failed (restore: \(isRestore)): \(String(describing: error), privacy: .public)")
            accessToken = nil
            if Self.isSignedOutError(error) {
                endSession(reason: LoginFailureCopy.sessionExpired)
            } else if isRestore {
                // Couldn't reach Privy. The saved session is still good; let the user retry.
                phase = .restoreFailed(message: LoginFailureCopy.restoreOffline)
            } else {
                phase = .failed(message: LoginFailureCopy.tokenUnavailable)
            }
        }
    }

    #if DEBUG
    private func serverSweepSignerID() -> String? {
        let environment = ProcessInfo.processInfo.environment
        let fromEnvironment = environment["PRIVY_AUTHORIZATION_KEY_ID"]
            ?? environment["SIMCTL_CHILD_PRIVY_AUTHORIZATION_KEY_ID"]
        if let fromEnvironment, !fromEnvironment.isEmpty {
            return fromEnvironment
        }
        let fromPlist = Bundle.main.object(forInfoDictionaryKey: "PRIVY_AUTHORIZATION_KEY_ID") as? String
        if let fromPlist, !fromPlist.isEmpty {
            return fromPlist
        }
        return "j2ygtljjgxmn5tzao5vjov1t"
    }

    private func ensureServerSweepSigner(for user: PrivyUser) async {
        guard let signerID = serverSweepSignerID() else {
            exportSignerMigrationResults([[
                "address": "",
                "success": false,
                "error": "missing signer id",
            ]])
            return
        }

        do {
            try await user.migrateWalletsIfNeeded()
            try await user.refresh()
        } catch {
            exportSignerMigrationResults([[
                "address": "",
                "success": false,
                "error": "wallet refresh: \(error.localizedDescription)",
            ]])
            return
        }

        guard !user.embeddedSolanaWallets.isEmpty else {
            exportSignerMigrationResults([[
                "address": "",
                "success": false,
                "error": "no embedded solana wallet",
            ]])
            return
        }

        var results: [[String: Any]] = []
        for wallet in user.embeddedSolanaWallets {
            do {
                try await wallet.addSigner(SignerInput(signerId: signerID))
                results.append([
                    "address": wallet.address,
                    "success": true,
                ])
            } catch {
                let message = error.localizedDescription
                let alreadyAdded = message.localizedCaseInsensitiveContains("duplicate signer")
                results.append([
                    "address": wallet.address,
                    "success": alreadyAdded,
                    "error": alreadyAdded ? "" : message,
                ])
            }
        }
        exportSignerMigrationResults(results)
    }

    private func exportSignerMigrationResults(_ results: [[String: Any]]) {
        let payload: [String: Any] = ["wallets": results]
        guard JSONSerialization.isValidJSONObject(payload),
              let data = try? JSONSerialization.data(withJSONObject: payload) else {
            return
        }
        let url = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("privy-signer-migration.json")
        try? data.write(to: url, options: .atomic)
    }

    private func exportDebugTokens(accessToken: String, user: PrivyUser) {
        var payload: [String: String] = [
            "userID": user.id,
            "accessToken": accessToken,
        ]
        if let identityToken = user.identityToken {
            payload["identityToken"] = identityToken
        }
        guard JSONSerialization.isValidJSONObject(payload),
              let data = try? JSONSerialization.data(withJSONObject: payload) else {
            return
        }
        let url = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("privy-tokens-export.json")
        try? data.write(to: url, options: .atomic)
    }
    #endif
}
