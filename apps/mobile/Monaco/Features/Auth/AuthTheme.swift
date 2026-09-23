import SwiftUI

extension View {
    /// Same 56pt field shape as `MonacoTextField`, for fields that need an external focus binding
    /// and stable accessibility identifiers — on ink, because the whole pre-auth flow is.
    ///
    /// The fill is `Ink.raised` rather than a well: a field is a control you type into, and on ink
    /// a control reads lighter than the surface it sits on, exactly as `fillQuiet` does on paper.
    func authTextFieldStyle() -> some View {
        font(MonacoTheme.Typo.body)
            // A caret is not a tap target, so it is never the accent. On ink that is white.
            .tint(MonacoTheme.Ink.fgPrimary)
            .padding(.horizontal, MonacoTheme.Space.m)
            .frame(minHeight: 56)
            .background(
                MonacoTheme.Ink.raised,
                in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
                    .strokeBorder(MonacoTheme.Ink.line, lineWidth: 1)
            }
            .foregroundStyle(MonacoTheme.Ink.fgPrimary)
    }

    func authSecondaryCaption() -> some View {
        font(.footnote)
            .foregroundStyle(MonacoTheme.Ink.fgMuted)
    }

    /// **The pre-auth flow is ink in both schemes.** Dark means *this is your money*, and the first
    /// thing Monaco says is that it is a money app; the app then opens into daylight at Home, which
    /// is the moment the two worlds introduce themselves.
    ///
    /// Asking for the dark scheme here is deliberate and is not a colour decision: every shared
    /// control below this point — the button styles, `MonacoTextField`, the toast — resolves its own
    /// adaptive pair, and the dark pair is the one that reads on `Ink.base`. Pinning it means one
    /// screen cannot be left in a light-mode control on a dark surface because someone forgot a
    /// token. It is `preferredColorScheme` rather than an environment write because the **status
    /// bar** is part of this surface: in light mode a dark clock on `#0B1220` is unreadable.
    func authScreenBackground() -> some View {
        monacoWorld(.ink)
            .preferredColorScheme(.dark)
            .background {
                ZStack {
                    MonacoTheme.Ink.base
                    // The same top-left radial every ink surface carries, so the sign-in screen
                    // and Home's fold are recognisably the same material.
                    RadialGradient(
                        colors: [MonacoTheme.Ink.highlight, .clear],
                        center: MonacoInkBandMetrics.highlightCenter,
                        startRadius: 0,
                        endRadius: MonacoInkBandMetrics.highlightRadius
                    )
                }
                .ignoresSafeArea()
            }
    }
}
