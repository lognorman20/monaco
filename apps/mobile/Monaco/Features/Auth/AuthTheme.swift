import SwiftUI

extension View {
    /// Same look as `MonacoTextField` (56pt, sunken fill, field radius), for fields that need
    /// an external focus binding and stable accessibility identifiers.
    func authTextFieldStyle() -> some View {
        font(MonacoTheme.Typo.body)
            .tint(MonacoTheme.ink)
            .padding(.horizontal, MonacoTheme.Space.m)
            .frame(minHeight: 56)
            .background(
                MonacoTheme.surfaceSunken,
                in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
            )
            .foregroundStyle(MonacoTheme.primaryText)
    }

    func authSecondaryCaption() -> some View {
        font(.footnote)
            .foregroundStyle(MonacoTheme.secondaryText)
    }

    func authScreenBackground() -> some View {
        monacoCanvas()
    }
}
