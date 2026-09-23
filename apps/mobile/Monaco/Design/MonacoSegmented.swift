import SwiftUI

/// The app's one selection vocabulary: a capsule track with a brand thumb that slides between
/// options — the selected label on the thumb, the others muted on the track.
///
/// It reads `\.monacoWorld`, so the same control works on paper and inside an ink band: the track
/// takes the world's quiet fill (`fillQuiet` on paper, `Ink.sunken` on ink, because a track inside
/// an ink band is a *well*, not a raised control) and the unselected labels take the world's muted
/// foreground. The thumb stays brand in both worlds — blue means tap, everywhere.
struct MonacoSegmented<T: Hashable>: View {
    private let options: [T]
    @Binding private var selection: T
    private let label: (T) -> String

    @Namespace private var thumb
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.monacoPalette) private var palette

    init(_ options: [T], selection: Binding<T>, label: @escaping (T) -> String) {
        self.options = options
        _selection = selection
        self.label = label
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
                        .foregroundStyle(isSelected ? MonacoTheme.primaryButtonLabel : palette.fgMuted)
                        .padding(.horizontal, 12)
                        .frame(maxWidth: .infinity, minHeight: 36)
                        .background {
                            if isSelected {
                                Capsule()
                                    .fill(MonacoTheme.primaryButtonFill)
                                    .matchedGeometryEffect(id: "thumb", in: thumb)
                            }
                        }
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(isSelected ? [.isSelected] : [])
            }
        }
        .padding(4)
        .frame(minHeight: 44)
        .background(Capsule().fill(palette.quietFill))
    }
}
