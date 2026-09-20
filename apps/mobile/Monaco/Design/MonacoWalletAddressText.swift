import SwiftUI
import UIKit

enum MonacoWalletAddressFormatting {
    /// Character-wrapped, never hyphenated. Addresses are one long "word", and SwiftUI
    /// `Text` hyphenates long words on its own while ignoring UIKit paragraph styles,
    /// so rendering goes through TextKit where these attributes are honored.
    static func paragraphStyle() -> NSParagraphStyle {
        let style = NSMutableParagraphStyle()
        style.hyphenationFactor = 0
        style.lineBreakMode = .byCharWrapping
        return style
    }

    static func attributedString(_ address: String, font: UIFont, color: UIColor) -> NSAttributedString {
        NSAttributedString(
            string: address,
            attributes: [
                .font: font,
                .foregroundColor: color,
                .paragraphStyle: paragraphStyle(),
            ]
        )
    }

    /// Monospaced system font at the Dynamic Type size of `textStyle`.
    static func font(for textStyle: UIFont.TextStyle) -> UIFont {
        let base = UIFont.monospacedSystemFont(
            ofSize: UIFont.preferredFont(forTextStyle: textStyle, compatibleWith: UITraitCollection(preferredContentSizeCategory: .large)).pointSize,
            weight: .regular
        )
        return UIFontMetrics(forTextStyle: textStyle).scaledFont(for: base)
    }
}

/// On-chain address display — wraps by character without auto-inserted hyphens.
struct MonacoWalletAddressText: View {
    let address: String
    var textStyle: UIFont.TextStyle = .body
    var foreground: Color = MonacoTheme.primaryText
    var allowsSelection: Bool = true

    var body: some View {
        WalletAddressTextView(
            address: address,
            textStyle: textStyle,
            color: UIColor(foreground),
            isSelectable: allowsSelection
        )
        .accessibilityElement()
        .accessibilityLabel(address)
        .accessibilityAddTraits(.isStaticText)
    }
}

private struct WalletAddressTextView: UIViewRepresentable {
    let address: String
    let textStyle: UIFont.TextStyle
    let color: UIColor
    let isSelectable: Bool

    func makeUIView(context: Context) -> UITextView {
        let view = UITextView()
        view.isEditable = false
        view.isScrollEnabled = false
        view.backgroundColor = .clear
        view.textContainerInset = .zero
        view.textContainer.lineFragmentPadding = 0
        view.textContainer.lineBreakMode = .byCharWrapping
        view.adjustsFontForContentSizeCategory = true
        view.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        view.setContentHuggingPriority(.defaultHigh, for: .vertical)
        return view
    }

    func updateUIView(_ view: UITextView, context: Context) {
        view.isSelectable = isSelectable
        view.attributedText = MonacoWalletAddressFormatting.attributedString(
            address,
            font: MonacoWalletAddressFormatting.font(for: textStyle),
            color: color
        )
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView: UITextView, context: Context) -> CGSize? {
        let proposedWidth = proposal.width.flatMap { $0.isFinite ? $0 : nil }
        let width = proposedWidth ?? CGFloat.greatestFiniteMagnitude
        let fitted = uiView.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude))
        return CGSize(width: proposedWidth ?? ceil(fitted.width), height: ceil(fitted.height))
    }
}

extension View {
    /// Form field for entering a Base address.
    func monacoWalletAddressField() -> some View {
        font(.body.monospaced())
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
    }
}
