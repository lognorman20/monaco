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
    enum Phase: Equatable {
        case restoring
        case restoreFailed(message: String)
        case idle
        case sendingCode
        case awaitingCode
        case verifyingCode
        case codeRejected(message: String)
        case authenticated(userID: String)
        case failed(message: String)
    }

    @Published private(set) var phase: Phase
    @Published private(set) var accessToken: String?
    @Published private(set) var lastSignOutReason: String?
    @Published private(set) var needsDeviceRegistration = false
    @Published private(set) var needsStepUp = false

    private var sessionStore = MonacoSessionStore()
    private let tokenRefresh = SingleFlight<String?>()
    private var isRestoreInFlight = false
    private var cancellables = Set<AnyCancellable>()
    private let sdk: DynamicSDK?

    private static var stepUpScope: TokenScope {
        TokenScope.allCases.first(where: { $0.rawValue.contains("basic") }) ?? TokenScope.allCases[TokenScope.allCases.startIndex]
    }

    init(settings: DynamicAuthSettings) {
        if settings.isConfigured {
            sdk = DynamicAuthService.ensureSDK(environmentID: settings.environmentID)
        } else {
            sdk = nil
        }
        phase = sessionStore.hasExplicitLogin ? .restoring : .idle
        observeSDK()

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

    private func observeSDK() {
        guard let sdk else { return }
        sdk.auth.tokenChanges
            .receive(on: DispatchQueue.main)
            .sink { [weak self] token in
                guard let self, self.accessToken != nil else { return }
                self.accessToken = token
            }
            .store(in: &cancellables)

        sdk.auth.authenticatedUserChanges
            .receive(on: DispatchQueue.main)
            .sink { [weak self] user in
                guard let self else { return }
                if user == nil, case .authenticated = self.phase {
                    self.endSession(reason: LoginFailureCopy.sessionExpired)
                }
                self.refreshSecurityFlags()
            }
            .store(in: &cancellables)

        sdk.deviceRegistration.isDeviceRegistrationRequiredChanges
            .receive(on: DispatchQueue.main)
            .sink { [weak self] required in
                self?.needsDeviceRegistration = required
            }
            .store(in: &cancellables)
    }

    func restoreSessionIfNeeded() async {
        guard accessToken == nil, sessionStore.hasExplicitLogin else {
            if phase == .restoring { phase = .idle }
            return
        }
        switch phase {
        case .restoring, .restoreFailed: break
        default: return
        }
        guard !isRestoreInFlight else { return }
        isRestoreInFlight = true
        defer { isRestoreInFlight = false }

        phase = .restoring
        guard let sdk else {
            phase = .restoreFailed(message: LoginFailureCopy.restoreOffline)
            return
        }
        if let user = sdk.auth.authenticatedUser, let token = sdk.auth.token {
            applyAuthenticated(userID: user.userId ?? "", token: token, isRestore: true)
            return
        }
        if sdk.auth.authenticatedUser == nil, sdk.auth.token == nil {
            AppLogger.session.notice("Session restore: Dynamic has no saved session")
            endSession(reason: LoginFailureCopy.sessionExpired)
            return
        }
        AppLogger.session.notice("Session restore: saved session could not be verified (offline)")
        phase = .restoreFailed(message: LoginFailureCopy.restoreOffline)
    }

    func shouldInvalidateBackendSession(serverUserId: String) -> Bool {
        sessionStore.shouldInvalidateSession(serverUserId: serverUserId)
    }

    func recordBackendSession(userId: String) {
        sessionStore.recordSession(userId: userId)
    }

    func refreshedAccessToken(replacing rejectedToken: String) async throws -> String? {
        if let current = accessToken, current != rejectedToken {
            return current
        }
        guard let sdk else { return nil }
        if sdk.auth.authenticatedUser == nil {
            return nil
        }
        let fresh = sdk.auth.token
        if let fresh, fresh != rejectedToken, accessToken != nil {
            accessToken = fresh
        }
        return fresh
    }

    func sendSMSCode(to phoneNumberE164: String) async {
        await sendCode {
            let phone = try E164Phone.data(from: phoneNumberE164)
            try await self.requireSDK().auth.sms.sendOTP(phoneData: phone)
        }
    }

    func loginWithSMSCode(_ code: String, sentTo _: String) async {
        await verifyCode {
            try await self.requireSDK().auth.sms.verifyOTP(token: code)
        }
    }

    func sendEmailCode(to email: String) async {
        await sendCode {
            try await self.requireSDK().auth.email.sendOTP(email: email)
        }
    }

    func loginWithEmailCode(_ code: String, sentTo _: String) async {
        await verifyCode {
            try await self.requireSDK().auth.email.verifyOTP(token: code)
        }
    }

    func sendDeviceRegistrationCode() async {
        await sendCode {
            try await self.requireSDK().auth.email.resendOTP()
        }
    }

    func completeDeviceRegistration(_ code: String) async {
        await verifySecurityStep {
            try await self.requireSDK().auth.email.verifyOTP(token: code)
            self.refreshSecurityFlags()
        }
    }

    func sendStepUpCode() async {
        await sendCode {
            _ = try await self.requireSDK().stepUpAuth.sendOtp()
        }
    }

    func completeStepUp(_ code: String) async {
        await verifySecurityStep {
            try await self.requireSDK().stepUpAuth.verifyOtp(
                verificationToken: code,
                requestedScopes: [Self.stepUpScope]
            )
            self.refreshSecurityFlags()
        }
    }

    private func sendCode(_ send: () async throws -> Void) async {
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

    private func verifyCode(_ verify: () async throws -> Void) async {
        guard phase != .verifyingCode, phase != .sendingCode else { return }
        phase = .verifyingCode
        do {
            try await verify()
            captureAuthenticatedSession(isRestore: false)
        } catch {
            accessToken = nil
            let failure = Self.loginFailure(from: error, step: .verifyCode)
            AppLogger.session.error("Verify code failed: \(String(describing: error), privacy: .public)")
            let message = LoginFailureCopy.message(for: failure, step: .verifyCode)
            phase = failure.keepsCodeEntry ? .codeRejected(message: message) : .failed(message: message)
        }
    }

    private func verifySecurityStep(_ verify: () async throws -> Void) async {
        guard phase != .verifyingCode, phase != .sendingCode else { return }
        phase = .verifyingCode
        do {
            try await verify()
            if let user = sdk?.auth.authenticatedUser, let token = sdk?.auth.token {
                applyAuthenticated(userID: user.userId ?? "", token: token, isRestore: false)
            } else {
                phase = .failed(message: LoginFailureCopy.tokenUnavailable)
            }
        } catch {
            let failure = Self.loginFailure(from: error, step: .verifyCode)
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

    func logout() async {
        await performLogout()
    }

    func signOut(reason: String) async {
        await performLogout()
        lastSignOutReason = reason
    }

    func signOutAfterRejectedSession() async {
        await signOut(reason: LoginFailureCopy.sessionExpired)
    }

    private func performLogout() async {
        if let sdk {
            try? await sdk.auth.logout()
        }
        endSession(reason: nil)
    }

    private func endSession(reason: String?) {
        accessToken = nil
        lastSignOutReason = reason
        needsDeviceRegistration = false
        needsStepUp = false
        phase = .idle
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

    private func captureAuthenticatedSession(isRestore: Bool) {
        guard let sdk, let user = sdk.auth.authenticatedUser else {
            phase = .failed(message: LoginFailureCopy.tokenUnavailable)
            return
        }
        guard let token = sdk.auth.token else {
            phase = .failed(message: LoginFailureCopy.tokenUnavailable)
            return
        }
        applyAuthenticated(userID: user.userId ?? "", token: token, isRestore: isRestore)
    }

    private func applyAuthenticated(userID: String, token: String, isRestore: Bool) {
        accessToken = token
        lastSignOutReason = nil
        phase = .authenticated(userID: userID)
        if !isRestore {
            sessionStore.markExplicitLogin()
        }
        refreshSecurityFlags()
    }

    private func refreshSecurityFlags() {
        guard let sdk else { return }
        needsDeviceRegistration = sdk.deviceRegistration.isDeviceRegistrationRequired
        Task {
            let required = (try? await sdk.stepUpAuth.isStepUpRequired(scope: Self.stepUpScope)) ?? false
            self.needsStepUp = required
        }
    }

    private func requireSDK() throws -> DynamicSDK {
        guard let sdk else {
            throw DynamicAuthHTTPError(status: 500, detail: "Dynamic is not configured")
        }
        return sdk
    }
}

private enum E164Phone {
    static func data(from raw: String) throws -> PhoneData {
        var digits = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if digits.hasPrefix("+") {
            digits = String(digits.dropFirst())
        }
        digits = digits.filter(\.isNumber)
        if digits.hasPrefix("1"), digits.count == 11 {
            return PhoneData(dialCode: "+1", iso2: "US", phone: String(digits.dropFirst()))
        }
        if digits.hasPrefix("44"), digits.count >= 11 {
            return PhoneData(dialCode: "+44", iso2: "GB", phone: String(digits.dropFirst(2)))
        }
        guard digits.count >= 8 else {
            throw DynamicAuthHTTPError(status: 422, detail: "That phone number looks incomplete.")
        }
        return PhoneData(dialCode: "+1", iso2: "US", phone: digits)
    }
}
