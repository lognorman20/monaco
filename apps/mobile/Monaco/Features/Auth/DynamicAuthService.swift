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
    @Published private(set) var loginChannel: DynamicLoginChannel = .sms
    @Published private(set) var securityOTPMessage: String?

    private var sessionStore = MonacoSessionStore()
    private let tokenRefresh = SingleFlight<String?>()
    private var isRestoreInFlight = false
    private var cancellables = Set<AnyCancellable>()
    private let sdk: DynamicSDK?

    private static var stepUpScope: TokenScope {
        .userUpdate
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
        sdk.auth.minAuthTokenChanges
            .receive(on: DispatchQueue.main)
            .sink { [weak self] token in
                guard let self, let jwt = DynamicSessionToken.preferred(minAuthToken: token, idToken: nil) else { return }
                self.accessToken = jwt
            }
            .store(in: &cancellables)

        sdk.auth.tokenChanges
            .receive(on: DispatchQueue.main)
            .sink { [weak self] token in
                guard let self else { return }
                guard self.accessToken == nil else { return }
                guard let jwt = DynamicSessionToken.preferred(minAuthToken: nil, idToken: token) else { return }
                self.accessToken = jwt
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

        // Device registration / step-up would send a second OTP (often email).
        // Product login is SMS only — ignore those SDK flags.
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
        let fresh = sdkSessionJWT()
        if let fresh, fresh != rejectedToken, accessToken != nil {
            accessToken = fresh
        }
        return fresh
    }

    func sendSMSCode(to phoneNumberE164: String) async {
        loginChannel = .sms
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
        loginChannel = .email
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
            await captureAuthenticatedSession(isRestore: false)
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
        securityOTPMessage = nil
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

    private func captureAuthenticatedSession(isRestore: Bool) async {
        if let creds = await waitForSessionCredentials() {
            applyAuthenticated(userID: creds.userID, token: creds.token, isRestore: isRestore)
            return
        }
        if let user = sdk?.auth.authenticatedUser, let token = sdkSessionJWT(), !token.isEmpty {
            applyAuthenticated(userID: user.userId ?? "", token: token, isRestore: isRestore)
            return
        }
        phase = .failed(message: LoginFailureCopy.tokenUnavailable)
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
        accessToken = token.isEmpty ? nil : token
        lastSignOutReason = nil
        securityOTPMessage = nil
        phase = .authenticated(userID: userID)
        if !isRestore {
            sessionStore.markExplicitLogin()
        }
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

private enum E164Phone {
    private static let prefixes: [(code: String, iso2: String)] = [
        ("1", "US"), ("7", "RU"), ("20", "EG"), ("27", "ZA"), ("30", "GR"), ("31", "NL"),
        ("32", "BE"), ("33", "FR"), ("34", "ES"), ("36", "HU"), ("39", "IT"), ("40", "RO"),
        ("41", "CH"), ("43", "AT"), ("44", "GB"), ("45", "DK"), ("46", "SE"), ("47", "NO"),
        ("48", "PL"), ("49", "DE"), ("51", "PE"), ("52", "MX"), ("53", "CU"), ("54", "AR"),
        ("55", "BR"), ("56", "CL"), ("57", "CO"), ("58", "VE"), ("60", "MY"), ("61", "AU"),
        ("62", "ID"), ("63", "PH"), ("64", "NZ"), ("65", "SG"), ("66", "TH"), ("81", "JP"),
        ("82", "KR"), ("84", "VN"), ("86", "CN"), ("90", "TR"), ("91", "IN"), ("92", "PK"),
        ("93", "AF"), ("94", "LK"), ("95", "MM"), ("98", "IR"), ("212", "MA"), ("213", "DZ"),
        ("216", "TN"), ("218", "LY"), ("220", "GM"), ("234", "NG"), ("254", "KE"), ("255", "TZ"),
        ("256", "UG"), ("351", "PT"), ("352", "LU"), ("353", "IE"), ("354", "IS"), ("358", "FI"),
        ("370", "LT"), ("371", "LV"), ("372", "EE"), ("380", "UA"), ("381", "RS"), ("385", "HR"),
        ("386", "SI"), ("420", "CZ"), ("421", "SK"), ("852", "HK"), ("853", "MO"), ("855", "KH"),
        ("856", "LA"), ("880", "BD"), ("886", "TW"), ("960", "MV"), ("961", "LB"), ("962", "JO"),
        ("963", "SY"), ("964", "IQ"), ("965", "KW"), ("966", "SA"), ("971", "AE"), ("972", "IL"),
        ("973", "BH"), ("974", "QA"), ("975", "BT"), ("976", "MN"), ("977", "NP"), ("992", "TJ"),
        ("993", "TM"), ("994", "AZ"), ("995", "GE"), ("996", "KG"), ("998", "UZ"),
    ]

    static func data(from raw: String) throws -> PhoneData {
        var digits = LoginPhone.normalizedE164(raw)
        if digits.hasPrefix("+") {
            digits = String(digits.dropFirst())
        }
        digits = digits.filter(\.isNumber)
        guard digits.count >= 11 else {
            throw DynamicAuthHTTPError(status: 422, detail: "That phone number looks incomplete.")
        }
        let match = prefixes
            .sorted { $0.code.count > $1.code.count }
            .first { digits.hasPrefix($0.code) }
        let code = match?.code ?? "1"
        let iso2 = match?.iso2 ?? "US"
        let national = String(digits.dropFirst(code.count))
        guard national.count >= 6 else {
            throw DynamicAuthHTTPError(status: 422, detail: "That phone number looks incomplete.")
        }
        return PhoneData(dialCode: "+" + code, iso2: iso2, phone: national)
    }
}
