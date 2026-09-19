import SwiftUI
import UIKit

extension View {
    /// Keeps the view tagged `id` in a `ScrollViewReader`'s scroll view fully visible while `isActive`
    /// (usually "this field has focus"): clear of the keyboard and of anything pinned with
    /// `safeAreaInset(edge: .bottom)`, such as `BottomCTA`.
    ///
    /// SwiftUI only nudges a focused field into view once, as focus lands. It does not follow the keyboard
    /// when it changes height (a decimal pad giving way to the full keyboard) or rises after focus, and it
    /// does not follow a multi-line field as it grows, so the field ends up under the pinned button.
    /// This scrolls again on each of those events.
    func revealsWhileActive<ID: Hashable, Value: Equatable>(
        _ id: ID,
        isActive: Bool,
        tracking value: Value,
        proxy: ScrollViewProxy,
        anchor: UnitPoint = .bottom
    ) -> some View {
        modifier(RevealWhileActive(id: id, isActive: isActive, value: value, proxy: proxy, anchor: anchor))
    }
}

private struct RevealWhileActive<ID: Hashable, Value: Equatable>: ViewModifier {
    let id: ID
    let isActive: Bool
    let value: Value
    let proxy: ScrollViewProxy
    let anchor: UnitPoint

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content
            .onChange(of: isActive) { _, active in
                if active { reveal() }
            }
            .onChange(of: value) { _, _ in
                if isActive { reveal() }
            }
            .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardDidChangeFrameNotification)) { _ in
                if isActive { reveal() }
            }
    }

    private func reveal() {
        // Next run loop: the safe area has taken the new keyboard or field height by then.
        DispatchQueue.main.async {
            withAnimation(reduceMotion ? nil : .snappy(duration: 0.25)) {
                proxy.scrollTo(id, anchor: anchor)
            }
        }
    }
}
