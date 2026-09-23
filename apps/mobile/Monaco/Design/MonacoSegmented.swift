import SwiftUI

/// The app's one selection vocabulary: a capsule track with a brand thumb that slides between
/// options — the selected label on the thumb, the others muted on the track.
///
/// It reads `\.monacoWorld`, so the same control works on paper and inside an ink band: the track
/// takes the world's quiet fill (`fillQuiet` on paper, `Ink.sunken` on ink, because a track inside
/// an ink band is a *well*, not a raised control) and the unselected labels take the world's muted
/// foreground. The thumb stays brand in both worlds — blue means tap, everywhere. The track also
/// carries a 1pt `line` edge, because its fill alone is 1.05:1 on the light canvas.
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
        // The track keeps a 1pt edge, and it is the same decision as `MonacoChip`'s: `fillQuiet`
        // `#EDF1F7` on `bgBase` `#F4F6FA` measures **1.05:1** in light, so a segmented control
        // sitting straight on the canvas — which is where most of them sit — shows nothing but a
        // brand thumb floating in space, and the unselected options stop reading as a control at
        // all. On a card (`bgRaised` `#FFFFFF`) the fill does carry itself, but a control that
        // only has an edge on half the surfaces it lands on is a control nobody can place.
        .overlay(Capsule().strokeBorder(palette.line, lineWidth: 1))
    }
}
