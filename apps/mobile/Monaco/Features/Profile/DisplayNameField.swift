import MonacoCore
import SwiftUI

/// The one display-name input: an optional label with a live character counter, the
/// field itself, and underneath it either the rule the draft breaks or a hint.
///
/// `ProfileNameEditor` (Profile → Edit profile) and `OnboardingNameView` (first run)
/// both build on this, so the validation copy, the counter and the focus ring can't
/// drift apart between the two places a name is set.
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
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.muted)
                }
                Spacer()
                Text("\(characterCount)/\(DisplayNameRules.maxLength)")
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(
                        characterCount > DisplayNameRules.maxLength
                            ? MonacoTheme.destructive
                            : MonacoTheme.muted
                    )
                    .accessibilityIdentifier("\(identifierPrefix)-count")
            }

            HStack(spacing: MonacoTheme.Space.s) {
                TextField(placeholder, text: $draft)
                    .font(MonacoTheme.TypeRole.body)
                    .foregroundStyle(MonacoTheme.ink)
                    .textInputAutocapitalization(.words)
                    .autocorrectionDisabled()
                    .submitLabel(.done)
                    .focused(focus)
                    .onSubmit(onSubmit)
                    .padding(.horizontal, MonacoTheme.Space.m)
                    .padding(.vertical, 12)
                    .background(
                        MonacoTheme.surface,
                        in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                    )
                    .overlay {
                        RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                            .strokeBorder(borderColor, lineWidth: focus.wrappedValue ? 2 : 1)
                    }
                    .accessibilityIdentifier("\(identifierPrefix)-field")

                trailing()
            }

            if let errorMessage {
                Text(errorMessage)
                    .font(MonacoTheme.TypeRole.caption)
                    .foregroundStyle(MonacoTheme.destructive)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("\(identifierPrefix)-error")
            } else {
                Text(hint)
                    .monacoSecondaryCaption()
            }
        }
        .animation(.easeInOut(duration: 0.15), value: errorMessage)
    }

    private var borderColor: Color {
        if errorMessage != nil {
            return MonacoTheme.destructive.opacity(0.6)
        }
        return focus.wrappedValue ? MonacoTheme.brand : MonacoTheme.hairline
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
