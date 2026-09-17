import SwiftUI

enum MonacoWalletAddressFormatting {
    /// Attributed string that wraps without auto-inserted hyphens.
    static func attributedString(_ address: String) -> AttributedString {
        var paragraphStyle = NSMutableParagraphStyle()
        paragraphStyle.hyphenationFactor = 0
        paragraphStyle.lineBreakMode = .byCharWrapping
        paragraphStyle.lineBreakStrategy = .pushOut

        var container = AttributeContainer()
        container.uiKit.paragraphStyle = paragraphStyle

        return AttributedString(address, attributes: container)
    }
}

/// On-chain address display — wraps without auto-inserted hyphens.
struct MonacoWalletAddressText: View {
    let address: String
    var font: Font = .body.monospaced()
    var foreground: Color = MonacoTheme.primaryText
    var allowsSelection: Bool = true

    var body: some View {
        if allowsSelection {
            styledText.textSelection(.enabled)
        } else {
            styledText
        }
    }

    private var styledText: some View {
        Text(MonacoWalletAddressFormatting.attributedString(address))
            .font(font)
            .foregroundStyle(foreground)
            .multilineTextAlignment(.leading)
            .fixedSize(horizontal: false, vertical: true)
    }
}

extension View {
    /// Form field for entering a Solana wallet address.
    func monacoWalletAddressField() -> some View {
        font(.body.monospaced())
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
    }
}
