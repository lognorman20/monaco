import SwiftUI

struct MonacoToast: Equatable, Identifiable {
    let id = UUID()
    let message: String
    var isSuccess = false
}

/// Ink capsule with paper text. Short sentence, no trailing period.
struct MonacoToastBanner: View {
    let message: String
    var isSuccess = false

    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: isSuccess ? "checkmark" : "exclamationmark")
                .font(.system(size: 16, weight: .bold))
                .foregroundStyle(isSuccess ? MonacoToastPalette.successGlyph : MonacoTheme.primaryButtonLabel)
                .frame(width: 20)
                .accessibilityHidden(true)
            Text(message)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(MonacoTheme.primaryButtonLabel)
                .multilineTextAlignment(.leading)
                .lineLimit(3)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.leading, 16)
        .padding(.trailing, 20)
        .padding(.vertical, 14)
        .frame(minHeight: 48)
        .background(Capsule().fill(MonacoTheme.primaryButtonFill))
        .shadow(color: .black.opacity(colorScheme == .dark ? 0 : 0.08), radius: 16, y: 6)
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("monaco-toast-banner")
    }
}

private enum MonacoToastPalette {
    /// Profit green that stays legible on the ink capsule in both modes.
    static let successGlyph = MonacoTheme.profit
}

private struct MonacoToastModifier: ViewModifier {
    @Binding var toast: MonacoToast?
    let bottomInset: CGFloat

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var dragOffset: CGFloat = 0

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .bottom) {
                if let toast {
                    MonacoToastBanner(message: toast.message, isSuccess: toast.isSuccess)
                        .id(toast.id)
                        .offset(y: max(dragOffset, 0))
                        .gesture(
                            DragGesture(minimumDistance: 8)
                                .onChanged { dragOffset = $0.translation.height }
                                .onEnded { value in
                                    if value.translation.height > 24 || value.predictedEndTranslation.height > 60 {
                                        self.toast = nil
                                    }
                                    withAnimation(.snappy) { dragOffset = 0 }
                                }
                        )
                        .onTapGesture { self.toast = nil }
                        .transition(
                            reduceMotion
                                ? .opacity
                                : .move(edge: .bottom).combined(with: .opacity)
                        )
                        .padding(.bottom, bottomInset)
                        .zIndex(1)
                }
            }
            .animation(reduceMotion ? .easeInOut(duration: 0.2) : .spring(response: 0.35, dampingFraction: 0.85), value: toast?.id)
            .onChange(of: toast?.id) { _, newID in
                guard let newID, let current = toast else { return }
                dragOffset = 0
                if !current.isSuccess {
                    Haptics.warning()
                }
                AccessibilityNotification.Announcement(current.message).post()
                Task { @MainActor in
                    try? await Task.sleep(for: .seconds(2.5))
                    if toast?.id == newID {
                        toast = nil
                    }
                }
            }
    }
}

extension View {
    /// Bottom toast, 2.5s, swipe down or tap to dismiss. Screens with a `BottomCTA` pass `bottomInset: 72`.
    func monacoToast(_ toast: Binding<MonacoToast?>, bottomInset: CGFloat = 12) -> some View {
        modifier(MonacoToastModifier(toast: toast, bottomInset: bottomInset))
    }
}
