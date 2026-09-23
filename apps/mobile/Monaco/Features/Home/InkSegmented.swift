import SwiftUI

/// `MonacoSegmented`'s ink-world twin: a capsule track on `Ink.sunken` with a neutral
/// `#FFFFFF`@0.14 thumb and white labels.
///
/// Two reasons it is not just `MonacoSegmented` today. First, that control fills its track with
/// `surfaceSunken`, which is `#EDF1F7` in light mode — a near-white capsule sitting on an ink slab
/// that is dark in *both* schemes. Second, its thumb is `brandFill`, and an ink surface may carry
/// **one** saturated fill (§1.9): on Home's slab that one is "Add money", so the range the curve is
/// showing cannot also spend it. The thumb is therefore an ink wash, not the accent — blue still
/// means tap, and the pills are still tappable, they just do not shout it.
///
/// It folds back into `MonacoSegmented` the moment Chunk B's world-aware version lands: the
/// selection, the haptic, the `matchedGeometryEffect` and the `.isSelected` trait are the same
/// mechanism, so the merge is a `\.monacoWorld` read and a deletion.
struct InkSegmented<T: Hashable>: View {
    private let options: [T]
    @Binding private var selection: T
    private let label: (T) -> String
    private let accessibilityName: (T) -> String

    @Namespace private var thumb
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(
        _ options: [T],
        selection: Binding<T>,
        label: @escaping (T) -> String,
        accessibilityName: ((T) -> String)? = nil
    ) {
        self.options = options
        _selection = selection
        self.label = label
        self.accessibilityName = accessibilityName ?? label
    }

    var body: some View {
        HStack(spacing: 0) {
            ForEach(options, id: \.self) { option in
                let isSelected = option == selection
                Button {
                    guard option != selection else { return }
                    Haptics.selection()
                    withAnimation(MonacoMotion.snap.reduced(reduceMotion)) {
                        selection = option
                    }
                } label: {
                    Text(label(option))
                        .font(MonacoTheme.Typo.callout.weight(.semibold))
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        .foregroundStyle(isSelected ? MonacoTheme.Ink.fgPrimary : MonacoTheme.Ink.fgMuted)
                        .padding(.horizontal, 10)
                        .frame(maxWidth: .infinity, minHeight: 32)
                        .background {
                            if isSelected {
                                Capsule()
                                    .fill(Color.white.opacity(0.14))
                                    .matchedGeometryEffect(id: "ink-thumb", in: thumb)
                            }
                        }
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(accessibilityName(option))
                .accessibilityAddTraits(isSelected ? [.isSelected] : [])
            }
        }
        .padding(4)
        .frame(minHeight: 44)
        .background(Capsule().fill(MonacoTheme.Ink.sunken))
        .overlay(Capsule().strokeBorder(MonacoTheme.Ink.line, lineWidth: 1))
    }
}

#Preview {
    struct Harness: View {
        @State private var range: HomeLeaderboardRange = .oneDay
        var body: some View {
            VStack(spacing: 24) {
                InkSegmented(HomeLeaderboardRange.allCases, selection: $range) { $0.label }
            }
            .padding(24)
            .background(MonacoTheme.Ink.base)
        }
    }
    return Harness()
}
