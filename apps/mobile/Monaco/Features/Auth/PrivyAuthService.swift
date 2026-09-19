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

    private var sessionStore = MonacoSessionStore()

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
        guard accessToken == nil, sessionStore.hasExplicitLogin else { return }

        if case .authenticated(let user) = await privy.getAuthState() {
            await storeAuthenticatedUser(user, markExplicitLogin: false)
        }
    }

    func shouldInvalidateBackendSession(serverUserId: String) -> Bool {
        sessionStore.shouldInvalidateSession(serverUserId: serverUserId)
    }

    func recordBackendSession(userId: String) {
        sessionStore.recordSession(userId: userId)
    }

    func sendSMSCode(to phoneNumberE164: String) async {
        phase = .sendingCode

        do {
            try await privy.sms.sendCode(to: phoneNumberE164)
            phase = .awaitingCode
        } catch {
            phase = .failed(message: privySendCodeErrorMessage("Could not send SMS code.", error: error))
        }
    }

    func loginWithSMSCode(_ code: String, sentTo phoneNumberE164: String) async {
        phase = .verifyingCode

        do {
            let user = try await privy.sms.loginWithCode(code, sentTo: phoneNumberE164)
            await storeAuthenticatedUser(user, markExplicitLogin: true)
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
            phase = .failed(message: privySendCodeErrorMessage("Could not send email code.", error: error))
        }
    }

    func loginWithEmailCode(_ code: String, sentTo email: String) async {
        phase = .verifyingCode

        do {
            let user = try await privy.email.loginWithCode(code, sentTo: email)
            await storeAuthenticatedUser(user, markExplicitLogin: true)
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
        sessionStore.clear()
    }

    private func privySendCodeErrorMessage(_ fallback: String, error: Error) -> String {
        let detail = error.localizedDescription.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !detail.isEmpty else { return fallback }
        return "\(fallback) \(detail)"
    }

    private func storeAuthenticatedUser(_ user: PrivyUser, markExplicitLogin: Bool) async {
        do {
            let token = try await user.getAccessToken()
            accessToken = token
            phase = .authenticated(userID: user.id)
            if markExplicitLogin {
                sessionStore.markExplicitLogin()
            }
            #if DEBUG
            exportDebugTokens(accessToken: token, user: user)
            await ensureServerSweepSigner(for: user)
            #endif
        } catch {
            accessToken = nil
            phase = .failed(message: "Logged in but could not fetch access token.")
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
