import SwiftUI

extension View {
    /// `MonacoTextField`'s anatomy, drawn by the same `MonacoFieldChrome`, for the sign-in fields:
    /// they need an external focus binding (the form moves focus from the address to the code)
    /// and identifiers the UI tests sign in by, which `MonacoTextField` does not expose.
    func authTextFieldStyle(isFocused: Bool, isInvalid: Bool = false) -> some View {
        font(MonacoTheme.Typo.body)
            .foregroundStyle(MonacoTheme.ink)
            .tint(MonacoTheme.ink)
            .monacoFieldChrome(isFocused: isFocused, isInvalid: isInvalid)
    }

    func authScreenBackground() -> some View {
        monacoCanvas()
    }
}
