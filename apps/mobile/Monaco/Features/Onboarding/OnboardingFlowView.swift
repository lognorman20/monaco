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
            .frame(maxWidth: 560, alignment: .leading)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 32)
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
                title: "Invest with your people",
                body: "Pool money in a cabal, vote on stock buys, and see how your returns compare with other cabals.",
                stepIdentifier: "onboarding-step-2",
                isFinal: false
            )
        case .clubs:
            tourStep(
                title: "Find your cabal",
                body: "Create a cabal with friends or join one from the Cabals tab. Each cabal has a shared pot and its own voting rules.",
                stepIdentifier: "onboarding-step-3",
                isFinal: false
            )
        case .deposits:
            tourStep(
                title: "Add money",
                body: "Open Add money in your cabal and send USDC to your deposit address. Once the deposit reaches the pot, your slice appears in the cabal.",
                stepIdentifier: "onboarding-step-4",
                isFinal: false
            )
        case .proposals:
            tourStep(
                title: "Make the call together",
                body: "Propose a stock buy and follow the vote. When a proposal passes, Monaco places the trade. Track the result in your cabal’s holdings and activity.",
                stepIdentifier: "onboarding-step-5",
                isFinal: true
            )
        }
    }

    private var usernameStep: some View {
        VStack(alignment: .leading, spacing: 24) {
            onboardingProgress

            Text("Choose a username")
                .font(.largeTitle.bold())
                .accessibilityAddTraits(.isHeader)
                .foregroundStyle(MonacoTheme.primaryText)

            Text("This name appears on leaderboards and in your cabals.")
                .font(.body)
                .foregroundStyle(MonacoTheme.secondaryText)

            TextField(
                "Username",
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

            Button {
                Task { await saveUsername() }
            } label: {
                Text("Continue")
                    .frame(maxWidth: .infinity, minHeight: 28)
            }
            .buttonStyle(.monacoPrimary)
            .disabled(isContinueDisabled)
            .accessibilityIdentifier("onboarding-username-continue-button")
        }
        .padding(.horizontal, 24)
    }

    private func tourStep(title: String, body: String, stepIdentifier: String, isFinal: Bool) -> some View {
        VStack(alignment: .leading, spacing: 24) {
            onboardingProgress

            Text(title)
                .font(.largeTitle.bold())
                .accessibilityAddTraits(.isHeader)
                .foregroundStyle(MonacoTheme.primaryText)

            Text(body)
                .font(.body)
                .foregroundStyle(MonacoTheme.secondaryText)
                .fixedSize(horizontal: false, vertical: true)

            if isFinal {
                Button {
                    Task { await finishOnboarding() }
                } label: {
                    Text("Open Monaco")
                        .frame(maxWidth: .infinity, minHeight: 28)
                }
                .buttonStyle(.monacoPrimary)
                .accessibilityIdentifier("onboarding-get-started-button")
            } else {
                Button {
                    advanceTour()
                } label: {
                    Text("Continue")
                        .frame(maxWidth: .infinity, minHeight: 28)
                }
                .buttonStyle(.monacoPrimary)
            }

            Button {
                Task { await finishOnboarding() }
            } label: {
                Text("Skip tour")
                    .frame(maxWidth: .infinity, minHeight: 28)
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("onboarding-skip-button")
        }
        .padding(.horizontal, 24)
        .accessibilityIdentifier(stepIdentifier)
    }

    private var onboardingProgress: some View {
        HStack {
            Text("MONACO")
                .font(.caption.weight(.bold))
                .tracking(2)
                .foregroundStyle(MonacoTheme.accent)
            Spacer()
            Text("\(step.rawValue) of 5")
                .font(.caption.monospacedDigit())
                .foregroundStyle(MonacoTheme.secondaryText)
                .accessibilityLabel("Step \(step.rawValue) of 5")
        }
        .padding(.bottom, 16)
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
