import SwiftUI

extension View {
    func authTextFieldStyle() -> some View {
        padding(.horizontal, 12)
            .padding(.vertical, 12)
            .frame(minHeight: 44)
            .background(MonacoTheme.surface)
            .foregroundStyle(MonacoTheme.primaryText)
            .clipShape(RoundedRectangle(cornerRadius: 10))
            .overlay {
                RoundedRectangle(cornerRadius: 10)
                    .stroke(MonacoTheme.border, lineWidth: 1.5)
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
