import SwiftUI

extension View {
    func authTextFieldStyle() -> some View {
        padding(.horizontal, 12)
            .padding(.vertical, 12)
            .frame(minHeight: 44)
            .background(MonacoTheme.surface)
            .foregroundStyle(MonacoTheme.primaryText)
            .clipShape(RoundedRectangle(cornerRadius: 18))
            .overlay {
                RoundedRectangle(cornerRadius: 18)
                    .stroke(MonacoTheme.border, lineWidth: 0.8)
            }
    }

    func authSecondaryCaption() -> some View {
        font(.footnote)
            .foregroundStyle(MonacoTheme.secondaryText)
    }

    func authScreenBackground() -> some View {
        background(MonacoTheme.background)
    }
}
