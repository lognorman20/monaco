import SwiftUI
import UIKit

/// 56pt field, no resting stroke; a 1pt stroke in the world's primary foreground while focused.
///
/// The fill is the world's, not a single static: `fillQuiet` on paper, `Ink.raised` inside an ink
/// band — a field is a raised control, and on ink the sign-in flow puts it on the raised card fill
/// rather than in a well. The caret is `controlTint`, never the brand accent: blue means tap, and
/// a caret is not a tap target.
struct MonacoTextField: View {
    private let placeholder: String
    @Binding private var text: String
    private let keyboard: UIKeyboardType
    private let contentType: UITextContentType?

    @FocusState private var focused: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.monacoWorld) private var world
    @Environment(\.monacoPalette) private var palette

    /// A field is a raised control. On paper that is `fillQuiet`; on ink the raised card fill,
    /// because `Ink.sunken` is the well a chart sits in, not the surface a control stands on.
    private var fill: Color {
        world == .ink ? MonacoTheme.Ink.raised : MonacoTheme.fillQuiet
    }

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
            prompt: Text(placeholder).foregroundStyle(palette.fgSubtle)
        )
        .font(MonacoTheme.Typo.body)
        .foregroundStyle(palette.fgPrimary)
        .tint(palette.controlTint)
        .keyboardType(keyboard)
        .textContentType(contentType)
        .textInputAutocapitalization(autocapitalization)
        .autocorrectionDisabled(disablesAutocorrection)
        .focused($focused)
        .padding(.horizontal, MonacoTheme.Space.m)
        .frame(minHeight: 56)
        .background(
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                .fill(fill)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                .strokeBorder(palette.fgPrimary, lineWidth: focused ? 1 : 0)
        }
        .contentShape(Rectangle())
        .onTapGesture { focused = true }
        .animation(MonacoMotion.glide.reduced(reduceMotion), value: focused)
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
