import MonacoCore
import SwiftUI

/// A stand-in for `AppSessionStore.updateDisplayName`, so the debug harnesses can drive this
/// screen against a store that answers. An existential rather than an `async` closure on
/// purpose: a closure handed down through two view layers is reabstracted at every hop.
protocol DisplayNameSaving {
    func saveDisplayName(_ draft: String) async -> ProfileSaveOutcome
}

/// Inline display-name editor: validates as you type with the server's rules and saves
/// through `AppSessionStore.updateDisplayName` (optimistic, rolled back on failure).
///
/// The editor is presented in a sheet, so it reports failures in its own error slot rather
/// than through the host screen's toast: an overlay on the presenter renders *behind* the
/// sheet, where nobody can read it. Success is handed to `onSaved`, which closes the sheet
/// first and only then toasts on the now-uncovered screen.
struct ProfileNameEditor: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    /// Called once the server has stored the new name. The host closes the sheet.
    var onSaved: () -> Void
    /// Stands in for the store call. Nil in the app; the debug harnesses use it to exercise
    /// this screen against a backend that answers.
    var saveName: (any DisplayNameSaving)?

    @State private var draft: String
    @State private var isSaving = false
    @State private var saveError: String?
    @FocusState private var isFocused: Bool

    init(
        auth: PrivyAuthService,
        initialDraft: String? = nil,
        saveName: (any DisplayNameSaving)? = nil,
        onSaved: @escaping () -> Void
    ) {
        self.auth = auth
        self.saveName = saveName
        self.onSaved = onSaved
        _draft = State(initialValue: initialDraft ?? "")
    }

    private var savedName: String {
        session.me?.displayName ?? ""
    }

    private var normalizedDraft: String? {
        try? DisplayNameRules.normalize(draft).get()
    }

    private var canSave: Bool {
        guard !isSaving, let normalizedDraft else { return false }
        return normalizedDraft != savedName
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            DisplayNameField(
                draft: $draft,
                identifierPrefix: "profile-name",
                label: "Display name",
                hint: "Shown on leaderboards and in your cabals.",
                // A name is already set, so emptying the field is a real error here.
                showsValidationWhenEmpty: !savedName.isEmpty,
                saveError: saveError,
                focus: $isFocused,
                onSubmit: { Task { await save() } }
            ) {
                Button {
                    Task { await save() }
                } label: {
                    if isSaving {
                        ProgressView().tint(MonacoTheme.primaryButtonLabel)
                    } else {
                        Text("Save")
                    }
                }
                .buttonStyle(.monacoPrimary)
                .fixedSize()
                .disabled(!canSave)
                .accessibilityIdentifier("profile-name-save")
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.top, MonacoTheme.Space.s)
        .onAppear {
            if draft.isEmpty {
                draft = savedName
            }
        }
        .onChange(of: draft) { _, _ in
            // Editing is the retry: the last rejection no longer describes what is typed.
            saveError = nil
        }
        .onChange(of: session.me?.userId) { _, _ in
            // Different account: drop the previous user's draft.
            draft = savedName
            saveError = nil
        }
    }

    private func save() async {
        guard canSave else { return }
        isSaving = true
        isFocused = false
        saveError = nil
        let outcome: ProfileSaveOutcome
        if let saveName {
            outcome = await saveName.saveDisplayName(draft)
        } else {
            outcome = await session.updateDisplayName(draft, auth: auth)
        }
        isSaving = false
        switch outcome {
        case .saved:
            draft = savedName
            onSaved()
        case .unchanged:
            break
        case .failed(let message):
            // The store rolled `me` back; keep the draft so the user can fix it.
            // The toast used to carry the haptic and the announcement, so raise them here.
            saveError = message
            Haptics.warning()
            AccessibilityNotification.Announcement(message).post()
        }
    }
}
