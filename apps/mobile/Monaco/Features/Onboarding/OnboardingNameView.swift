import MonacoCore
import SwiftUI

/// First run, after sign-in and before the tabs: the one screen that asks a new account
/// what to call it.
///
/// Monaco is a social product — proposals, votes, leaderboards and chat all carry a name —
/// and an account without one renders as "Member" everywhere, so there is no Skip. It is
/// not a trap either: "Sign out" is always on screen.
///
/// The screen owns no data. `SessionGateView` hands it the real save and sign-out; the
/// debug harness hands it canned ones, which is why QA can screenshot it without a backend.
struct OnboardingNameView: View {
    @ObservedObject var auth: DynamicAuthService

    /// Persists the name. Returning `.saved` is what lets `FirstRunGate` move on, so it
    /// must only succeed once the server has the name.
    var save: (String) async -> ProfileSaveOutcome
    var signOut: () async -> Void
    /// Debug harness only: open with the field already filled (to shoot validation).
    var initialDraft: String = ""

    @State private var draft: String = ""
    @State private var isSaving = false
    /// Why the last Continue failed. Stays under the field until the draft changes or
    /// Continue is tried again, so a retry is one tap and the reason is never a vanished toast.
    @State private var saveError: String?
    @State private var toast: MonacoToast?
    @FocusState private var isFocused: Bool

    private var normalizedDraft: String? {
        try? DisplayNameRules.normalize(draft).get()
    }

    private var canContinue: Bool {
        !isSaving && normalizedDraft != nil
    }

    var body: some View {
        MonacoScreen {
            ScrollView {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                    title
                    photo
                    DisplayNameField(
                        draft: $draft,
                        identifierPrefix: "onboarding-name",
                        placeholder: "Your name",
                        hint: "You can change this later in Profile.",
                        saveError: saveError,
                        focus: $isFocused,
                        onSubmit: { Task { await submit() } }
                    )
                }
                .padding(.horizontal, MonacoTheme.Space.gutter)
                .padding(.top, MonacoTheme.Space.xl)
                .padding(.bottom, MonacoTheme.Space.l)
            }
            .scrollBounceBehavior(.basedOnSize)
            .scrollDismissesKeyboard(.interactively)
        }
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                VStack(spacing: MonacoTheme.Space.sm) {
                    Button {
                        Task { await submit() }
                    } label: {
                        if isSaving {
                            ProgressView().tint(MonacoTheme.primaryButtonLabel)
                        } else {
                            Text("Continue")
                        }
                    }
                    .buttonStyle(.monacoPrimary)
                    .disabled(!canContinue)
                    .accessibilityIdentifier("onboarding-continue")

                    Button("Sign out") {
                        Task { await signOut() }
                    }
                    .font(MonacoTheme.TypeRole.caption.weight(.semibold))
                    .foregroundStyle(MonacoTheme.muted)
                    .frame(minHeight: 44)
                    .accessibilityIdentifier("onboarding-sign-out")
                }
            }
        }
        .monacoToast($toast, bottomInset: 108)
        .onChange(of: draft) { _, _ in
            saveError = nil
        }
        .onAppear {
            if draft.isEmpty, !initialDraft.isEmpty {
                draft = initialDraft
            }
            isFocused = true
        }
    }

    private var title: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("What should friends call you?")
                .font(MonacoTheme.Typo.display)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
            Text("Shown on votes, leaderboards and in chat.")
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private var photo: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            ProfilePhotoPicker(
                auth: auth,
                size: 108,
                accessibilityID: "onboarding-photo"
            ) { toast = $0 }

            Text("Add a photo (optional)")
                .monacoSecondaryCaption()
        }
        .frame(maxWidth: .infinity)
    }

    private func submit() async {
        // Also the double-tap guard: `isSaving` flips before the first suspension point.
        guard canContinue else { return }
        isSaving = true
        saveError = nil
        isFocused = false
        let outcome = await save(draft)
        isSaving = false

        switch outcome {
        case .saved, .unchanged:
            // The gate is already switching to the tabs behind this screen.
            Haptics.success()
        case .failed(let message):
            saveError = message
            Haptics.warning()
            isFocused = true
        }
    }
}
