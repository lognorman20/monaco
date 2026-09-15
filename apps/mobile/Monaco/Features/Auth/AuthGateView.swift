import SwiftUI

struct AuthGateView: View {
    @ObservedObject var auth: PrivyAuthService

    var body: some View {
        Group {
            if Config.privy.isConfigured {
                if hasLoginMethod {
                    LoginView(auth: auth)
                } else {
                    missingLoginMethodsView
                }
            } else {
                missingConfigView
            }
        }
        .task {
            await auth.restoreSessionIfNeeded()
        }
    }

    private var hasLoginMethod: Bool {
        Config.privy.smsLoginEnabled || Config.privy.emailLoginEnabled
    }

    private var missingConfigView: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label("Privy not configured", systemImage: "key.fill")
                .font(.headline)

            Text("Set PRIVY_APP_ID and PRIVY_APP_CLIENT_ID in your Xcode scheme or shell env. Copy values from `.env.example`.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    private var missingLoginMethodsView: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label("No login methods enabled", systemImage: "person.crop.circle.badge.exclamationmark")
                .font(.headline)

            Text("Enable PRIVY_SMS_LOGIN_ENABLED and/or PRIVY_EMAIL_LOGIN_ENABLED, or turn SMS/email on in Privy dashboard Login Methods.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }
}

#Preview {
    AuthGateView(auth: PrivyAuthService())
}
