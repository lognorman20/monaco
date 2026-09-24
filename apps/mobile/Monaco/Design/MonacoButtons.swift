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

/// Shared button geometry. `MonacoToastPlacement` sizes its inset against this, so the bar height
/// and the toast's idea of the bar height cannot drift apart.
enum MonacoButtonMetrics {
    /// Tap-target floor for every Monaco button capsule.
    static let minimumHeight: CGFloat = 50
}

private struct MonacoButtonLabel: ViewModifier {
    @Environment(\.monacoButtonFullWidth) private var fullWidth

    func body(content: Content) -> some View {
        content
            .font(MonacoTheme.Typo.body.weight(.semibold))
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .padding(.horizontal, 20)
            .frame(maxWidth: fullWidth ? .infinity : nil, minHeight: MonacoButtonMetrics.minimumHeight)
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
            .foregroundStyle(isEnabled ? MonacoTheme.primaryButtonLabel : MonacoTheme.disabledLabel)
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
            .foregroundStyle(isEnabled ? MonacoTheme.secondaryButtonLabel : MonacoTheme.disabledLabel)
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
            .foregroundStyle(isEnabled ? MonacoTheme.destructive : MonacoTheme.disabledLabel)
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

/// Ceilings for `CircleAction`'s disc and glyph.
///
/// The disc is a container, not type, and it shares a row with three others: `GroupActionRow` puts
/// four `CircleAction`s in an `HStack(spacing: 0)` with each in a `.frame(maxWidth: .infinity)`
/// column — about 97pt on a 390pt screen. Scaled without a ceiling, a 56pt disc reaches ~99pt at
/// AX1 and ~189pt at AX5, so the four main money actions draw over each other from AX1 upwards.
/// A 2x2 grid would only raise the column to ~195pt, so it does not remove the need for a ceiling.
///
/// The glyph keeps scaling up to its own ceiling, so the control still grows with Dynamic Type —
/// it just stops growing before it outgrows the space it has. Past the ceiling the label below,
/// which is uncapped, carries the rest of the size increase.
enum CircleActionMetrics {
    /// Fits the ~97pt column the four-across row gives each action on the narrowest phone.
    static let maximumDiscSize: CGFloat = 88

    /// Keeps the symbol proportionate inside a capped disc (20/56 of the disc, as at the base size).
    static let maximumGlyphSize: CGFloat = 30

    static func discSize(scaled: CGFloat) -> CGFloat { min(scaled, maximumDiscSize) }

    static func glyphSize(scaled: CGFloat) -> CGFloat { min(scaled, maximumGlyphSize) }
}

/// Brand-tinted disc with a symbol and a footnote label below (Group detail action row).
/// A wash rather than a solid fill: four solid brand discs in a row would spend the accent.
/// The disc, the glyph and the label all scale with Dynamic Type — these are the main money
/// actions, and they used to stay at 13pt while every label around them grew. The disc and the
/// glyph stop at `CircleActionMetrics`' ceilings so they stay inside their column; the label does not.
struct CircleAction: View {
    private let title: String
    private let systemImage: String
    private let action: () -> Void

    @Environment(\.isEnabled) private var isEnabled
    @ScaledMetric(relativeTo: .footnote) private var scaledDiscSize: CGFloat = 56
    @ScaledMetric(relativeTo: .footnote) private var scaledGlyphSize: CGFloat = 20

    private var discSize: CGFloat { CircleActionMetrics.discSize(scaled: scaledDiscSize) }

    private var glyphSize: CGFloat { CircleActionMetrics.glyphSize(scaled: scaledGlyphSize) }

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
                    .font(.system(size: glyphSize, weight: .semibold))
                    // Filled, not washed. The brand is monochrome now, so a low-alpha brand tint
                    // over cream has no colour left in it — the disc came out a flat sage-grey and
                    // the row read as four disabled controls. A solid disc carries the affordance
                    // the way the saturated blue glyph used to.
                    .foregroundStyle(isEnabled ? MonacoTheme.onBrand : MonacoTheme.disabledLabel)
                    .frame(width: discSize, height: discSize)
                    .background(Circle().fill(isEnabled ? MonacoTheme.brandFill : MonacoTheme.surfaceSunken))
                Text(title)
                    .font(.system(.footnote, weight: .medium))
                    .foregroundStyle(isEnabled ? MonacoTheme.ink : MonacoTheme.disabledLabel)
                    .lineLimit(2)
                    .multilineTextAlignment(.center)
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
