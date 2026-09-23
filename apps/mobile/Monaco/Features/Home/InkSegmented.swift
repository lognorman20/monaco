import SwiftUI

/// `MonacoSegmented`'s ink-world twin: a capsule track on `Ink.sunken` with a neutral
/// `Ink.lineStrong` (`#FFFFFF`@0.18) thumb and white labels.
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
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

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

    /// Five options across one track cannot hold their labels at accessibility sizes: at AX5 the
    /// row read "1… 1… 1… 1… …", which is a control nobody can use to pick a window. Past that
    /// threshold the track stacks, exactly as the cash fold above it and the vote deck below it
    /// do — one full-width row per window, every label whole.
    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        let layout = isStacked
            ? AnyLayout(VStackLayout(spacing: 4))
            : AnyLayout(HStackLayout(spacing: 0))

        layout {
            ForEach(options, id: \.self) { option in
                optionButton(option)
            }
        }
        .padding(4)
        .frame(minHeight: 44)
        .background(shape.fill(MonacoTheme.Ink.sunken))
        .overlay(shape.strokeBorder(MonacoTheme.Ink.line, lineWidth: 1))
    }

    /// A stacked track is a rounded container, not a pill: a capsule around five rows would
    /// bow its ends away from the options inside it.
    private var shape: AnyInsettableShape {
        isStacked
            ? AnyInsettableShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous))
            : AnyInsettableShape(Capsule())
    }

    private func optionButton(_ option: T) -> some View {
        let isSelected = option == selection
        return Button {
            guard option != selection else { return }
            Haptics.selection()
            withAnimation(MonacoMotion.snap.reduced(reduceMotion)) {
                selection = option
            }
        } label: {
            Text(label(option))
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .lineLimit(isStacked ? nil : 1)
                .minimumScaleFactor(isStacked ? 1 : 0.8)
                .foregroundStyle(isSelected ? MonacoTheme.Ink.fgPrimary : MonacoTheme.Ink.fgMuted)
                // 36/12, matching `MonacoSegmented` exactly: the per-option tap target is the
                // smaller of the two boxes, and 32 inside a 44pt track put it under the 44pt
                // floor.
                .padding(.horizontal, 12)
                .padding(.vertical, isStacked ? 8 : 0)
                .frame(maxWidth: .infinity, minHeight: 36)
                .background {
                    if isSelected {
                        Capsule()
                            .fill(MonacoTheme.Ink.lineStrong)
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

/// A type-erased `InsettableShape`, so the track can be a capsule in one layout and a rounded
/// rectangle in the other without the two branches having different view types.
struct AnyInsettableShape: InsettableShape {
    private let pathBuilder: (CGRect) -> Path
    private let insetBuilder: (CGFloat) -> AnyInsettableShape

    init<S: InsettableShape>(_ shape: S) {
        pathBuilder = { shape.path(in: $0) }
        insetBuilder = { AnyInsettableShape(shape.inset(by: $0)) }
    }

    func path(in rect: CGRect) -> Path { pathBuilder(rect) }

    func inset(by amount: CGFloat) -> AnyInsettableShape { insetBuilder(amount) }
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
