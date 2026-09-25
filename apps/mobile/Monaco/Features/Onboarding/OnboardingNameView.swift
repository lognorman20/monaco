import MonacoCore
import SwiftUI

/// First run, after sign-in and before the tabs: the one screen that asks a new account what
/// to call it, and the first place a new member meets the product.
///
/// Monaco is a social product — proposals, votes, leaderboards and chat all carry a name —
/// and an account without one renders as "Member" everywhere, so there is no Skip. It is
/// not a trap either: "Sign out" is always on screen.
///
/// Under the name, three ruled lines say what happens after it. The field does not take
/// focus on arrival, so those lines are read before the keyboard covers them; once it has
/// focus, the field and its caption stay clear of the keyboard and the button.
///
/// The screen owns no data. `SessionGateView` hands it the real save and sign-out; the
/// debug harness hands it canned ones, which is why QA can screenshot it without a backend.
struct OnboardingNameView: View {
    @ObservedObject var auth: PrivyAuthService

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

    /// The name field's block: what stays in view while it has focus.
    private static let fieldAnchor = "onboarding-name"

    private var normalizedDraft: String? {
        try? DisplayNameRules.normalize(draft).get()
    }

    private var canContinue: Bool {
        !isSaving && normalizedDraft != nil
    }

    /// A spinner has no words, so the button says what it is doing while the name saves.
    private var continueLabel: String {
        isSaving ? "Saving your name" : "Continue"
    }

    /// The field's caption changes height when an error comes or goes, and has to be
    /// revealed again when it does.
    private var revealKey: String {
        [DisplayNameRules.validationMessage(for: draft), saveError]
            .compactMap { $0 }
            .joined(separator: "|")
    }

    var body: some View {
        MonacoScreen {
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                        title
                            .padding(.horizontal, MonacoTheme.Space.gutter)
                        photo
                            .padding(.top, MonacoTheme.Space.l)
                        DisplayNameField(
                            draft: $draft,
                            identifierPrefix: "onboarding-name",
                            placeholder: "Your name",
                            hint: "You can change this later in Profile.",
                            saveError: saveError,
                            focus: $isFocused,
                            onSubmit: { Task { await submit() } }
                        )
                        .padding(.horizontal, MonacoTheme.Space.gutter)
                        .padding(.top, MonacoTheme.Space.l)
                        .id(Self.fieldAnchor)
                        FirstRunNextSteps()
                            .padding(.top, MonacoTheme.Space.xl)
                    }
                    .padding(.top, MonacoTheme.Space.m)
                    .padding(.bottom, MonacoTheme.Space.l)
                }
                .scrollBounceBehavior(.basedOnSize)
                .scrollDismissesKeyboard(.interactively)
                .revealsWhileActive(Self.fieldAnchor, isActive: isFocused, tracking: revealKey, proxy: proxy)
            }
        }
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                VStack(spacing: MonacoTheme.Space.sm) {
                    Button {
                        Task { await submit() }
                    } label: {
                        if isSaving {
                            // The button is disabled while the name saves. A cream spinner, the
                            // enabled label's colour, vanished into the disabled fill.
                            ProgressView().tint(MonacoTheme.muted)
                        } else {
                            Text("Continue")
                        }
                    }
                    .buttonStyle(.monacoPrimary)
                    .disabled(!canContinue)
                    .accessibilityLabel(continueLabel)
                    .accessibilityIdentifier("onboarding-continue")

                    Button {
                        Task { await signOut() }
                    } label: {
                        Text("Sign out")
                            .font(MonacoTheme.Typo.calloutStrong)
                            .foregroundStyle(MonacoTheme.brand)
                            .frame(minHeight: 44)
                            .padding(.horizontal, MonacoTheme.Space.m)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
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
                size: 96,
                accessibilityID: "onboarding-photo"
            ) { toast = $0 }

            Text("Add a photo (optional)")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
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

/// The first-run screen's words for what comes after the name.
enum FirstRunCopy {
    static let nextStepsTitle = "What happens next"
    static let nextSteps = [
        "Start a cabal or join one",
        "Add money to the pot",
        "Vote on every buy",
    ]
}

/// What happens after the name, in the ledger's voice: three numbered lines between rules.
///
/// First run is where a new member meets the product, so it says what Monaco is the way the
/// rest of the app would: a numbered table on the paper, the numbers in the market's mono, no
/// pictures.
struct FirstRunNextSteps: View {
    /// One digit of `Typo.data`, as a column, so every line's text starts in the same place.
    @ScaledMetric(relativeTo: .subheadline) private var numberColumn: CGFloat = 12

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text(FirstRunCopy.nextStepsTitle)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .accessibilityAddTraits(.isHeader)
                .padding(.horizontal, MonacoTheme.Space.gutter)

            MonacoGroupedList {
                ForEach(Array(FirstRunCopy.nextSteps.enumerated()), id: \.offset) { index, step in
                    row(
                        number: index + 1,
                        text: step,
                        isLast: index == FirstRunCopy.nextSteps.count - 1
                    )
                }
            }
        }
    }

    private func row(number: Int, text: String, isLast: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.sm) {
            Text(verbatim: String(number))
                .font(MonacoTheme.Typo.data)
                .foregroundStyle(MonacoTheme.muted)
                .frame(width: numberColumn, alignment: .leading)
            Text(text)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .padding(.vertical, MonacoTheme.Space.sm)
        .frame(minHeight: 44)
        .overlay(alignment: .bottom) {
            if !isLast {
                // Under the words, as a row's rule runs under its text.
                MonacoRule()
                    .padding(.leading, MonacoTheme.Space.gutter + numberColumn + MonacoTheme.Space.sm)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Step \(number): \(text)")
    }
}
