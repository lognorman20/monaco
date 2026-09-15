import Combine
import Foundation
import PrivySDK

/// Wraps Privy SDK init and SMS/email OTP login. Access token is for later `POST /v1/auth/session`.
@MainActor
final class PrivyAuthService: ObservableObject {
    enum Phase: Equatable {
        case idle
        case sendingCode
        case awaitingCode
        case verifyingCode
        case authenticated(userID: String)
        case failed(message: String)
    }

    @Published private(set) var phase: Phase = .idle
    @Published private(set) var accessToken: String?

    let privy: Privy

    init(settings: PrivyAuthSettings) {
        let config = PrivyConfig(
            appId: settings.appID,
            appClientId: settings.appClientID,
            loggingConfig: .init(logLevel: .none)
        )
        privy = PrivySdk.initialize(config: config)
    }

    convenience init() {
        self.init(settings: Config.privy)
    }

    func restoreSessionIfNeeded() async {
        guard accessToken == nil else { return }

        if case .authenticated(let user) = await privy.getAuthState() {
            await storeAuthenticatedUser(user)
        }
    }

    func sendSMSCode(to phoneNumberE164: String) async {
        phase = .sendingCode

        do {
            try await privy.sms.sendCode(to: phoneNumberE164)
            phase = .awaitingCode
        } catch {
            phase = .failed(message: "Could not send SMS code.")
        }
    }

    func loginWithSMSCode(_ code: String, sentTo phoneNumberE164: String) async {
        phase = .verifyingCode

        do {
            let user = try await privy.sms.loginWithCode(code, sentTo: phoneNumberE164)
            await storeAuthenticatedUser(user)
        } catch {
            accessToken = nil
            phase = .failed(message: "Invalid code or phone number.")
        }
    }

    func sendEmailCode(to email: String) async {
        phase = .sendingCode

        do {
            try await privy.email.sendCode(to: email)
            phase = .awaitingCode
        } catch {
            phase = .failed(message: "Could not send email code.")
        }
    }

    func loginWithEmailCode(_ code: String, sentTo email: String) async {
        phase = .verifyingCode

        do {
            let user = try await privy.email.loginWithCode(code, sentTo: email)
            await storeAuthenticatedUser(user)
        } catch {
            accessToken = nil
            phase = .failed(message: "Invalid code or email address.")
        }
    }

    func resetLoginFlow() {
        guard case .authenticated = phase else {
            phase = .idle
            return
        }
    }

    func logout() async {
        if let user = await privy.getUser() {
            await user.logout()
        }
        accessToken = nil
        phase = .idle
    }

    private func storeAuthenticatedUser(_ user: PrivyUser) async {
        do {
            let token = try await user.getAccessToken()
            accessToken = token
            phase = .authenticated(userID: user.id)
        } catch {
            accessToken = nil
            phase = .failed(message: "Logged in but could not fetch access token.")
        }
    }
}
