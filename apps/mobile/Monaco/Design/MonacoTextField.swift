import SwiftUI
import UIKit

/// The one field anatomy: 56pt on `surfaceSunken` with the field radius, no stroke at rest, a
/// 1pt ink stroke while focused, and a 1pt `loss` stroke while what is typed can't be used.
///
/// `MonacoTextField` draws itself with it. Fields that need their own focus binding or stable
/// accessibility identifiers (the sign-in form, the display-name field) apply it directly, so the
/// three cannot drift apart again: the name field had become a white box with a hairline and a
/// 2pt brand ring while every other field was sunken paper.
struct MonacoFieldChrome: ViewModifier {
    let isFocused: Bool
    var isInvalid = false

    static let height: CGFloat = 56

    private var shape: RoundedRectangle {
        RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
    }

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, MonacoTheme.Space.m)
            .frame(minHeight: Self.height)
            .background(shape.fill(MonacoTheme.surfaceSunken))
            .overlay {
                shape.strokeBorder(
                    isInvalid ? MonacoTheme.loss : MonacoTheme.ink,
                    lineWidth: isInvalid || isFocused ? 1 : 0
                )
            }
            .animation(.easeOut(duration: 0.15), value: isFocused)
            .animation(.easeOut(duration: 0.15), value: isInvalid)
    }
}

extension View {
    /// Draws this text field as every Monaco field is drawn. See `MonacoFieldChrome`.
    func monacoFieldChrome(isFocused: Bool, isInvalid: Bool = false) -> some View {
        modifier(MonacoFieldChrome(isFocused: isFocused, isInvalid: isInvalid))
    }
}

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
            prompt: Text(placeholder).foregroundStyle(MonacoTheme.disabledLabel)
        )
        .font(MonacoTheme.Typo.body)
        .foregroundStyle(MonacoTheme.ink)
        .tint(MonacoTheme.ink)
        .keyboardType(keyboard)
        .textContentType(contentType)
        .textInputAutocapitalization(autocapitalization)
        .autocorrectionDisabled(disablesAutocorrection)
        .focused($focused)
        .monacoFieldChrome(isFocused: focused)
        // The whole 56pt is the target, not just the line of text in the middle of it.
        .contentShape(Rectangle())
        .onTapGesture { focused = true }
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

/// A field for a Solana address: monospace, wraps by character, never hyphenates, takes no
/// newline. `TextField(axis: .vertical)` hyphenates a long word where it wraps, and an address
/// must never carry a hyphen (see `MonacoWalletAddressText`), so this is a `UITextView` told to
/// break lines by character. Drawn with `MonacoFieldChrome` like every other field.
struct MonacoAddressField: View {
    let placeholder: String
    @Binding var text: String
    var isInvalid = false
    /// Set on the text view itself: a modifier on the wrapper does not reach the element the UI
    /// tests type into.
    var accessibilityIdentifier: String?

    @State private var isFocused = false
    @State private var focusRequests = 0

    var body: some View {
        ZStack(alignment: .leading) {
            if text.isEmpty {
                Text(placeholder)
                    .font(MonacoTheme.Typo.body)
                    .foregroundStyle(MonacoTheme.disabledLabel)
                    .accessibilityHidden(true)
            }
            CharacterWrappingTextView(
                text: $text,
                isFocused: $isFocused,
                focusRequests: focusRequests,
                placeholder: placeholder,
                accessibilityIdentifier: accessibilityIdentifier
            )
        }
        .padding(.vertical, MonacoTheme.Space.sm)
        .monacoFieldChrome(isFocused: isFocused, isInvalid: isInvalid)
        // The whole field is the target, not just the line of text in the middle of it.
        .contentShape(Rectangle())
        .onTapGesture { focusRequests += 1 }
    }
}

private struct CharacterWrappingTextView: UIViewRepresentable {
    @Binding var text: String
    @Binding var isFocused: Bool
    let focusRequests: Int
    let placeholder: String
    let accessibilityIdentifier: String?

    /// `Typo.data`: 15pt mono, medium, scaled with the subheadline style.
    private static var font: UIFont {
        UIFontMetrics(forTextStyle: .subheadline)
            .scaledFont(for: .monospacedSystemFont(ofSize: 15, weight: .medium))
    }

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIView(context: Context) -> UITextView {
        let view = UITextView()
        view.delegate = context.coordinator
        view.backgroundColor = .clear
        view.isScrollEnabled = false
        view.textContainerInset = .zero
        view.textContainer.lineFragmentPadding = 0
        view.textContainer.lineBreakMode = .byCharWrapping
        view.keyboardType = .asciiCapable
        view.returnKeyType = .done
        view.autocorrectionType = .no
        view.autocapitalizationType = .none
        view.spellCheckingType = .no
        // A smart dash would turn a typed hyphen into an en dash inside an address.
        view.smartDashesType = .no
        view.smartQuotesType = .no
        view.smartInsertDeleteType = .no
        view.adjustsFontForContentSizeCategory = true
        view.font = Self.font
        view.textColor = UIColor(MonacoTheme.ink)
        view.tintColor = UIColor(MonacoTheme.ink)
        view.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        view.setContentHuggingPriority(.required, for: .vertical)
        view.accessibilityLabel = placeholder
        view.accessibilityIdentifier = accessibilityIdentifier
        return view
    }

    func updateUIView(_ view: UITextView, context: Context) {
        context.coordinator.parent = self
        if view.text != text {
            view.text = text
        }
        if focusRequests != context.coordinator.focusRequests {
            context.coordinator.focusRequests = focusRequests
            view.becomeFirstResponder()
        }
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView: UITextView, context: Context) -> CGSize? {
        let proposed = proposal.width ?? uiView.bounds.width
        let width = proposed.isFinite && proposed > 0 ? proposed : 320
        let fitted = uiView.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude))
        return CGSize(width: width, height: fitted.height)
    }

    final class Coordinator: NSObject, UITextViewDelegate {
        var parent: CharacterWrappingTextView
        var focusRequests = 0

        init(_ parent: CharacterWrappingTextView) {
            self.parent = parent
        }

        func textViewDidChange(_ view: UITextView) {
            parent.text = view.text
        }

        func textViewDidBeginEditing(_ view: UITextView) {
            parent.isFocused = true
        }

        func textViewDidEndEditing(_ view: UITextView) {
            parent.isFocused = false
        }

        /// Return is Done: an address has no second line.
        func textView(_ view: UITextView, shouldChangeTextIn range: NSRange, replacementText text: String) -> Bool {
            guard text.contains("\n") else { return true }
            view.resignFirstResponder()
            return false
        }
    }
}
