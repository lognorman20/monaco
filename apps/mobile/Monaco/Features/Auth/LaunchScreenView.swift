import SwiftUI

/// Brand block at the top of login: the mark at launch-screen size, wordmark, one headline.
struct LaunchScreenView: View {
    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            MonacoMark(size: 88)
                .padding(.bottom, MonacoTheme.Space.s)

            Text("Monaco")
                .font(.custom("AvenirNext-Bold", size: 40, relativeTo: .largeTitle))
                .foregroundStyle(MonacoTheme.ink)
                .accessibilityAddTraits(.isHeader)

            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                Text("Your group chat, with a portfolio.")
                    .font(.system(size: 22, weight: .semibold))
                    .foregroundStyle(MonacoTheme.ink)
                    .fixedSize(horizontal: false, vertical: true)

                Text("Pool money with friends, vote on every buy, and see who's up.")
                    .font(MonacoTheme.Typo.body)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

#Preview {
    LaunchScreenView()
        .padding()
}
