import MonacoCore
import SwiftUI

/// Inline display-name editor: validates as you type with the server's rules and saves
/// through `AppSessionStore.updateDisplayName` (optimistic, rolled back on failure).
struct ProfileNameEditor: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    var onResult: (MonacoToast) -> Void

    @State private var draft: String
    @State private var isSaving = false
    @FocusState private var isFocused: Bool

    init(auth: PrivyAuthService, initialDraft: String? = nil, onResult: @escaping (MonacoToast) -> Void) {
        self.auth = auth
        self.onResult = onResult
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
        MonacoCard {
            DisplayNameField(
                draft: $draft,
                identifierPrefix: "profile-name",
                label: "Display name",
                hint: "Shown on leaderboards and in your cabals.",
                // A name is already set, so emptying the field is a real error here.
                showsValidationWhenEmpty: !savedName.isEmpty,
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
        .onAppear {
            if draft.isEmpty {
                draft = savedName
            }
        }
        .onChange(of: session.me?.userId) { _, _ in
            // Different account: drop the previous user's draft.
            draft = savedName
        }
    }

    private func save() async {
        guard canSave else { return }
        isSaving = true
        isFocused = false
        let outcome = await session.updateDisplayName(draft, auth: auth)
        isSaving = false
        switch outcome {
        case .saved:
            draft = savedName
            onResult(MonacoToast(message: "Name updated.", isSuccess: true))
        case .unchanged:
            break
        case .failed(let message):
            // The store rolled `me` back; keep the draft so the user can fix it.
            onResult(MonacoToast(message: message, isSuccess: false))
        }
    }
}
