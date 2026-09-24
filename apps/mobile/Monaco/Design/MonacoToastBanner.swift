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

    /// The longest a drag may hold the countdown open past the toast's own dwell.
    ///
    /// The hold is driven by a gesture flag, and SwiftUI does not deliver `onEnded` when a gesture
    /// is cancelled rather than completed — the banner torn down mid-drag, the screen pushed away,
    /// a system edge gesture winning. Without a ceiling the flag stays set, the countdown never
    /// decrements, and the toast pins itself over a money screen with tap as the only way out.
    static let maximumHold: TimeInterval = 2.5

    static func dwell(message: String, isSuccess: Bool, voiceOverRunning: Bool) -> TimeInterval {
        let reading = base + secondsPerCharacter * Double(message.count)
        let floor = isSuccess ? successFloor : failureFloor
        let dwell = min(max(reading, floor), ceiling)
        guard voiceOverRunning else { return dwell }
        return min(dwell * voiceOverFactor, voiceOverCeiling)
    }
}

/// The dwell countdown, as state rather than as a loop, so the hold and its ceiling are testable
/// without a view or a running clock.
///
/// A drag pauses the countdown so a toast is not dismissed out from under a moving thumb, but the
/// pause is bounded by wall clock: `elapsed` keeps running while `isHeld` is true, and once the
/// toast has been up for `dwell + maximumHold` it goes regardless. That bound is what a flag alone
/// cannot give — see `MonacoToastTiming.maximumHold`.
struct MonacoToastCountdown {
    private(set) var remaining: TimeInterval
    private let limit: TimeInterval
    private var elapsed: TimeInterval = 0

    init(dwell: TimeInterval, maximumHold: TimeInterval = MonacoToastTiming.maximumHold) {
        remaining = dwell
        limit = dwell + maximumHold
    }

    /// True once the toast should come down: either the countdown ran out or the hold ceiling did.
    var isFinished: Bool { remaining <= 0 || elapsed >= limit }

    /// Advances by one slice. A held toast keeps its `remaining` but still spends wall clock.
    mutating func tick(slice: TimeInterval, isHeld: Bool) {
        elapsed += slice
        guard !isHeld else { return }
        remaining -= slice
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

    /// `BottomCTA`'s button floor, taken from the button itself rather than restated here.
    static let bottomCTAButtonHeight = MonacoButtonMetrics.minimumHeight

    /// One line of `.body` at the default text size — `BottomCTA`'s button is a single such line
    /// (17pt at roughly 1.2 line height) inside the `minimumHeight` frame.
    static let bottomCTALabelLineHeight: CGFloat = 21

    /// `BottomCTA`'s own padding above and below its button.
    static let bottomCTAChrome: CGFloat = MonacoTheme.Space.sm + MonacoTheme.Space.s

    /// Gap between the toast and whatever is under it.
    static let gap: CGFloat = MonacoTheme.Space.sm

    /// At or above this, a caller-measured inset is assumed to have been sized for a bottom bar.
    static let callerInsetBarThreshold = bottomCTAButtonHeight

    /// `scaledLabelLineHeight` is `bottomCTALabelLineHeight` scaled for the current text size.
    ///
    /// The bar grows by its *label*, not in proportion to the button floor: the button is
    /// `max(floor, one line of body)`, so it does not move at all until the line outgrows the
    /// floor, and then it grows a line-height at a time. Scaling the 50pt floor instead put the
    /// toast roughly 100pt above the bar at AX5 — the floor is a minimum, not a base to multiply.
    func bottomInset(scaledLabelLineHeight: CGFloat) -> CGFloat {
        let button = max(Self.bottomCTAButtonHeight, scaledLabelLineHeight)
        let growth = button - Self.bottomCTAButtonHeight
        switch self {
        case .screenBottom:
            return Self.gap
        case .aboveBottomCTA:
            return button + Self.bottomCTAChrome + Self.gap
        case .custom(let inset):
            // A heuristic standing in for a measurement until #398 lands: it reads "sized for a
            // bar" off the magnitude of the number, so it cannot tell a taller bar from a shorter
            // one — OnboardingNameView's 108 and WithdrawView's 72 both get the same growth.
            return inset >= Self.callerInsetBarThreshold ? inset + growth : inset
        }
    }
}

private struct MonacoToastModifier: ViewModifier {
    @Binding var toast: MonacoToast?
    let placement: MonacoToastPlacement

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.accessibilityVoiceOverEnabled) private var voiceOverEnabled
    @ScaledMetric(relativeTo: .body)
    private var labelLineHeight: CGFloat = MonacoToastPlacement.bottomCTALabelLineHeight
    @State private var dragOffset: CGFloat = 0
    @State private var isDragging = false

    private var bottomInset: CGFloat {
        placement.bottomInset(scaledLabelLineHeight: labelLineHeight)
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
                // A drag that was cancelled rather than ended leaves this set; a new toast must not
                // inherit it, or it starts its life already held.
                isDragging = false
                await dismiss(current, after: MonacoToastTiming.dwell(
                    message: current.message,
                    isSuccess: current.isSuccess,
                    voiceOverRunning: voiceOverEnabled
                ))
            }
            .onDisappear { isDragging = false }
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

    /// Counts down in slices so a toast is not dismissed out from under a dragging thumb, and never
    /// past `MonacoToastTiming.maximumHold` beyond its dwell even if the drag never reports an end.
    private func dismiss(_ current: MonacoToast, after dwell: TimeInterval) async {
        let slice: TimeInterval = 0.1
        var countdown = MonacoToastCountdown(dwell: dwell)
        while !countdown.isFinished {
            do {
                try await Task.sleep(for: .seconds(slice))
            } catch {
                return
            }
            countdown.tick(slice: slice, isHeld: isDragging)
        }
        if toast?.id == current.id {
            toast = nil
        }
    }
}

extension View {
    /// Bottom toast: swipe down or tap to dismiss, otherwise it dwells for as long as its message
    /// takes to read. Screens with a `BottomCTA` pass `placement: .aboveBottomCTA`.
    ///
    /// A drag in progress pauses the countdown, so a toast is not pulled out from under a moving
    /// thumb. A finger resting on the toast without moving does not pause it: the dismiss gesture
    /// only begins after 8pt of travel, so there is nothing to tell a still finger from no finger.
    func monacoToast(_ toast: Binding<MonacoToast?>, placement: MonacoToastPlacement = .screenBottom) -> some View {
        modifier(MonacoToastModifier(toast: toast, placement: placement))
    }

    /// Caller-measured inset. Prefer `placement: .aboveBottomCTA` on screens with a `BottomCTA`.
    func monacoToast(_ toast: Binding<MonacoToast?>, bottomInset: CGFloat) -> some View {
        monacoToast(toast, placement: .custom(bottomInset))
    }
}
