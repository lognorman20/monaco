import SwiftUI

/// The brand block at the top of sign-in: the mark at launch-screen size, the wordmark, and the
/// one sentence that says what Monaco is.
///
/// Avenir Next is gone. The wordmark is SF Pro **Expanded** Bold at 40 with −0.025em of tracking,
/// which is the same width axis the whole display voice now uses — no bundled face, no licence to
/// verify, and no silent fallback where a bad `Info.plist` ships an app that looks subtly wrong.
///
/// The mark's three circles stagger in 80ms apart, and that is the only animation in the pre-auth
/// flow. Under Reduce Motion they are simply there.
struct LaunchScreenView: View {
    /// Scaled inside the view tree, the way `MoneyFont` and `DisplayFont` are, so a text-size
    /// change actually moves it and a `.dynamicTypeSize` cap applies.
    @ScaledMetric(relativeTo: .largeTitle) private var wordmarkSize: CGFloat = 40

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            MonacoMark(size: 88, stagger: true)
                .padding(.bottom, MonacoTheme.Space.s)

            Text("Monaco")
                .font(.system(size: wordmarkSize, weight: .bold).width(.expanded))
                .tracking(wordmarkSize * -0.025)
                .foregroundStyle(MonacoTheme.Ink.fgPrimary)
                .minimumScaleFactor(0.7)
                .lineLimit(1)
                .accessibilityAddTraits(.isHeader)

            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                Text("Your group chat, with a portfolio.")
                    .font(.system(.title2, weight: .semibold).width(.expanded))
                    .foregroundStyle(MonacoTheme.Ink.fgPrimary)
                    .fixedSize(horizontal: false, vertical: true)

                Text("Pool money with friends, vote on every buy, and see who's up.")
                    .font(MonacoTheme.Typo.body)
                    .foregroundStyle(MonacoTheme.Ink.fgMuted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

#Preview {
    LaunchScreenView()
        .padding()
        .authScreenBackground()
}
