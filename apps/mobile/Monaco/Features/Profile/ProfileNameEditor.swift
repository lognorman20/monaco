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

    private var validationMessage: String? {
        DisplayNameRules.validationMessage(for: draft)
    }

    private var normalizedDraft: String? {
        try? DisplayNameRules.normalize(draft).get()
    }

    private var canSave: Bool {
        guard !isSaving, let normalizedDraft else { return false }
        return normalizedDraft != savedName
    }

    private var characterCount: Int {
        draft.trimmingCharacters(in: .whitespacesAndNewlines).unicodeScalars.count
    }

    var body: some View {
        MonacoCard {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                HStack {
                    Text("Display name")
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    Spacer()
                    Text("\(characterCount)/\(DisplayNameRules.maxLength)")
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(characterCount > DisplayNameRules.maxLength ? MonacoTheme.destructive : MonacoTheme.muted)
                        .accessibilityIdentifier("profile-name-count")
                }

                HStack(spacing: MonacoTheme.Space.s) {
                    TextField("Name shown on boards", text: $draft)
                        .font(MonacoTheme.TypeRole.body)
                        .foregroundStyle(MonacoTheme.ink)
                        .textInputAutocapitalization(.words)
                        .autocorrectionDisabled()
                        .submitLabel(.done)
                        .focused($isFocused)
                        .onSubmit { Task { await save() } }
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .padding(.vertical, 12)
                        .background(MonacoTheme.canvas, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                        .overlay {
                            RoundedRectangle(cornerRadius: 14, style: .continuous)
                                .strokeBorder(
                                    validationMessage == nil ? MonacoTheme.hairline : MonacoTheme.destructive.opacity(0.6),
                                    lineWidth: 1
                                )
                        }
                        .accessibilityIdentifier("profile-name-field")

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

                if let validationMessage, !draft.isEmpty || !savedName.isEmpty {
                    Text(validationMessage)
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.destructive)
                        .accessibilityIdentifier("profile-name-error")
                } else {
                    Text("Shown on leaderboards and in your cabals.")
                        .monacoSecondaryCaption()
                }
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
