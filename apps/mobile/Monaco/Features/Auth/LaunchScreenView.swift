import SwiftUI

/// The brand block at the top of login: the mark, the wordmark, what Monaco is in one line and
/// what you do in it in another. Nothing else shares the first viewport with it but the form.
///
/// The mark is the four-person cluster, so it is the picture for "with your friends" as well as
/// the logo; it is the same artwork the launch screen shows a moment earlier.
struct LaunchScreenView: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            MonacoMark(size: 64)

            Text("Monaco")
                .font(MonacoTheme.Typo.display)
                .foregroundStyle(MonacoTheme.ink)
                .accessibilityAddTraits(.isHeader)
                .padding(.top, MonacoTheme.Space.l)

            Text("The hedge fund with your friends.")
                .font(MonacoTheme.Typo.title)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, MonacoTheme.Space.xs)

            Text("Pool money in a cabal, vote on every buy, and see who's up.")
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, MonacoTheme.Space.s)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

#Preview {
    LaunchScreenView()
        .padding()
}
