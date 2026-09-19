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
    @State private var dimmed = false

    func body(content: Content) -> some View {
        content
            .opacity(active && !reduceMotion && dimmed ? 0.55 : 1)
            .onAppear {
                guard active, !reduceMotion else { return }
                withAnimation(.easeInOut(duration: 1.2).repeatForever(autoreverses: true)) {
                    dimmed = true
                }
            }
    }
}
