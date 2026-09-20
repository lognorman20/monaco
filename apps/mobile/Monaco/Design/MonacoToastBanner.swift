import SwiftUI

struct MonacoToast: Equatable, Identifiable {
    let id = UUID()
    let message: String
    var isSuccess = false
}

/// Ink panel with paper text and a filled state glyph. One or two short sentences.
///
/// The toast has its own `toast*` tokens. It used to borrow `primaryButton*`, so when the brand
/// accent became electric blue the toast turned into a blue capsule: the green success glyph fell
/// to 1.3:1 on it, and an error toast read as a second primary button above the real one.
struct MonacoToastBanner: View {
    let message: String
    var isSuccess = false

    @Environment(\.colorScheme) private var colorScheme
    @ScaledMetric(relativeTo: .subheadline) private var glyphSize: CGFloat = 17

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            // Filled circle, not a bare tick: the state still reads without relying on hue.
            Image(systemName: isSuccess ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                .font(.system(size: glyphSize, weight: .bold))
                .foregroundStyle(isSuccess ? MonacoTheme.toastSuccessGlyph : MonacoTheme.toastErrorGlyph)
                .accessibilityHidden(true)
            Text(message)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(MonacoTheme.toastLabel)
                .multilineTextAlignment(.leading)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 14)
        .frame(minHeight: 48)
        .background {
            let shape = RoundedRectangle(cornerRadius: MonacoTheme.Radius.chip, style: .continuous)
            shape
                .fill(MonacoTheme.toastFill)
                .overlay { shape.strokeBorder(MonacoTheme.toastStroke, lineWidth: 1) }
        }
        .shadow(color: .black.opacity(colorScheme == .dark ? 0 : 0.08), radius: 16, y: 6)
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("monaco-toast-banner")
    }
}

/// How long a toast stays up.
///
/// A fixed 2.5s is too short for the money-flow failures that go through this path — for example
/// "We couldn't confirm that went through. Check your balance before trying again." — and there is
/// nowhere to read them again.
enum MonacoToastTiming {
    /// Rough reading speed, in seconds per character.
    static let secondsPerCharacter: TimeInterval = 0.06

    static let base: TimeInterval = 2
    static let successFloor: TimeInterval = 2.5
    static let failureFloor: TimeInterval = 4
    static let ceiling: TimeInterval = 10

    /// VoiceOver reads the whole announcement before the user can act on it.
    static let voiceOverFactor: Double = 1.5
    static let voiceOverCeiling: TimeInterval = 15

    static func dwell(message: String, isSuccess: Bool, voiceOverRunning: Bool) -> TimeInterval {
        let reading = base + secondsPerCharacter * Double(message.count)
        let floor = isSuccess ? successFloor : failureFloor
        let dwell = min(max(reading, floor), ceiling)
        guard voiceOverRunning else { return dwell }
        return min(dwell * voiceOverFactor, voiceOverCeiling)
    }
}

/// Where the toast sits above the bottom edge.
enum MonacoToastPlacement: Equatable {
    /// Screens with nothing pinned at the bottom.
    case screenBottom
    /// Screens with a `BottomCTA`. Clears it at every text size.
    case aboveBottomCTA
    /// A caller-measured inset.
    case custom(CGFloat)

    /// `BottomCTA`'s button floor at the default text size.
    static let bottomCTAButtonHeight: CGFloat = 50

    /// `BottomCTA`'s own padding above and below its button.
    static let bottomCTAChrome: CGFloat = MonacoTheme.Space.sm + MonacoTheme.Space.s

    /// Gap between the toast and whatever is under it.
    static let gap: CGFloat = MonacoTheme.Space.sm

    /// `scaledButtonHeight` is `BottomCTA`'s 50pt button floor scaled for the current text size.
    /// A bottom bar grows with Dynamic Type, so an inset sized for one has to grow with it —
    /// otherwise the toast lands on top of the button at accessibility sizes.
    func bottomInset(scaledButtonHeight: CGFloat) -> CGFloat {
        let growth = max(0, scaledButtonHeight - Self.bottomCTAButtonHeight)
        switch self {
        case .screenBottom:
            return Self.gap
        case .aboveBottomCTA:
            return Self.bottomCTAButtonHeight + Self.bottomCTAChrome + growth + Self.gap
        case .custom(let inset):
            return inset >= Self.bottomCTAButtonHeight ? inset + growth : inset
        }
    }
}

private struct MonacoToastModifier: ViewModifier {
    @Binding var toast: MonacoToast?
    let placement: MonacoToastPlacement

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.accessibilityVoiceOverEnabled) private var voiceOverEnabled
    @ScaledMetric(relativeTo: .body) private var ctaButtonHeight: CGFloat = MonacoToastPlacement.bottomCTAButtonHeight
    @State private var dragOffset: CGFloat = 0
    @State private var isDragging = false

    private var bottomInset: CGFloat {
        placement.bottomInset(scaledButtonHeight: ctaButtonHeight)
    }

    private var transitionAnimation: Animation {
        reduceMotion ? .easeInOut(duration: 0.2) : .spring(response: 0.35, dampingFraction: 0.85)
    }

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .bottom) {
                // The animation lives on this container, never on `content`: a toast set in the
                // same update as a balance or a list reload used to spring the whole screen.
                ZStack(alignment: .bottom) {
                    if let toast {
                        banner(toast)
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottom)
                .animation(transitionAnimation, value: toast?.id)
            }
            .onChange(of: toast?.id, initial: true) { _, newID in
                guard newID != nil, let current = toast else { return }
                dragOffset = 0
                isDragging = false
                if !current.isSuccess {
                    Haptics.warning()
                }
                AccessibilityNotification.Announcement(current.message).post()
            }
            .task(id: toast?.id) {
                guard let current = toast else { return }
                await dismiss(current, after: MonacoToastTiming.dwell(
                    message: current.message,
                    isSuccess: current.isSuccess,
                    voiceOverRunning: voiceOverEnabled
                ))
            }
    }

    private func banner(_ toast: MonacoToast) -> some View {
        MonacoToastBanner(message: toast.message, isSuccess: toast.isSuccess)
            .id(toast.id)
            .offset(y: max(dragOffset, 0))
            .gesture(
                DragGesture(minimumDistance: 8)
                    .onChanged {
                        isDragging = true
                        dragOffset = $0.translation.height
                    }
                    .onEnded { value in
                        isDragging = false
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

    /// Counts down in slices so a held toast is not dismissed out from under the user's thumb.
    private func dismiss(_ current: MonacoToast, after dwell: TimeInterval) async {
        let slice: TimeInterval = 0.1
        var remaining = dwell
        while remaining > 0 {
            do {
                try await Task.sleep(for: .seconds(slice))
            } catch {
                return
            }
            if !isDragging {
                remaining -= slice
            }
        }
        if toast?.id == current.id {
            toast = nil
        }
    }
}

extension View {
    /// Bottom toast: swipe down or tap to dismiss, otherwise it dwells for as long as its message
    /// takes to read. Screens with a `BottomCTA` pass `placement: .aboveBottomCTA`.
    func monacoToast(_ toast: Binding<MonacoToast?>, placement: MonacoToastPlacement = .screenBottom) -> some View {
        modifier(MonacoToastModifier(toast: toast, placement: placement))
    }

    /// Caller-measured inset. Prefer `placement: .aboveBottomCTA` on screens with a `BottomCTA`.
    func monacoToast(_ toast: Binding<MonacoToast?>, bottomInset: CGFloat) -> some View {
        monacoToast(toast, placement: .custom(bottomInset))
    }
}
