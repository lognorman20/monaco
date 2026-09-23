import SwiftUI

/// Shared press feel: scale 0.97 on `MonacoMotion.snap`, opacity 0.80 and no animation under
/// Reduce Motion. The curve used to be a hand-rolled `.spring(0.25, 0.8)` that had already
/// drifted from the segmented thumb's `.spring(0.3, 0.85)` — the same "a control responded to my
/// finger" moment, two curves. Both are `snap` now.
private struct MonacoPressEffect: ViewModifier {
    let isPressed: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content
            .scaleEffect(isPressed && !reduceMotion ? 0.97 : 1)
            .opacity(isPressed && reduceMotion ? 0.8 : 1)
            .animation(MonacoMotion.snap.reduced(reduceMotion), value: isPressed)
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
                    .fill(isEnabled ? MonacoTheme.primaryButtonFill : MonacoTheme.fillQuiet)
            )
            .contentShape(Capsule())
            .modifier(MonacoPressEffect(isPressed: configuration.isPressed))
    }
}

/// The outlined capsule. Reads `\.monacoWorld`, so a secondary action inside an ink band is an
/// ink capsule rather than a white one: on paper the palette resolves to exactly the statics this
/// used to name (`raised` is `secondaryButtonFill`, `fgPrimary` is `secondaryButtonLabel`), so
/// nothing moves there — it just stops being paper-only.
struct MonacoSecondaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled
    @Environment(\.monacoPalette) private var palette

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .monacoButtonLabel()
            .foregroundStyle(isEnabled ? palette.fgPrimary : MonacoTheme.disabledLabel)
            .background(Capsule().fill(palette.raised))
            .overlay {
                Capsule()
                    .strokeBorder(palette.line, lineWidth: 1)
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
            .background(Capsule().fill(MonacoTheme.bgRaised))
            .overlay {
                Capsule()
                    .strokeBorder(isEnabled ? MonacoTheme.destructive.opacity(0.5) : MonacoTheme.line, lineWidth: 1)
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

/// The app's one pinned-bar treatment: flat canvas, a 1pt top rule, a light-mode shadow, and
/// `Capsule()` buttons at `MonacoButtonMetrics.minimumHeight`. There is no material anywhere in
/// Monaco — a blur behind a money figure is decoration the loudness budget does not have room for.
///
/// Use inside `.safeAreaInset(edge: .bottom) { BottomCTA { … } }` so it rides above the keyboard.
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
            MonacoTheme.bgBase
                .ignoresSafeArea(edges: .bottom)
                .shadow(color: .black.opacity(colorScheme == .dark ? 0 : 0.08), radius: 16, y: -2)
        }
        .overlay(alignment: .top) {
            Rectangle()
                .fill(MonacoTheme.line)
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
@available(*, deprecated, message: "Deleted with CircleAction. Last site: GroupDetailView.swift GroupActionRow (Chunk D).")
enum CircleActionMetrics {
    /// Fits the ~97pt column the four-across row gives each action on the narrowest phone.
    static let maximumDiscSize: CGFloat = 88

    /// Keeps the symbol proportionate inside a capped disc (20/56 of the disc, as at the base size).
    static let maximumGlyphSize: CGFloat = 30

    static func discSize(scaled: CGFloat) -> CGFloat { min(scaled, maximumDiscSize) }

    static func glyphSize(scaled: CGFloat) -> CGFloat { min(scaled, maximumGlyphSize) }
}

/// Brand-tinted disc with a footnote label below.
///
/// **On the way out.** Four identical brand-wash circles reading `plus` / `arrow.up.right` /
/// `arrow.down.left` / `bubble.left` is the most recognisable fintech-template component shipping,
/// and desaturating them made them generic rather than fixing them. The replacement states the
/// hierarchy instead: one full-width brand capsule for the product's core verb ("Propose a buy"),
/// then two text-and-glyph chips on `fillQuiet`. Chunk B has removed its own two call sites (the
/// gallery and `SampleProposalFeedService`); the type goes when Chunk D removes the last one.
@available(*, deprecated, message: "Use a labelled capsule plus fillQuiet chips. Last site: GroupDetailView.swift:791 (Chunk D).")
struct CircleAction: View {
    private let title: String
    private let systemImage: String
    private let action: () -> Void

    @Environment(\.isEnabled) private var isEnabled
    @ScaledMetric(relativeTo: .footnote) private var scaledDiscSize: CGFloat = 56
    @ScaledMetric(relativeTo: .footnote) private var scaledGlyphSize: CGFloat = 20

    // The ceilings live in `CircleActionMetrics` and are called, not inlined: `DesignLayoutTests`
    // tests the enum, so an inlined constant would let the ceiling change under a green suite (or
    // the suite change without touching the shipped disc). Referencing a deprecated declaration
    // from inside a deprecated one raises no warning.
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
                    .foregroundStyle(isEnabled ? MonacoTheme.brandOnWash : MonacoTheme.disabledLabel)
                    .frame(width: discSize, height: discSize)
                    .background(Circle().fill(isEnabled ? MonacoTheme.brandWash : MonacoTheme.fillQuiet))
                Text(title)
                    .font(.system(.footnote, weight: .medium))
                    .foregroundStyle(isEnabled ? MonacoTheme.fgPrimary : MonacoTheme.disabledLabel)
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
