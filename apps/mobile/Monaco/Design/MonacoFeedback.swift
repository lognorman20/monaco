import SwiftUI
import UIKit

/// Haptic vocabulary. `selection` for chips, segments and tabs; `tap` for primary buttons;
/// `success` when something lands (vote, proposal, funding, cash out, comment, message);
/// `warning` on every error toast. Safe no-ops on the simulator and on devices without a Taptic Engine.
enum Haptics {
    static func tap() {
        UIImpactFeedbackGenerator(style: .light).impactOccurred()
    }

    static func success() {
        UINotificationFeedbackGenerator().notificationOccurred(.success)
    }

    static func warning() {
        UINotificationFeedbackGenerator().notificationOccurred(.warning)
    }

    static func selection() {
        UISelectionFeedbackGenerator().selectionChanged()
    }
}

/// Placeholder bar in the shape of the content that is loading.
struct SkeletonBlock: View {
    private let width: CGFloat?
    private let height: CGFloat
    private let radius: CGFloat

    init(width: CGFloat? = nil, height: CGFloat, radius: CGFloat = 8) {
        self.width = width
        self.height = height
        self.radius = radius
    }

    var body: some View {
        RoundedRectangle(cornerRadius: radius, style: .continuous)
            .fill(MonacoTheme.surfaceSunken)
            .frame(width: width, height: height)
            .frame(maxWidth: width == nil ? .infinity : nil, alignment: .leading)
            .modifier(SkeletonPulse(active: true))
            .accessibilityHidden(true)
    }
}

extension View {
    /// Redacts the view to placeholder shapes and pulses it while `active`.
    /// Pair with sample content of the real layout; VoiceOver hears "Loading".
    func skeleton(_ active: Bool) -> some View {
        modifier(SkeletonModifier(active: active))
    }
}

private struct SkeletonModifier: ViewModifier {
    let active: Bool

    func body(content: Content) -> some View {
        if active {
            content
                .redacted(reason: .placeholder)
                .modifier(SkeletonPulse(active: true))
                .allowsHitTesting(false)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Loading")
        } else {
            content
        }
    }
}

/// 1.2s opacity pulse 0.55 ↔ 1. Static under Reduce Motion.
private struct SkeletonPulse: ViewModifier {
    let active: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content.opacityLoop(to: 0.55, halfPeriod: 1.2, active: active && !reduceMotion)
    }
}

extension View {
    /// Loops this view's opacity between 1 and `low`, easing each way over `halfPeriod`, while `active`.
    ///
    /// The loop is a `PhaseAnimator`, so its animation only ever reaches the opacity. Starting a loop with
    /// `withAnimation(.repeatForever)` in `onAppear` instead puts every change of that first update into
    /// the repeating transaction, including layout that is still settling elsewhere on screen (safe-area
    /// insets under a navigation bar, a sheet or the keyboard): sibling views then drift back and forth
    /// between two positions until some later transaction happens to replace the animation.
    @ViewBuilder
    func opacityLoop(to low: Double, halfPeriod: Double, active: Bool = true) -> some View {
        if active {
            phaseAnimator([1.0, low]) { view, opacity in
                view.opacity(opacity)
            } animation: { _ in
                .easeInOut(duration: halfPeriod)
            }
        } else {
            self
        }
    }
}
