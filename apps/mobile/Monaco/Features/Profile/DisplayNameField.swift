import MonacoCore
import SwiftUI

/// The one display-name input: an optional label with a live character counter, the
/// field itself, and underneath it either the rule the draft breaks or a hint.
///
/// `ProfileNameEditor` (Profile → Edit profile) and `OnboardingNameView` (first run)
/// both build on this, so the validation copy, the counter and the field can't drift
/// apart between the two places a name is set. The field is `MonacoFieldChrome`, the
/// anatomy every other field in the app has.
///
/// `identifierPrefix` names the three elements for UI tests: `<prefix>-field`,
/// `<prefix>-count` and `<prefix>-error`.
struct DisplayNameField<Trailing: View>: View {
    @Binding var draft: String
    let identifierPrefix: String
    var label: String?
    var placeholder: String = "Name shown on boards"
    var hint: String
    /// Show the rule violation even while the draft is empty. Profile passes true once a
    /// name is already saved (clearing the field is a real error there); first run passes
    /// false, so an untouched screen doesn't open with "Display name is required."
    var showsValidationWhenEmpty: Bool = false
    /// A save the server turned down (offline, rate limited, rejected). Shown in the error
    /// slot whenever the draft itself is valid, so the reason sits next to the field.
    var saveError: String?
    var focus: FocusState<Bool>.Binding
    var onSubmit: () -> Void = {}
    /// Sits to the right of the field. `ProfileNameEditor` puts its Save button here.
    @ViewBuilder var trailing: () -> Trailing

    /// Counted the way the server counts: trimmed, in unicode scalars.
    private var characterCount: Int {
        draft.trimmingCharacters(in: .whitespacesAndNewlines).unicodeScalars.count
    }

    private var validationMessage: String? {
        DisplayNameRules.validationMessage(for: draft)
    }

    private var showsValidation: Bool {
        validationMessage != nil && (!draft.isEmpty || showsValidationWhenEmpty)
    }

    /// The broken rule wins: it is the thing the user can fix by typing.
    private var errorMessage: String? {
        showsValidation ? validationMessage : saveError
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            HStack {
                if let label {
                    Text(label)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                }
                Spacer()
                Text("\(characterCount)/\(DisplayNameRules.maxLength)")
                    .font(MonacoTheme.Typo.stamp)
                    .foregroundStyle(
                        characterCount > DisplayNameRules.maxLength
                            ? MonacoTheme.loss
                            : MonacoTheme.muted
                    )
                    .accessibilityIdentifier("\(identifierPrefix)-count")
            }

            HStack(spacing: MonacoTheme.Space.s) {
                TextField(
                    placeholder,
                    text: $draft,
                    prompt: Text(placeholder).foregroundStyle(MonacoTheme.disabledLabel)
                )
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.ink)
                .tint(MonacoTheme.ink)
                .textInputAutocapitalization(.words)
                .autocorrectionDisabled()
                .submitLabel(.done)
                .focused(focus)
                .onSubmit(onSubmit)
                .monacoFieldChrome(isFocused: focus.wrappedValue, isInvalid: errorMessage != nil)
                .accessibilityIdentifier("\(identifierPrefix)-field")

                trailing()
            }

            if let errorMessage {
                Text(errorMessage)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.loss)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("\(identifierPrefix)-error")
            } else {
                Text(hint)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .animation(.easeInOut(duration: 0.15), value: errorMessage)
    }
}

extension DisplayNameField where Trailing == EmptyView {
    init(
        draft: Binding<String>,
        identifierPrefix: String,
        label: String? = nil,
        placeholder: String = "Name shown on boards",
        hint: String,
        showsValidationWhenEmpty: Bool = false,
        saveError: String? = nil,
        focus: FocusState<Bool>.Binding,
        onSubmit: @escaping () -> Void = {}
    ) {
        self.init(
            draft: draft,
            identifierPrefix: identifierPrefix,
            label: label,
            placeholder: placeholder,
            hint: hint,
            showsValidationWhenEmpty: showsValidationWhenEmpty,
            saveError: saveError,
            focus: focus,
            onSubmit: onSubmit,
            trailing: { EmptyView() }
        )
    }
}
