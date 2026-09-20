import SwiftUI
import UIKit

/// 56pt field on `surfaceSunken`, no resting stroke; a 1pt ink stroke while focused.
struct MonacoTextField: View {
    private let placeholder: String
    @Binding private var text: String
    private let keyboard: UIKeyboardType
    private let contentType: UITextContentType?

    @FocusState private var focused: Bool

    init(
        _ placeholder: String,
        text: Binding<String>,
        keyboard: UIKeyboardType = .default,
        contentType: UITextContentType? = nil
    ) {
        self.placeholder = placeholder
        _text = text
        self.keyboard = keyboard
        self.contentType = contentType
    }

    var body: some View {
        TextField(
            "",
            text: $text,
            prompt: Text(placeholder).foregroundStyle(MonacoTheme.tertiaryText)
        )
        .font(MonacoTheme.Typo.body)
        .foregroundStyle(MonacoTheme.ink)
        .tint(MonacoTheme.ink)
        .keyboardType(keyboard)
        .textContentType(contentType)
        .textInputAutocapitalization(autocapitalization)
        .autocorrectionDisabled(disablesAutocorrection)
        .focused($focused)
        .padding(.horizontal, MonacoTheme.Space.m)
        .frame(minHeight: 56)
        .background(
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                .fill(MonacoTheme.surfaceSunken)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                .strokeBorder(MonacoTheme.ink, lineWidth: focused ? 1 : 0)
        }
        .contentShape(Rectangle())
        .onTapGesture { focused = true }
        .animation(.easeOut(duration: 0.15), value: focused)
        .accessibilityLabel(placeholder)
    }

    private var isCodeOrContact: Bool {
        switch keyboard {
        case .emailAddress, .numberPad, .phonePad, .URL, .asciiCapableNumberPad, .decimalPad: return true
        default: return contentType == .oneTimeCode || contentType == .emailAddress
        }
    }

    private var autocapitalization: TextInputAutocapitalization {
        isCodeOrContact ? .never : .sentences
    }

    private var disablesAutocorrection: Bool {
        isCodeOrContact
    }
}
