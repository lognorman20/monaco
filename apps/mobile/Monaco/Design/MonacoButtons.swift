import SwiftUI

/// Shared press feel: scale 0.97 on a short spring, none under Reduce Motion.
private struct MonacoPressEffect: ViewModifier {
    let isPressed: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content
            .scaleEffect(isPressed && !reduceMotion ? 0.97 : 1)
            .opacity(isPressed && reduceMotion ? 0.8 : 1)
            .animation(reduceMotion ? nil : .spring(response: 0.25, dampingFraction: 0.8), value: isPressed)
    }
}

private struct MonacoButtonFullWidthKey: EnvironmentKey {
    static let defaultValue = false
}

extension EnvironmentValues {
    /// When true, Monaco button styles stretch their capsule to the available width.
    var monacoButtonFullWidth: Bool {
        get { self[MonacoButtonFullWidthKey.self] }
        set { self[MonacoButtonFullWidthKey.self] = newValue }
    }
}

extension View {
    /// Stretches `.monacoPrimary` / `.monacoSecondary` / `.monacoDestructive` capsules to full width.
    func monacoFullWidthButtons(_ enabled: Bool = true) -> some View {
        environment(\.monacoButtonFullWidth, enabled)
    }
}

private struct MonacoButtonLabel: ViewModifier {
    @Environment(\.monacoButtonFullWidth) private var fullWidth

    func body(content: Content) -> some View {
        content
            .font(MonacoTheme.Typo.body.weight(.semibold))
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .padding(.horizontal, 20)
            .frame(maxWidth: fullWidth ? .infinity : nil, minHeight: 50)
    }
}

private extension View {
    func monacoButtonLabel() -> some View {
        modifier(MonacoButtonLabel())
    }
}

struct MonacoPrimaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .monacoButtonLabel()
            .foregroundStyle(isEnabled ? MonacoTheme.primaryButtonLabel : MonacoTheme.tertiaryText)
            .background(
                Capsule()
                    .fill(isEnabled ? MonacoTheme.primaryButtonFill : MonacoTheme.surfaceSunken)
            )
            .contentShape(Capsule())
            .modifier(MonacoPressEffect(isPressed: configuration.isPressed))
    }
}

struct MonacoSecondaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .monacoButtonLabel()
            .foregroundStyle(isEnabled ? MonacoTheme.secondaryButtonLabel : MonacoTheme.tertiaryText)
            .background(Capsule().fill(MonacoTheme.secondaryButtonFill))
            .overlay {
                Capsule()
                    .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
            }
            .contentShape(Capsule())
            .modifier(MonacoPressEffect(isPressed: configuration.isPressed))
    }
}

struct MonacoDestructiveButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .monacoButtonLabel()
            .foregroundStyle(isEnabled ? MonacoTheme.destructive : MonacoTheme.tertiaryText)
            .background(Capsule().fill(MonacoTheme.surface))
            .overlay {
                Capsule()
                    .strokeBorder(isEnabled ? MonacoTheme.destructive.opacity(0.5) : MonacoTheme.hairline, lineWidth: 1)
            }
            .contentShape(Capsule())
            .modifier(MonacoPressEffect(isPressed: configuration.isPressed))
    }
}

extension ButtonStyle where Self == MonacoPrimaryButtonStyle {
    static var monacoPrimary: MonacoPrimaryButtonStyle { MonacoPrimaryButtonStyle() }
}

extension ButtonStyle where Self == MonacoSecondaryButtonStyle {
    static var monacoSecondary: MonacoSecondaryButtonStyle { MonacoSecondaryButtonStyle() }
}

extension ButtonStyle where Self == MonacoDestructiveButtonStyle {
    static var monacoDestructive: MonacoDestructiveButtonStyle { MonacoDestructiveButtonStyle() }
}

/// Pinned action bar. Use inside `.safeAreaInset(edge: .bottom) { BottomCTA { … } }` so it rides above the keyboard.
/// Buttons inside get the full width; put one primary, or a primary and a secondary side by side.
struct BottomCTA<Content: View>: View {
    private let content: Content
    @Environment(\.colorScheme) private var colorScheme

    init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    var body: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            content
        }
        .monacoFullWidthButtons()
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .padding(.top, MonacoTheme.Space.sm)
        .padding(.bottom, MonacoTheme.Space.s)
        .background {
            MonacoTheme.canvas
                .ignoresSafeArea(edges: .bottom)
                .shadow(color: .black.opacity(colorScheme == .dark ? 0 : 0.08), radius: 16, y: -2)
        }
        .overlay(alignment: .top) {
            Rectangle()
                .fill(MonacoTheme.hairline)
                .frame(height: 1)
        }
    }
}

/// 56pt ink circle with a symbol and a 13pt label below (Group detail action row).
struct CircleAction: View {
    private let title: String
    private let systemImage: String
    private let action: () -> Void

    @Environment(\.isEnabled) private var isEnabled

    init(_ title: String, systemImage: String, action: @escaping () -> Void) {
        self.title = title
        self.systemImage = systemImage
        self.action = action
    }

    var body: some View {
        Button {
            Haptics.tap()
            action()
        } label: {
            VStack(spacing: MonacoTheme.Space.s) {
                Image(systemName: systemImage)
                    .font(.system(size: 20, weight: .semibold))
                    .foregroundStyle(isEnabled ? MonacoTheme.primaryButtonLabel : MonacoTheme.tertiaryText)
                    .frame(width: 56, height: 56)
                    .background(Circle().fill(isEnabled ? MonacoTheme.primaryButtonFill : MonacoTheme.surfaceSunken))
                Text(title)
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(isEnabled ? MonacoTheme.ink : MonacoTheme.tertiaryText)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
            }
            .frame(minWidth: 64)
            .contentShape(Rectangle())
        }
        .buttonStyle(CircleActionPressStyle())
        .accessibilityLabel(title)
    }
}

private struct CircleActionPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .modifier(MonacoPressEffect(isPressed: configuration.isPressed))
    }
}
