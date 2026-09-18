import SwiftUI

/// First-login username + product tour before Home.
struct OnboardingFlowView: View {
    @ObservedObject var auth: PrivyAuthService
    let initialProfile: MeResponse
    var onComplete: () async -> Void

    private let apiClient = MonacoAPIClient()

    @State private var step: OnboardingStep
    @State private var username = ""
    @State private var savedProfile: MeResponse
    @State private var usernameError: String?
    @State private var isSavingUsername = false
    @State private var sessionStore = MonacoSessionStore()

    private enum OnboardingStep: Int {
        case username = 1
        case whatIsMonaco = 2
        case clubs = 3
        case deposits = 4
        case proposals = 5
    }

    init(
        auth: PrivyAuthService,
        initialProfile: MeResponse,
        onComplete: @escaping () async -> Void
    ) {
        self.auth = auth
        self.initialProfile = initialProfile
        self.onComplete = onComplete
        _savedProfile = State(initialValue: initialProfile)
        let hasUsername = !initialProfile.displayName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        _step = State(initialValue: hasUsername ? .whatIsMonaco : .username)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                stepContent
            }
            .padding(.vertical, 24)
        }
        .authScreenBackground()
        .tint(MonacoTheme.accent)
        .foregroundStyle(MonacoTheme.primaryText)
    }

    @ViewBuilder
    private var stepContent: some View {
        switch step {
        case .username:
            usernameStep
        case .whatIsMonaco:
            tourStep(
                title: "Welcome to Monaco",
                body: "Monaco helps groups invest together in tokenized stocks. Pool money with friends, vote on trades, and track performance on leaderboards.",
                stepIdentifier: "onboarding-step-2",
                isFinal: false
            )
        case .clubs:
            tourStep(
                title: "Cabals and groups",
                body: "Create or join a cabal to share a treasury. Each group has its own pot, member board, and rules for who can vote on proposals.",
                stepIdentifier: "onboarding-step-3",
                isFinal: false
            )
        case .deposits:
            tourStep(
                title: "Add money",
                body: "Send USDC to your personal deposit address in the app. The backend sweeps funds into the group vault so everyone’s balance stays in sync.",
                stepIdentifier: "onboarding-step-4",
                isFinal: false
            )
        case .proposals:
            tourStep(
                title: "Proposals and votes",
                body: "Members propose stock buys with treasury USDC. The group votes; approved trades execute on-chain and show up in transaction history.",
                stepIdentifier: "onboarding-step-5",
                isFinal: true
            )
        }
    }

    private var usernameStep: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Choose a username")
                .font(.title2.bold())
                .foregroundStyle(MonacoTheme.primaryText)

            Text("This name appears on leaderboards and in your cabals.")
                .authSecondaryCaption()

            TextField(
                "",
                text: $username,
                prompt: Text("Username").foregroundStyle(MonacoTheme.disabled)
            )
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .authTextFieldStyle()
            .accessibilityIdentifier("onboarding-username-field")

            if let usernameError {
                Text(usernameError)
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.destructive)
            }

            Button("Continue") {
                Task { await saveUsername() }
            }
            .buttonStyle(.monacoPrimary)
            .disabled(isContinueDisabled)
            .accessibilityIdentifier("onboarding-username-continue-button")
        }
        .padding(.horizontal)
    }

    private func tourStep(title: String, body: String, stepIdentifier: String, isFinal: Bool) -> some View {
        VStack(alignment: .leading, spacing: 20) {
            Text(title)
                .font(.title2.bold())
                .foregroundStyle(MonacoTheme.primaryText)

            Text(body)
                .authSecondaryCaption()
                .fixedSize(horizontal: false, vertical: true)

            if isFinal {
                Button("Get started") {
                    Task { await finishOnboarding() }
                }
                .buttonStyle(.monacoPrimary)
                .accessibilityIdentifier("onboarding-get-started-button")
            } else {
                Button("Continue") {
                    advanceTour()
                }
                .buttonStyle(.monacoPrimary)
            }

            Button("Skip") {
                Task { await finishOnboarding() }
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("onboarding-skip-button")
        }
        .padding(.horizontal)
        .accessibilityIdentifier(stepIdentifier)
    }

    private var trimmedUsername: String {
        username.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var isContinueDisabled: Bool {
        trimmedUsername.isEmpty || isSavingUsername
    }

    private func saveUsername() async {
        guard let accessToken = auth.accessToken else {
            usernameError = "Missing sign-in token."
            return
        }

        isSavingUsername = true
        usernameError = nil
        defer { isSavingUsername = false }

        do {
            let profile = try await apiClient.updateProfile(accessToken: accessToken, displayName: trimmedUsername)
            savedProfile = profile
            step = .whatIsMonaco
        } catch MonacoAPIError.httpStatus(let status) where status == 400 {
            usernameError = "Username must be 1–32 characters."
        } catch MonacoAPIError.httpStatus(let status) {
            usernameError = "Could not save username (HTTP \(status))."
        } catch {
            usernameError = "Could not save username. Try again."
        }
    }

    private func advanceTour() {
        switch step {
        case .whatIsMonaco:
            step = .clubs
        case .clubs:
            step = .deposits
        case .deposits:
            step = .proposals
        case .username, .proposals:
            break
        }
    }

    private func finishOnboarding() async {
        sessionStore.markOnboardingCompleted(for: savedProfile.userId)
        await onComplete()
    }
}

#Preview {
    OnboardingFlowView(
        auth: PrivyAuthService(),
        initialProfile: MeResponse(userId: "preview", displayName: "", memberWalletAddress: ""),
        onComplete: {}
    )
}
